//go:generate ../../../tools/readme_config_includer/generator
package amqp_consumer

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var once sync.Once

type empty struct{}
type externalAuth struct{}

type semaphore chan empty

type AMQPConsumer struct {
	URL                    string            `toml:"url" deprecated:"1.7.0;1.35.0;use 'brokers' instead"`
	Brokers                []string          `toml:"brokers"`
	Username               config.Secret     `toml:"username"`
	Password               config.Secret     `toml:"password"`
	Exchange               string            `toml:"exchange"`
	ExchangeType           string            `toml:"exchange_type"`
	ExchangeDurability     string            `toml:"exchange_durability"`
	ExchangePassive        bool              `toml:"exchange_passive"`
	ExchangeArguments      map[string]string `toml:"exchange_arguments"`
	MaxUndeliveredMessages int               `toml:"max_undelivered_messages"`
	Queue                  string            `toml:"queue"`
	QueueDurability        string            `toml:"queue_durability"`
	QueuePassive           bool              `toml:"queue_passive"`
	QueueArguments         map[string]int    `toml:"queue_arguments"`
	QueueConsumeArguments  map[string]string `toml:"queue_consume_arguments"`
	BindingKey             string            `toml:"binding_key"`
	PrefetchCount          int               `toml:"prefetch_count"`
	AuthMethod             string            `toml:"auth_method"`
	ContentEncoding        string            `toml:"content_encoding"`
	MaxDecompressionSize   config.Size       `toml:"max_decompression_size"`
	Timeout                config.Duration   `toml:"timeout"`
	Log                    telegraf.Logger   `toml:"-"`
	tls.ClientConfig

	deliveries map[telegraf.TrackingID]amqp.Delivery

	parser  telegraf.Parser
	conn    *amqp.Connection
	wg      *sync.WaitGroup
	cancel  context.CancelFunc
	decoder internal.ContentDecoder
}

func (*externalAuth) Mechanism() string {
	return "EXTERNAL"
}

func (*externalAuth) Response() string {
	return "\000"
}

func (*AMQPConsumer) SampleConfig() string {
	return sampleConfig
}

func (a *AMQPConsumer) Init() error {
	// Defaults
	if a.URL != "" {
		a.Brokers = append(a.Brokers, a.URL)
	}
	if len(a.Brokers) == 0 {
		a.Brokers = []string{"amqp://localhost:5672/influxdb"}
	}

	if a.AuthMethod == "" {
		a.AuthMethod = "PLAIN"
	}

	if a.ExchangeType == "" {
		a.ExchangeType = "topic"
	}

	if a.ExchangeDurability == "" {
		a.ExchangeDurability = "durable"
	}

	if a.QueueDurability == "" {
		a.QueueDurability = "durable"
	}

	if a.PrefetchCount == 0 {
		a.PrefetchCount = 50
	}

	if a.MaxUndeliveredMessages == 0 {
		a.MaxUndeliveredMessages = 1000
	}

	return nil
}

func (a *AMQPConsumer) SetParser(parser telegraf.Parser) {
	a.parser = parser
}

func (a *AMQPConsumer) Start(acc telegraf.Accumulator) error {
	amqpConf, err := a.createConfig()
	if err != nil {
		return err
	}

	var options []internal.DecodingOption
	if a.MaxDecompressionSize > 0 {
		options = append(options, internal.WithMaxDecompressionSize(int64(a.MaxDecompressionSize)))
	}
	a.decoder, err = internal.NewContentDecoder(a.ContentEncoding, options...)
	if err != nil {
		return err
	}

	msgs, err := a.connect(amqpConf)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	a.wg = &sync.WaitGroup{}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.process(ctx, msgs, acc)
	}()

	go func() {
		for {
			err := <-a.conn.NotifyClose(make(chan *amqp.Error))
			if err == nil {
				break
			}

			a.Log.Infof("Connection closed: %s; trying to reconnect", err)
			for {
				msgs, err := a.connect(amqpConf)
				if err != nil {
					a.Log.Errorf("AMQP connection failed: %s", err)
					time.Sleep(10 * time.Second)
					continue
				}

				a.wg.Add(1)
				go func() {
					defer a.wg.Done()
					a.process(ctx, msgs, acc)
				}()
				break
			}
		}
	}()

	return nil
}

