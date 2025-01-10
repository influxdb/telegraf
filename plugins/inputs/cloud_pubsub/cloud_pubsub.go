//go:generate ../../../tools/readme_config_includer/generator
package cloud_pubsub

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"cloud.google.com/go/pubsub"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var once sync.Once

const (
	defaultMaxUndeliveredMessages = 1000
	defaultRetryDelaySeconds      = 5
)

type PubSub struct {
	sync.Mutex

	CredentialsFile string `toml:"credentials_file"`
	Project         string `toml:"project"`
	Subscription    string `toml:"subscription"`

	// Subscription ReceiveSettings
	MaxExtension           config.Duration `toml:"max_extension"`
	MaxOutstandingMessages int             `toml:"max_outstanding_messages"`
	MaxOutstandingBytes    int             `toml:"max_outstanding_bytes"`
	MaxReceiverGoRoutines  int             `toml:"max_receiver_go_routines"`

	// Agent settings
	MaxMessageLen            int `toml:"max_message_len"`
	MaxUndeliveredMessages   int `toml:"max_undelivered_messages"`
	RetryReceiveDelaySeconds int `toml:"retry_delay_seconds"`

	Base64Data bool `toml:"base64_data"`

	ContentEncoding      string          `toml:"content_encoding"`
	MaxDecompressionSize config.Size     `toml:"max_decompression_size"`
	Log                  telegraf.Logger `toml:"-"`

	sub     subscription
	stubSub func() subscription

	cancel context.CancelFunc

	parser telegraf.Parser
	wg     *sync.WaitGroup
	acc    telegraf.TrackingAccumulator

	undelivered  map[telegraf.TrackingID]message
	sem          semaphore
	decoder      internal.ContentDecoder
	decoderMutex sync.Mutex
}

type (
	empty     struct{}
	semaphore chan empty
)

func (*PubSub) SampleConfig() string {
	return sampleConfig
}

func (ps *PubSub) Init() error {
	if ps.Subscription == "" {
		return errors.New(`"subscription" is required`)
	}

	if ps.Project == "" {
		return errors.New(`"project" is required`)
	}

	switch ps.ContentEncoding {
	case "", "identity":
		ps.ContentEncoding = "identity"
	case "gzip":
		var err error
		var options []internal.DecodingOption
		if ps.MaxDecompressionSize > 0 {
			options = append(options, internal.WithMaxDecompressionSize(int64(ps.MaxDecompressionSize)))
		}
		ps.decoder, err = internal.NewContentDecoder(ps.ContentEncoding, options...)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid value %q for content_encoding", ps.ContentEncoding)
	}

	return nil
}

// SetParser implements ParserInput interface.
func (ps *PubSub) SetParser(parser telegraf.Parser) {
	ps.parser = parser
}

// Start initializes the plugin and processing messages from Google PubSub.
// Two goroutines are started - one pulling for the subscription, one
// receiving delivery notifications from the accumulator.
func (ps *PubSub) Start(ac telegraf.Accumulator) error {
	ps.sem = make(semaphore, ps.MaxUndeliveredMessages)
	ps.acc = ac.WithTracking(ps.MaxUndeliveredMessages)

	// Create top-level context with cancel that will be called on Stop().
	ctx, cancel := context.WithCancel(context.Background())
	ps.cancel = cancel

	if ps.stubSub != nil {
		ps.sub = ps.stubSub()
	} else {
		subRef, err := ps.getGCPSubscription(ps.Subscription)
		if err != nil {
			return fmt.Errorf("unable to create subscription handle: %w", err)
		}
		ps.sub = subRef
	}

	ps.wg = &sync.WaitGroup{}
	// Start goroutine to handle delivery notifications from accumulator.
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		ps.waitForDelivery(ctx)
	}()

	// Start goroutine for subscription receiver.
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		ps.receiveWithRetry(ctx)
	}()

	return nil
}

// Gather does nothing for this service input.
func (*PubSub) Gather(telegraf.Accumulator) error {
	return nil
}

// Stop ensures the PubSub subscriptions receivers are stopped by
// canceling the context and waits for goroutines to finish.
func (ps *PubSub) Stop() {
	ps.cancel()
	ps.wg.Wait()
}

// startReceiver is called within a goroutine and manages keeping a
// subscription.Receive() up and running while the plugin has not been stopped.
func (ps *PubSub) receiveWithRetry(parentCtx context.Context) {
	err := ps.startReceiver(parentCtx)

	for err != nil && parentCtx.Err() == nil {
		ps.Log.Errorf("Receiver for subscription %s exited with error: %v", ps.sub.ID(), err)

		delay := defaultRetryDelaySeconds
		if ps.RetryReceiveDelaySeconds > 0 {
			delay = ps.RetryReceiveDelaySeconds
		}

		ps.Log.Infof("Waiting %d seconds before attempting to restart receiver...", delay)
		time.Sleep(time.Duration(delay) * time.Second)

		err = ps.startReceiver(parentCtx)
	}
}

