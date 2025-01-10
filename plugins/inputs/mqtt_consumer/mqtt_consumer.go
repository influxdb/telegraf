//go:generate ../../../tools/readme_config_includer/generator
package mqtt_consumer

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/selfstat"
)

//go:embed sample.conf
var sampleConfig string

var (
	once sync.Once
	// 30 Seconds is the default used by paho.mqtt.golang
	defaultConnectionTimeout      = config.Duration(30 * time.Second)
	defaultMaxUndeliveredMessages = 1000
)

type MQTTConsumer struct {
	Servers                []string             `toml:"servers"`
	Topics                 []string             `toml:"topics"`
	TopicTag               *string              `toml:"topic_tag"`
	TopicParserConfig      []topicParsingConfig `toml:"topic_parsing"`
	Username               config.Secret        `toml:"username"`
	Password               config.Secret        `toml:"password"`
	QoS                    int                  `toml:"qos"`
	ConnectionTimeout      config.Duration      `toml:"connection_timeout"`
	KeepAliveInterval      config.Duration      `toml:"keepalive"`
	PingTimeout            config.Duration      `toml:"ping_timeout"`
	MaxUndeliveredMessages int                  `toml:"max_undelivered_messages"`
	PersistentSession      bool                 `toml:"persistent_session"`
	ClientTrace            bool                 `toml:"client_trace"`
	ClientID               string               `toml:"client_id"`
	Log                    telegraf.Logger      `toml:"-"`
	tls.ClientConfig

	parser        telegraf.Parser
	clientFactory clientFactory
	client        client
	opts          *mqtt.ClientOptions
	acc           telegraf.TrackingAccumulator
	sem           semaphore
	messages      map[telegraf.TrackingID]mqtt.Message
	messagesMutex sync.Mutex
	topicTagParse string
	topicParsers  []*topicParser
	ctx           context.Context
	cancel        context.CancelFunc
	payloadSize   selfstat.Stat
	messagesRecv  selfstat.Stat
	wg            sync.WaitGroup
}

type client interface {
	Connect() mqtt.Token
	SubscribeMultiple(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token
	AddRoute(topic string, callback mqtt.MessageHandler)
	Disconnect(quiesce uint)
	IsConnected() bool
}

type empty struct{}
type semaphore chan empty
type clientFactory func(o *mqtt.ClientOptions) client

func (*MQTTConsumer) SampleConfig() string {
	return sampleConfig
}

func (m *MQTTConsumer) Init() error {
	if m.ClientTrace {
		log := &mqttLogger{m.Log}
		mqtt.ERROR = log
		mqtt.CRITICAL = log
		mqtt.WARN = log
		mqtt.DEBUG = log
	}

	if m.PersistentSession && m.ClientID == "" {
		return errors.New("persistent_session requires client_id")
	}
	if m.QoS > 2 || m.QoS < 0 {
		return fmt.Errorf("qos value must be 0, 1, or 2: %d", m.QoS)
	}
	if time.Duration(m.ConnectionTimeout) < 1*time.Second {
		return fmt.Errorf("connection_timeout must be greater than 1s: %s", time.Duration(m.ConnectionTimeout))
	}
	m.topicTagParse = "topic"
	if m.TopicTag != nil {
		m.topicTagParse = *m.TopicTag
	}
	opts, err := m.createOpts()
	if err != nil {
		return err
	}
	m.opts = opts
	m.messages = make(map[telegraf.TrackingID]mqtt.Message)

	m.topicParsers = make([]*topicParser, 0, len(m.TopicParserConfig))
	for _, cfg := range m.TopicParserConfig {
		p, err := cfg.newParser()
		if err != nil {
			return fmt.Errorf("config error topic parsing: %w", err)
		}
		m.topicParsers = append(m.topicParsers, p)
	}

	m.payloadSize = selfstat.Register("mqtt_consumer", "payload_size", make(map[string]string))
	m.messagesRecv = selfstat.Register("mqtt_consumer", "messages_received", make(map[string]string))
	return nil
}

func (m *MQTTConsumer) SetParser(parser telegraf.Parser) {
	m.parser = parser
}

func (m *MQTTConsumer) Start(acc telegraf.Accumulator) error {
	m.acc = acc.WithTracking(m.MaxUndeliveredMessages)
	m.sem = make(semaphore, m.MaxUndeliveredMessages)
	m.ctx, m.cancel = context.WithCancel(context.Background())

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-m.ctx.Done():
				return
			case track := <-m.acc.Delivered():
				m.onDelivered(track)
			}
		}
	}()

	return m.connect()
}

