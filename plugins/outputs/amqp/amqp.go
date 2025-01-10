//go:generate ../../../tools/readme_config_includer/generator
package amqp

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/common/proxy"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	DefaultURL             = "amqp://localhost:5672/influxdb"
	DefaultAuthMethod      = "PLAIN"
	DefaultExchangeType    = "topic"
	DefaultRetentionPolicy = "default"
	DefaultDatabase        = "telegraf"
)

type externalAuth struct{}

func (a *externalAuth) Mechanism() string {
	return "EXTERNAL"
}

func (a *externalAuth) Response() string {
	return "\000"
}

type AMQP struct {
	URL                string            `toml:"url" deprecated:"1.7.0;1.35.0;use 'brokers' instead"`
	Brokers            []string          `toml:"brokers"`
	Exchange           string            `toml:"exchange"`
	ExchangeType       string            `toml:"exchange_type"`
	ExchangePassive    bool              `toml:"exchange_passive"`
	ExchangeDurability string            `toml:"exchange_durability"`
	ExchangeArguments  map[string]string `toml:"exchange_arguments"`
	Username           config.Secret     `toml:"username"`
	Password           config.Secret     `toml:"password"`
	MaxMessages        int               `toml:"max_messages"`
	AuthMethod         string            `toml:"auth_method"`
	RoutingTag         string            `toml:"routing_tag"`
	RoutingKey         string            `toml:"routing_key"`
	DeliveryMode       string            `toml:"delivery_mode"`
	Database           string            `toml:"database" deprecated:"1.7.0;1.35.0;use 'headers' instead"`
	RetentionPolicy    string            `toml:"retention_policy" deprecated:"1.7.0;1.35.0;use 'headers' instead"`
	Precision          string            `toml:"precision" deprecated:"1.2.0;1.35.0;option is ignored"`
	Headers            map[string]string `toml:"headers"`
	Timeout            config.Duration   `toml:"timeout"`
	UseBatchFormat     bool              `toml:"use_batch_format"`
	ContentEncoding    string            `toml:"content_encoding"`
	Log                telegraf.Logger   `toml:"-"`
	tls.ClientConfig
	proxy.TCPProxy

	serializer   telegraf.Serializer
	connect      func(*ClientConfig) (Client, error)
	client       Client
	config       *ClientConfig
	sentMessages int
	encoder      internal.ContentEncoder
}

type Client interface {
	Publish(key string, body []byte) error
	Close() error
}

func (*AMQP) SampleConfig() string {
	return sampleConfig
}

func (q *AMQP) SetSerializer(serializer telegraf.Serializer) {
	q.serializer = serializer
}

func (q *AMQP) Init() error {
	var err error
	q.config, err = q.makeClientConfig()
	if err != nil {
		return err
	}

	q.encoder, err = internal.NewContentEncoder(q.ContentEncoding)
	if err != nil {
		return err
	}

	return nil
}

func (q *AMQP) Connect() error {
	var err error
	q.client, err = q.connect(q.config)
	return err
}

func (q *AMQP) Close() error {
	if q.client != nil {
		return q.client.Close()
	}
	return nil
}

func (q *AMQP) routingKey(metric telegraf.Metric) string {
	if q.RoutingTag != "" {
		key, ok := metric.GetTag(q.RoutingTag)
		if ok {
			return key
		}
	}
	return q.RoutingKey
}

func (q *AMQP) Write(metrics []telegraf.Metric) error {
	batches := make(map[string][]telegraf.Metric)
	if q.ExchangeType == "header" {
		// Since the routing_key is ignored for this exchange type send as a
		// single batch.
		batches[""] = metrics
	} else {
		for _, metric := range metrics {
			routingKey := q.routingKey(metric)
			if _, ok := batches[routingKey]; !ok {
				batches[routingKey] = make([]telegraf.Metric, 0)
			}

			batches[routingKey] = append(batches[routingKey], metric)
		}
	}

	first := true
	for key, metrics := range batches {
		body, err := q.serialize(metrics)
		if err != nil {
			return err
		}

		body, err = q.encoder.Encode(body)
		if err != nil {
			return err
		}

		err = q.publish(key, body)
		if err != nil {
			// If this is the first attempt to publish and the connection is
			// closed, try to reconnect and retry once.

			var aerr *amqp.Error
			if first && errors.As(err, &aerr) && errors.Is(aerr, amqp.ErrClosed) {
				q.client = nil
				err := q.publish(key, body)
				if err != nil {
					return err
				}
			} else if q.client != nil {
				if err := q.client.Close(); err != nil {
					q.Log.Errorf("Closing connection failed: %v", err)
				}
				q.client = nil
				return err
			}
		}
		first = false
	}

	if q.sentMessages >= q.MaxMessages && q.MaxMessages > 0 {
		q.Log.Debug("Sent MaxMessages; closing connection")
		if err := q.client.Close(); err != nil {
			q.Log.Errorf("Closing connection failed: %v", err)
		}
		q.client = nil
	}

	return nil
}

