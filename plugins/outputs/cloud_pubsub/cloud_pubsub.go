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
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

type PubSub struct {
	CredentialsFile string            `toml:"credentials_file"`
	Project         string            `toml:"project"`
	Topic           string            `toml:"topic"`
	Attributes      map[string]string `toml:"attributes"`

	SendBatched           bool            `toml:"send_batched"`
	PublishCountThreshold int             `toml:"publish_count_threshold"`
	PublishByteThreshold  int             `toml:"publish_byte_threshold"`
	PublishNumGoroutines  int             `toml:"publish_num_go_routines"`
	PublishTimeout        config.Duration `toml:"publish_timeout"`
	Base64Data            bool            `toml:"base64_data"`
	ContentEncoding       string          `toml:"content_encoding"`

	Log telegraf.Logger `toml:"-"`

	t topic
	c *pubsub.Client

	stubTopic func(id string) topic

	serializer     telegraf.Serializer
	publishResults []publishResult
	encoder        internal.ContentEncoder
}

func (*PubSub) SampleConfig() string {
	return sampleConfig
}

func (ps *PubSub) SetSerializer(serializer telegraf.Serializer) {
	ps.serializer = serializer
}

func (ps *PubSub) Connect() error {
	if ps.stubTopic == nil {
		return ps.initPubSubClient()
	}

	return nil
}

func (ps *PubSub) Close() error {
	if ps.t != nil {
		ps.t.Stop()
	}
	return nil
}

func (ps *PubSub) Write(metrics []telegraf.Metric) error {
	ps.refreshTopic()

	// Serialize metrics and package into appropriate PubSub messages
	msgs, err := ps.toMessages(metrics)
	if err != nil {
		return err
	}

	cctx, cancel := context.WithCancel(context.Background())

	// Publish all messages - each call to Publish returns a future.
	ps.publishResults = make([]publishResult, 0, len(msgs))
	for _, m := range msgs {
		ps.publishResults = append(ps.publishResults, ps.t.Publish(cctx, m))
	}

	// topic.Stop() forces all published messages to be sent, even
	// if PubSub batch limits have not been reached.
	go ps.t.Stop()

	return ps.waitForResults(cctx, cancel)
}

func (ps *PubSub) initPubSubClient() error {
	var credsOpt option.ClientOption
	if ps.CredentialsFile != "" {
		credsOpt = option.WithCredentialsFile(ps.CredentialsFile)
	} else {
		creds, err := google.FindDefaultCredentials(context.Background(), pubsub.ScopeCloudPlatform)
		if err != nil {
			return fmt.Errorf(
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
		return fmt.Errorf("unable to generate PubSub client: %w", err)
	}
	ps.c = client
	return nil
}

func (ps *PubSub) refreshTopic() {
	if ps.stubTopic != nil {
		ps.t = ps.stubTopic(ps.Topic)
	} else {
		t := ps.c.Topic(ps.Topic)
		ps.t = &topicWrapper{t}
	}
	ps.t.SetPublishSettings(ps.publishSettings())
}

func (ps *PubSub) publishSettings() pubsub.PublishSettings {
	settings := pubsub.PublishSettings{}
	if ps.PublishNumGoroutines > 0 {
		settings.NumGoroutines = ps.PublishNumGoroutines
	}

	if time.Duration(ps.PublishTimeout) > 0 {
		settings.CountThreshold = 1
	}

	if ps.SendBatched {
		settings.CountThreshold = 1
	} else if ps.PublishCountThreshold > 0 {
		settings.CountThreshold = ps.PublishCountThreshold
	}

	if ps.PublishByteThreshold > 0 {
		settings.ByteThreshold = ps.PublishByteThreshold
	}

	return settings
}

func (ps *PubSub) toMessages(metrics []telegraf.Metric) ([]*pubsub.Message, error) {
	if ps.SendBatched {
		b, err := ps.serializer.SerializeBatch(metrics)
		if err != nil {
			return nil, err
		}

		b = ps.encodeB64Data(b)

		b, err = ps.compressData(b)
		if err != nil {
			return nil, fmt.Errorf("unable to compress message with %s: %w", ps.ContentEncoding, err)
		}

		msg := &pubsub.Message{Data: b}
		if ps.Attributes != nil {
			msg.Attributes = ps.Attributes
		}
		return []*pubsub.Message{msg}, nil
	}

	msgs := make([]*pubsub.Message, 0, len(metrics))
	for _, m := range metrics {
		b, err := ps.serializer.Serialize(m)
		if err != nil {
			ps.Log.Debugf("Could not serialize metric: %v", err)
			continue
		}

		b = ps.encodeB64Data(b)

		b, err = ps.compressData(b)
		if err != nil {
			ps.Log.Errorf("Unable to compress message with %s: %v", ps.ContentEncoding, err)
			continue
		}

		msg := &pubsub.Message{
			Data: b,
		}
		if ps.Attributes != nil {
			msg.Attributes = ps.Attributes
		}
		msgs = append(msgs, msg)
	}

	return msgs, nil
}

func (ps *PubSub) encodeB64Data(data []byte) []byte {
	if ps.Base64Data {
		encoded := base64.StdEncoding.EncodeToString(data)
		data = []byte(encoded)
	}

	return data
}

func (ps *PubSub) compressData(data []byte) ([]byte, error) {
	if ps.ContentEncoding == "identity" {
		return data, nil
	}

	data, err := ps.encoder.Encode(data)
	if err != nil {
		return nil, err
	}

	compressedData := make([]byte, len(data))
	copy(compressedData, data)
	data = compressedData

	return data, nil
}

func (ps *PubSub) waitForResults(ctx context.Context, cancel context.CancelFunc) error {
	var pErr error
	var setErr sync.Once
	var wg sync.WaitGroup

	for _, pr := range ps.publishResults {
		wg.Add(1)

		go func(r publishResult) {
			defer wg.Done()
			// Wait on each future
			_, err := r.Get(ctx)
			if err != nil {
				setErr.Do(func() {
					pErr = err
					cancel()
				})
			}
		}(pr)
	}

	wg.Wait()
	return pErr
}

func (ps *PubSub) Init() error {
	if ps.Topic == "" {
		return errors.New(`"topic" is required`)
	}

	if ps.Project == "" {
		return errors.New(`"project" is required`)
	}

	switch ps.ContentEncoding {
	case "", "identity":
		ps.ContentEncoding = "identity"
	case "gzip":
		var err error
		ps.encoder, err = internal.NewContentEncoder(ps.ContentEncoding)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid value %q for content_encoding", ps.ContentEncoding)
	}

	return nil
}

func init() {
	outputs.Add("cloud_pubsub", func() telegraf.Output {
		return &PubSub{}
	})
}