func (m *MQTTConsumer) Gather(_ telegraf.Accumulator) error {
	if !m.client.IsConnected() {
		m.Log.Debugf("Connecting %v", m.Servers)
		return m.connect()
	}
	return nil
}

func (m *MQTTConsumer) Stop() {
	if m.client.IsConnected() {
		m.Log.Debugf("Disconnecting %v", m.Servers)
		m.client.Disconnect(200)
		m.Log.Debugf("Disconnected %v", m.Servers)
	}
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *MQTTConsumer) connect() error {
	m.client = m.clientFactory(m.opts)
	// AddRoute sets up the function for handling messages.  These need to be
	// added in case we find a persistent session containing subscriptions so we
	// know where to dispatch persisted and new messages to.  In the alternate
	// case that we need to create the subscriptions these will be replaced.
	for _, topic := range m.Topics {
		m.client.AddRoute(topic, m.onMessage)
	}
	token := m.client.Connect()
	if token.Wait() && token.Error() != nil {
		if ct, ok := token.(*mqtt.ConnectToken); ok && ct.ReturnCode() == packets.ErrNetworkError {
			// Network errors might be retryable, stop the metric-tracking
			// goroutine and return a retryable error.
			if m.cancel != nil {
				m.cancel()
				m.cancel = nil
			}
			return &internal.StartupError{
				Err:   token.Error(),
				Retry: true,
			}
		}
		return token.Error()
	}
	m.Log.Infof("Connected %v", m.Servers)

	// Persistent sessions should skip subscription if a session is present, as
	// the subscriptions are stored by the server.
	type sessionPresent interface {
		SessionPresent() bool
	}
	if t, ok := token.(sessionPresent); ok && t.SessionPresent() {
		m.Log.Debugf("Session found %v", m.Servers)
		return nil
	}
	topics := make(map[string]byte)
	for _, topic := range m.Topics {
		topics[topic] = byte(m.QoS)
	}
	subscribeToken := m.client.SubscribeMultiple(topics, m.onMessage)
	subscribeToken.Wait()
	if subscribeToken.Error() != nil {
		m.acc.AddError(fmt.Errorf("subscription error: topics %q: %w", strings.Join(m.Topics[:], ","), subscribeToken.Error()))
	}
	return nil
}

func (m *MQTTConsumer) onConnectionLost(_ mqtt.Client, err error) {
	// Should already be disconnected, but make doubly sure
	m.client.Disconnect(5)
	m.acc.AddError(fmt.Errorf("connection lost: %w", err))
	m.Log.Debugf("Disconnected %v", m.Servers)
}

func (m *MQTTConsumer) onDelivered(track telegraf.DeliveryInfo) {
	<-m.sem

	m.messagesMutex.Lock()
	defer m.messagesMutex.Unlock()

	msg, ok := m.messages[track.ID()]
	if !ok {
		m.Log.Errorf("could not mark message delivered: %d", track.ID())
		return
	}

	if track.Delivered() && m.PersistentSession {
		msg.Ack()
	}

	delete(m.messages, track.ID())
}

