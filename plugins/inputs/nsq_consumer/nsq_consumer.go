//go:generate ../../../tools/readme_config_includer/generator
package nsq_consumer

import (
	"context"
	_ "embed"
	"errors"
	"sync"

	"github.com/nsqio/go-nsq"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	defaultMaxUndeliveredMessages = 1000
)

type NSQConsumer struct {
	Server                 string          `toml:"server" deprecated:"1.5.0;1.35.0;use 'nsqd' instead"`
	Nsqd                   []string        `toml:"nsqd"`
	Nsqlookupd             []string        `toml:"nsqlookupd"`
	Topic                  string          `toml:"topic"`
	Channel                string          `toml:"channel"`
	MaxInFlight            int             `toml:"max_in_flight"`
	MaxUndeliveredMessages int             `toml:"max_undelivered_messages"`
	Log                    telegraf.Logger `toml:"-"`

	parser   telegraf.Parser
	consumer *nsq.Consumer

	mu       sync.Mutex
	messages map[telegraf.TrackingID]*nsq.Message
	wg       sync.WaitGroup
	cancel   context.CancelFunc
}

type (
	empty     struct{}
	semaphore chan empty
)

type logger struct {
	log telegraf.Logger
}

func (l *logger) Output(_ int, s string) error {
	l.log.Debug(s)
	return nil
}

func (*NSQConsumer) SampleConfig() string {
	return sampleConfig
}

func (n *NSQConsumer) Init() error {
	// For backward compatibility
	if n.Server != "" {
		n.Nsqd = append(n.Nsqd, n.Server)
	}

	// Check if we have anything to connect to
	if len(n.Nsqlookupd) == 0 && len(n.Nsqd) == 0 {
		return errors.New("either 'nsqd' or 'nsqlookupd' needs to be specified")
	}

	return nil
}

// SetParser takes the data_format from the config and finds the right parser for that format
func (n *NSQConsumer) SetParser(parser telegraf.Parser) {
	n.parser = parser
}

func (n *NSQConsumer) Start(ac telegraf.Accumulator) error {
	acc := ac.WithTracking(n.MaxUndeliveredMessages)
	sem := make(semaphore, n.MaxUndeliveredMessages)
	n.messages = make(map[telegraf.TrackingID]*nsq.Message, n.MaxUndeliveredMessages)

	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel

	if err := n.connect(); err != nil {
		return err
	}
	n.consumer.SetLogger(&logger{log: n.Log}, nsq.LogLevelInfo)
	n.consumer.AddHandler(nsq.HandlerFunc(func(message *nsq.Message) error {
		metrics, err := n.parser.Parse(message.Body)
		if err != nil {
			acc.AddError(err)
			// Remove the message from the queue
			message.Finish()
			return nil
		}
		if len(metrics) == 0 {
			message.Finish()
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case sem <- empty{}:
			break
		}

		n.mu.Lock()
		id := acc.AddTrackingMetricGroup(metrics)
		n.messages[id] = message
		n.mu.Unlock()
		message.DisableAutoResponse()
		return nil
	}))

	if len(n.Nsqlookupd) > 0 {
		err := n.consumer.ConnectToNSQLookupds(n.Nsqlookupd)
		if err != nil && !errors.Is(err, nsq.ErrAlreadyConnected) {
			return err
		}
	}

	if len(n.Nsqd) > 0 {
		err := n.consumer.ConnectToNSQDs(n.Nsqd)
		if err != nil && !errors.Is(err, nsq.ErrAlreadyConnected) {
			return err
		}
	}

	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		n.onDelivery(ctx, acc, sem)
	}()
	return nil
}

func (*NSQConsumer) Gather(telegraf.Accumulator) error {
	return nil
}

func (n *NSQConsumer) Stop() {
	n.cancel()
	n.wg.Wait()
	n.consumer.Stop()
	<-n.consumer.StopChan
}

func (n *NSQConsumer) onDelivery(ctx context.Context, acc telegraf.TrackingAccumulator, sem semaphore) {
	for {
		select {
		case <-ctx.Done():
			return
		case info := <-acc.Delivered():
			n.mu.Lock()
			msg, ok := n.messages[info.ID()]
			if !ok {
				n.mu.Unlock()
				continue
			}
			<-sem
			delete(n.messages, info.ID())
			n.mu.Unlock()

			if info.Delivered() {
				msg.Finish()
			} else {
				msg.Requeue(-1)
			}
		}
	}
}

func (n *NSQConsumer) connect() error {
	if n.consumer == nil {
		config := nsq.NewConfig()
		config.MaxInFlight = n.MaxInFlight
		consumer, err := nsq.NewConsumer(n.Topic, n.Channel, config)
		if err != nil {
			return err
		}
		n.consumer = consumer
	}
	return nil
}

func init() {
	inputs.Add("nsq_consumer", func() telegraf.Input {
		return &NSQConsumer{
			MaxUndeliveredMessages: defaultMaxUndeliveredMessages,
		}
	})
}