func (q *AMQP) publish(key string, body []byte) error {
	if q.client == nil {
		client, err := q.connect(q.config)
		if err != nil {
			return err
		}
		q.sentMessages = 0
		q.client = client
	}

	err := q.client.Publish(key, body)
	if err != nil {
		return err
	}
	q.sentMessages++
	return nil
}

func (q *AMQP) serialize(metrics []telegraf.Metric) ([]byte, error) {
	if q.UseBatchFormat {
		return q.serializer.SerializeBatch(metrics)
	}

	var buf bytes.Buffer
	for _, metric := range metrics {
		octets, err := q.serializer.Serialize(metric)
		if err != nil {
			q.Log.Debugf("Could not serialize metric: %v", err)
			continue
		}
		buf.Write(octets)
	}
	body := buf.Bytes()
	return body, nil
}

func (q *AMQP) makeClientConfig() (*ClientConfig, error) {
	clientConfig := &ClientConfig{
		exchange:        q.Exchange,
		exchangeType:    q.ExchangeType,
		exchangePassive: q.ExchangePassive,
		encoding:        q.ContentEncoding,
		timeout:         time.Duration(q.Timeout),
		log:             q.Log,
	}

	switch q.ExchangeDurability {
	case "transient":
		clientConfig.exchangeDurable = false
	default:
		clientConfig.exchangeDurable = true
	}

	clientConfig.brokers = q.Brokers
	if len(clientConfig.brokers) == 0 {
		clientConfig.brokers = []string{q.URL}
	}

	switch q.DeliveryMode {
	case "transient":
		clientConfig.deliveryMode = amqp.Transient
	case "persistent":
		clientConfig.deliveryMode = amqp.Persistent
	default:
		clientConfig.deliveryMode = amqp.Transient
	}

	if len(q.Headers) > 0 {
		clientConfig.headers = make(amqp.Table, len(q.Headers))
		for k, v := range q.Headers {
			clientConfig.headers[k] = v
		}
	} else {
		// Copy deprecated fields into message header
		clientConfig.headers = amqp.Table{
			"database":         q.Database,
			"retention_policy": q.RetentionPolicy,
		}
	}

	if len(q.ExchangeArguments) > 0 {
		clientConfig.exchangeArguments = make(amqp.Table, len(q.ExchangeArguments))
		for k, v := range q.ExchangeArguments {
			clientConfig.exchangeArguments[k] = v
		}
	}

	tlsConfig, err := q.ClientConfig.TLSConfig()
	if err != nil {
		return nil, err
	}
	clientConfig.tlsConfig = tlsConfig

	dialer, err := q.TCPProxy.Proxy()
	if err != nil {
		return nil, err
	}
	clientConfig.dialer = dialer

	var auth []amqp.Authentication
	if strings.EqualFold(q.AuthMethod, "EXTERNAL") {
		auth = []amqp.Authentication{&externalAuth{}}
	} else if !q.Username.Empty() || !q.Password.Empty() {
		username, err := q.Username.Get()
		if err != nil {
			return nil, fmt.Errorf("getting username failed: %w", err)
		}
		defer username.Destroy()
		password, err := q.Password.Get()
		if err != nil {
			return nil, fmt.Errorf("getting password failed: %w", err)
		}
		defer password.Destroy()
		auth = []amqp.Authentication{
			&amqp.PlainAuth{
				Username: username.String(),
				Password: password.String(),
			},
		}
	}
	clientConfig.auth = auth

	return clientConfig, nil
}

func connect(clientConfig *ClientConfig) (Client, error) {
	return newClient(clientConfig)
}

func init() {
	outputs.Add("amqp", func() telegraf.Output {
		return &AMQP{
			Brokers:      []string{DefaultURL},
			ExchangeType: DefaultExchangeType,
			AuthMethod:   DefaultAuthMethod,
			Headers: map[string]string{
				"database":         DefaultDatabase,
				"retention_policy": DefaultRetentionPolicy,
			},
			Timeout: config.Duration(time.Second * 5),
			connect: connect,
		}
	})
}