func (ps *PubSub) startReceiver(parentCtx context.Context) error {
	ps.Log.Infof("Starting receiver for subscription %s...", ps.sub.ID())
	cctx, ccancel := context.WithCancel(parentCtx)
	err := ps.sub.Receive(cctx, func(ctx context.Context, msg message) {
		if err := ps.onMessage(ctx, msg); err != nil {
			ps.acc.AddError(fmt.Errorf("unable to add message from subscription %s: %w", ps.sub.ID(), err))
		}
	})
	if err != nil {
		ps.acc.AddError(fmt.Errorf("receiver for subscription %s exited: %w", ps.sub.ID(), err))
	} else {
		ps.Log.Info("Subscription pull ended (no error, most likely stopped)")
	}
	ccancel()
	return err
}

// onMessage handles parsing and adding a received message to the accumulator.
func (ps *PubSub) onMessage(ctx context.Context, msg message) error {
	if ps.MaxMessageLen > 0 && len(msg.Data()) > ps.MaxMessageLen {
		msg.Ack()
		return fmt.Errorf("message longer than max_message_len (%d > %d)", len(msg.Data()), ps.MaxMessageLen)
	}

	data, err := ps.decompressData(msg.Data())
	if err != nil {
		return fmt.Errorf("unable to decompress %s message: %w", ps.ContentEncoding, err)
	}

	data, err = ps.decodeB64Data(data)
	if err != nil {
		return fmt.Errorf("unable to decode base64 message: %w", err)
	}

	metrics, err := ps.parser.Parse(data)
	if err != nil {
		msg.Ack()
		return fmt.Errorf("unable to parse message: %w", err)
	}

	if len(metrics) == 0 {
		msg.Ack()

		once.Do(func() {
			ps.Log.Debug(internal.NoMetricsCreatedMsg)
		})

		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case ps.sem <- empty{}:
		break
	}

	ps.Lock()
	defer ps.Unlock()

	id := ps.acc.AddTrackingMetricGroup(metrics)
	if ps.undelivered == nil {
		ps.undelivered = make(map[telegraf.TrackingID]message)
	}
	ps.undelivered[id] = msg

	return nil
}

func (ps *PubSub) decompressData(data []byte) ([]byte, error) {
	if ps.ContentEncoding == "identity" {
		return data, nil
	}

	ps.decoderMutex.Lock()
	defer ps.decoderMutex.Unlock()
	data, err := ps.decoder.Decode(data)
	if err != nil {
		return nil, err
	}

	decompressedData := make([]byte, len(data))
	copy(decompressedData, data)
	data = decompressedData

	return data, nil
}

func (ps *PubSub) decodeB64Data(data []byte) ([]byte, error) {
	if ps.Base64Data {
		return base64.StdEncoding.DecodeString(string(data))
	}

	return data, nil
}

func (ps *PubSub) waitForDelivery(parentCtx context.Context) {
	for {
		select {
		case <-parentCtx.Done():
			return
		case info := <-ps.acc.Delivered():
			<-ps.sem
			msg := ps.removeDelivered(info.ID())

			if msg != nil {
				msg.Ack()
			}
		}
	}
}

func (ps *PubSub) removeDelivered(id telegraf.TrackingID) message {
	ps.Lock()
	defer ps.Unlock()

	msg, ok := ps.undelivered[id]
	if !ok {
		return nil
	}
	delete(ps.undelivered, id)
	return msg
}

func (ps *PubSub) getPubSubClient() (*pubsub.Client, error) {
	var credsOpt option.ClientOption
	if ps.CredentialsFile != "" {
		credsOpt = option.WithCredentialsFile(ps.CredentialsFile)
	} else {
		creds, err := google.FindDefaultCredentials(context.Background(), pubsub.ScopeCloudPlatform)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to find GCP Application Default Credentials: %v."+
					"Either set ADC or provide CredentialsFile config", err)
		}
		credsOpt = option.WithCredentials(creds)
	}
	client, err := pubsub.NewClient(
		context.Background(),
		ps.Project,
		credsOpt,
		option.WithScopes(pubsub.ScopeCloudPlatform),
		option.WithUserAgent(internal.ProductToken()),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to generate PubSub client: %w", err)
	}
	return client, nil
}

func (ps *PubSub) getGCPSubscription(subID string) (subscription, error) {
	client, err := ps.getPubSubClient()
	if err != nil {
		return nil, err
	}
	s := client.Subscription(subID)
	s.ReceiveSettings = pubsub.ReceiveSettings{
		NumGoroutines:          ps.MaxReceiverGoRoutines,
		MaxExtension:           time.Duration(ps.MaxExtension),
		MaxOutstandingMessages: ps.MaxOutstandingMessages,
		MaxOutstandingBytes:    ps.MaxOutstandingBytes,
	}
	return &gcpSubscription{s}, nil
}

func init() {
	inputs.Add("cloud_pubsub", func() telegraf.Input {
		ps := &PubSub{
			MaxUndeliveredMessages: defaultMaxUndeliveredMessages,
		}
		return ps
	})
}