func (m *MQTTConsumer) onMessage(_ mqtt.Client, msg mqtt.Message) {
	m.sem <- empty{}

	payloadBytes := len(msg.Payload())
	m.payloadSize.Incr(int64(payloadBytes))
	m.messagesRecv.Incr(1)

	metrics, err := m.parser.Parse(msg.Payload())
	if err != nil || len(metrics) == 0 {
		if len(metrics) == 0 {
			once.Do(func() {
				m.Log.Warn(internal.NoMetricsCreatedMsg)
			})
		}

		if m.PersistentSession {
			msg.Ack()
		}
		m.acc.AddError(err)
		<-m.sem
		return
	}

	for _, metric := range metrics {
		if m.topicTagParse != "" {
			metric.AddTag(m.topicTagParse, msg.Topic())
		}
		for _, p := range m.topicParsers {
			if err := p.parse(metric, msg.Topic()); err != nil {
				if m.PersistentSession {
					msg.Ack()
				}
				m.acc.AddError(err)
				<-m.sem
				return
			}
		}
	}
	m.messagesMutex.Lock()
	id := m.acc.AddTrackingMetricGroup(metrics)
	m.messages[id] = msg
	m.messagesMutex.Unlock()
}

func (m *MQTTConsumer) createOpts() (*mqtt.ClientOptions, error) {
	opts := mqtt.NewClientOptions()
	opts.ConnectTimeout = time.Duration(m.ConnectionTimeout)
	if m.ClientID == "" {
		randomString, err := internal.RandomString(5)
		if err != nil {
			return nil, fmt.Errorf("generating random string for client ID failed: %w", err)
		}
		opts.SetClientID("Telegraf-Consumer-" + randomString)
	} else {
		opts.SetClientID(m.ClientID)
	}
	tlsCfg, err := m.ClientConfig.TLSConfig()
	if err != nil {
		return nil, err
	}
	if tlsCfg != nil {
		opts.SetTLSConfig(tlsCfg)
	}
	if !m.Username.Empty() {
		user, err := m.Username.Get()
		if err != nil {
			return nil, fmt.Errorf("getting username failed: %w", err)
		}
		opts.SetUsername(user.String())
		user.Destroy()
	}

	if !m.Password.Empty() {
		password, err := m.Password.Get()
		if err != nil {
			return nil, fmt.Errorf("getting password failed: %w", err)
		}
		opts.SetPassword(password.String())
		password.Destroy()
	}
	if len(m.Servers) == 0 {
		return opts, errors.New("could not get host information")
	}
	for _, server := range m.Servers {
		// Preserve support for host:port style servers; deprecated in Telegraf 1.4.4
		if !strings.Contains(server, "://") {
			m.Log.Warnf("Server %q should be updated to use `scheme://host:port` format", server)
			if tlsCfg == nil {
				server = "tcp://" + server
			} else {
				server = "ssl://" + server
			}
		}
		opts.AddBroker(server)
	}
	opts.SetAutoReconnect(false)
	opts.SetKeepAlive(time.Duration(m.KeepAliveInterval))
	opts.SetPingTimeout(time.Duration(m.PingTimeout))
	opts.SetCleanSession(!m.PersistentSession)
	opts.SetAutoAckDisabled(m.PersistentSession)
	opts.SetConnectionLostHandler(m.onConnectionLost)
	return opts, nil
}

func newMQTTConsumer(factory clientFactory) *MQTTConsumer {
	return &MQTTConsumer{
		Servers:                []string{"tcp://127.0.0.1:1883"},
		MaxUndeliveredMessages: defaultMaxUndeliveredMessages,
		ConnectionTimeout:      defaultConnectionTimeout,
		KeepAliveInterval:      config.Duration(60 * time.Second),
		PingTimeout:            config.Duration(10 * time.Second),
		clientFactory:          factory,
	}
}
func init() {
	inputs.Add("mqtt_consumer", func() telegraf.Input {
		return newMQTTConsumer(func(o *mqtt.ClientOptions) client {
			return mqtt.NewClient(o)
		})
	})
}