func (*AMQPConsumer) Gather(_ telegraf.Accumulator) error {
	return nil
}

func (a *AMQPConsumer) Stop() {
	// We did not connect successfully so there is nothing to do here.
	if a.conn == nil || a.conn.IsClosed() {
		return
	}
	a.cancel()
	a.wg.Wait()
	err := a.conn.Close()
	if err != nil && !errors.Is(err, amqp.ErrClosed) {
		a.Log.Errorf("Error closing AMQP connection: %s", err)
		return
	}
}

func (a *AMQPConsumer) createConfig() (*amqp.Config, error) {
	// make new tls config
	tlsCfg, err := a.ClientConfig.TLSConfig()
	if err != nil {
		return nil, err
	}

	var auth []amqp.Authentication

	if strings.EqualFold(a.AuthMethod, "EXTERNAL") {
		auth = []amqp.Authentication{&externalAuth{}}
	} else if !a.Username.Empty() || !a.Password.Empty() {
		username, err := a.Username.Get()
		if err != nil {
			return nil, fmt.Errorf("getting username failed: %w", err)
		}
		defer username.Destroy()

		password, err := a.Password.Get()
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
	amqpConfig := amqp.Config{
		TLSClientConfig: tlsCfg,
		SASL:            auth, // if nil, it will be PLAIN
		Dial:            amqp.DefaultDial(time.Duration(a.Timeout)),
	}
	return &amqpConfig, nil
}

func (a *AMQPConsumer) connect(amqpConf *amqp.Config) (<-chan amqp.Delivery, error) {
	brokers := a.Brokers

	p := rand.Perm(len(brokers))
	for _, n := range p {
		broker := brokers[n]
		a.Log.Debugf("Connecting to %q", broker)
		conn, err := amqp.DialConfig(broker, *amqpConf)
		if err == nil {
			a.conn = conn
			a.Log.Debugf("Connected to %q", broker)
			break
		}
		a.Log.Errorf("Error connecting to %q: %s", broker, err)
	}

	if a.conn == nil {
		return nil, &internal.StartupError{
			Err:   errors.New("could not connect to any broker"),
			Retry: true,
		}
	}

	ch, err := a.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	if a.Exchange != "" {
		exchangeDurable := true
		if a.ExchangeDurability == "transient" {
			exchangeDurable = false
		}

		exchangeArgs := make(amqp.Table, len(a.ExchangeArguments))
		for k, v := range a.ExchangeArguments {
			exchangeArgs[k] = v
		}

		err = a.declareExchange(
			ch,
			exchangeDurable,
			exchangeArgs)
		if err != nil {
			return nil, err
		}
	}

	q, err := a.declareQueue(ch)
	if err != nil {
		return nil, err
	}

	if a.BindingKey != "" {
		err = ch.QueueBind(
			q.Name,       // queue
			a.BindingKey, // binding-key
			a.Exchange,   // exchange
			false,
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to bind a queue: %w", err)
		}
	}

	err = ch.Qos(
		a.PrefetchCount,
		0,     // prefetch-size
		false, // global
	)
	if err != nil {
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	consumeArgs := make(amqp.Table, len(a.QueueConsumeArguments))
	for k, v := range a.QueueConsumeArguments {
		consumeArgs[k] = v
	}

	msgs, err := ch.Consume(
		q.Name,      // queue
		"",          // consumer
		false,       // auto-ack
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		consumeArgs, // arguments
	)
	if err != nil {
		return nil, fmt.Errorf("failed establishing connection to queue: %w", err)
	}

	return msgs, err
}

func (a *AMQPConsumer) declareExchange(
	channel *amqp.Channel,
	exchangeDurable bool,
	exchangeArguments amqp.Table,
) error {
	var err error
	if a.ExchangePassive {
		err = channel.ExchangeDeclarePassive(
			a.Exchange,
			a.ExchangeType,
			exchangeDurable,
			false, // delete when unused
			false, // internal
			false, // no-wait
			exchangeArguments,
		)
	} else {
		err = channel.ExchangeDeclare(
			a.Exchange,
			a.ExchangeType,
			exchangeDurable,
			false, // delete when unused
			false, // internal
			false, // no-wait
			exchangeArguments,
		)
	}
	if err != nil {
		return fmt.Errorf("error declaring exchange: %w", err)
	}
	return nil
}

func (a *AMQPConsumer) declareQueue(channel *amqp.Channel) (*amqp.Queue, error) {
	var queue amqp.Queue
	var err error

	queueDurable := true
	if a.QueueDurability == "transient" {
		queueDurable = false
	}

	queueArgs := make(amqp.Table, len(a.QueueArguments))
	for k, v := range a.QueueArguments {
		queueArgs[k] = v
	}

	if a.QueuePassive {
		queue, err = channel.QueueDeclarePassive(
			a.Queue,      // queue
			queueDurable, // durable
			false,        // delete when unused
			false,        // exclusive
			false,        // no-wait
			queueArgs,    // arguments
		)
	} else {
		queue, err = channel.QueueDeclare(
			a.Queue,      // queue
			queueDurable, // durable
			false,        // delete when unused
			false,        // exclusive
			false,        // no-wait
			queueArgs,    // arguments
		)
	}
	if err != nil {
		return nil, fmt.Errorf("error declaring queue: %w", err)
	}
	return &queue, nil
}

// Read messages from queue and add them to the Accumulator
func (a *AMQPConsumer) process(ctx context.Context, msgs <-chan amqp.Delivery, ac telegraf.Accumulator) {
	a.deliveries = make(map[telegraf.TrackingID]amqp.Delivery)

	acc := ac.WithTracking(a.MaxUndeliveredMessages)
	sem := make(semaphore, a.MaxUndeliveredMessages)

	for {
		select {
		case <-ctx.Done():
			return
		case track := <-acc.Delivered():
			if a.onDelivery(track) {
				<-sem
			}
		case sem <- empty{}:
			select {
			case <-ctx.Done():
				return
			case track := <-acc.Delivered():
				if a.onDelivery(track) {
					<-sem
					<-sem
				}
			case d, ok := <-msgs:
				if !ok {
					return
				}
				err := a.onMessage(acc, d)
				if err != nil {
					acc.AddError(err)
					<-sem
				}
			}
		}
	}
}

func (a *AMQPConsumer) onMessage(acc telegraf.TrackingAccumulator, d amqp.Delivery) error {
	onError := func() {
		// Discard the message from the queue; will never be able to process it
		if err := d.Nack(false, false); err != nil {
			a.Log.Errorf("Unable to NACK message: %d: %v", d.DeliveryTag, err)
			a.conn.Close()
		}
	}

	a.decoder.SetEncoding(d.ContentEncoding)
	body, err := a.decoder.Decode(d.Body)
	if err != nil {
		onError()
		return err
	}

	metrics, err := a.parser.Parse(body)
	if err != nil {
		onError()
		return err
	}
	if len(metrics) == 0 {
		once.Do(func() {
			a.Log.Debug(internal.NoMetricsCreatedMsg)
		})
	}

	id := acc.AddTrackingMetricGroup(metrics)
	a.deliveries[id] = d
	return nil
}

func (a *AMQPConsumer) onDelivery(track telegraf.DeliveryInfo) bool {
	delivery, ok := a.deliveries[track.ID()]
	if !ok {
		// Added by a previous connection
		return false
	}

	if track.Delivered() {
		err := delivery.Ack(false)
		if err != nil {
			a.Log.Errorf("Unable to ack written delivery: %d: %v", delivery.DeliveryTag, err)
			a.conn.Close()
		}
	} else {
		err := delivery.Reject(false)
		if err != nil {
			a.Log.Errorf("Unable to reject failed delivery: %d: %v", delivery.DeliveryTag, err)
			a.conn.Close()
		}
	}

	delete(a.deliveries, track.ID())
	return true
}

func init() {
	inputs.Add("amqp_consumer", func() telegraf.Input {
		return &AMQPConsumer{Timeout: config.Duration(30 * time.Second)}
	})
}
