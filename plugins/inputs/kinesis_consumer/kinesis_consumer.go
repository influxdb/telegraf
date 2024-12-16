//go:generate ../../../tools/readme_config_includer/generator
package kinesis_consumer

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	consumer "github.com/harlow/kinesis-consumer"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	common_aws "github.com/influxdata/telegraf/plugins/common/aws"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var once sync.Once

type KinesisConsumer struct {
	StreamName             string          `toml:"streamname"`
	ShardIteratorType      string          `toml:"shard_iterator_type"`
	DynamoDB               *dynamoDB       `toml:"checkpoint_dynamodb"`
	MaxUndeliveredMessages int             `toml:"max_undelivered_messages"`
	ContentEncoding        string          `toml:"content_encoding"`
	Log                    telegraf.Logger `toml:"-"`
	common_aws.CredentialConfig

	cons   *consumer.Consumer
	parser telegraf.Parser
	cancel context.CancelFunc
	acc    telegraf.TrackingAccumulator
	sem    chan struct{}

	iteratorStore *store

	consumed    map[string]string
	records     map[telegraf.TrackingID]iterator
	consumedTex sync.Mutex

	wg sync.WaitGroup

	contentDecodingFunc decodingFunc
}

type dynamoDB struct {
	AppName   string          `toml:"app_name"`
	TableName string          `toml:"table_name"`
	Interval  config.Duration `toml:"interval"`
}

func (*KinesisConsumer) SampleConfig() string {
	return sampleConfig
}

func (k *KinesisConsumer) Init() error {
	// Set defaults
	if k.MaxUndeliveredMessages < 1 {
		k.MaxUndeliveredMessages = 1000
	}

	if k.ShardIteratorType == "" {
		k.ShardIteratorType = "TRIM_HORIZON"
	}
	if k.ContentEncoding == "" {
		k.ContentEncoding = "identity"
	}

	f, err := getDecodingFunc(k.ContentEncoding)
	if err != nil {
		return err
	}
	k.contentDecodingFunc = f

	if k.DynamoDB != nil {
		k.iteratorStore = newStore(k.DynamoDB.AppName, k.DynamoDB.TableName, time.Duration(k.DynamoDB.Interval), k.Log)
	}
	k.consumed = make(map[string]string)

	k.records = make(map[telegraf.TrackingID]iterator, k.MaxUndeliveredMessages)
	k.sem = make(chan struct{}, k.MaxUndeliveredMessages)

	return nil
}

func (k *KinesisConsumer) SetParser(parser telegraf.Parser) {
	k.parser = parser
}

func (k *KinesisConsumer) Start(acc telegraf.Accumulator) error {
	k.acc = acc.WithTracking(k.MaxUndeliveredMessages)

	return k.connect()
}

func (k *KinesisConsumer) Gather(telegraf.Accumulator) error {
	if k.cons == nil {
		return k.connect()
	}
	return nil
}

func (k *KinesisConsumer) Stop() {
	k.cancel()
	k.wg.Wait()

	if k.iteratorStore != nil {
		k.iteratorStore.stop()
	}
}

// Interface for the (to be replaced) kinesis-consumer library
func (*KinesisConsumer) SetCheckpoint(_, _, _ string) error {
	return nil
}

func (k *KinesisConsumer) GetCheckpoint(stream, shard string) (string, error) {
	k.consumedTex.Lock()
	defer k.consumedTex.Unlock()

	seqnr, found := k.consumed[stream+"/"+shard]
	if !found && k.iteratorStore != nil {
		v, err := k.iteratorStore.get(context.Background(), stream, shard)
		if err != nil && !errors.Is(err, errNotFound) {
			return "", err
		}
		seqnr = v
	}

	return seqnr, nil
}

// Internal functions
func (k *KinesisConsumer) connect() error {
	// Start the store if necessary
	if k.iteratorStore != nil {
		if err := k.iteratorStore.run(context.Background()); err != nil {
			return fmt.Errorf("starting DynamoDB store failed: %w", err)
		}
	}

	// Setup the client to connect to the Kinesis service
	cfg, err := k.CredentialConfig.Credentials()
	if err != nil {
		return err
	}
	if k.EndpointURL != "" {
		cfg.BaseEndpoint = &k.EndpointURL
	}
	logWrapper := &telegrafLoggerWrapper{k.Log}
	cfg.Logger = logWrapper
	cfg.ClientLogMode = aws.LogRetries
	client := kinesis.NewFromConfig(cfg)

	// Setup the consumer
	cons, err := consumer.New(
		k.StreamName,
		consumer.WithClient(client),
		consumer.WithShardIteratorType(k.ShardIteratorType),
		consumer.WithStore(k),
		consumer.WithLogger(logWrapper),
	)
	if err != nil {
		return err
	}
	k.cons = cons

	ctx := context.Background()
	ctx, k.cancel = context.WithCancel(ctx)

	// Start the go-routine handling metrics delivered to the output
	k.wg.Add(1)
	go func() {
		defer k.wg.Done()
		k.onDelivery(ctx)
	}()

	// Start the go-routine handling message consumption
	k.wg.Add(1)
	go func() {
		defer k.wg.Done()
		err := k.cons.Scan(ctx, func(r *consumer.Record) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case k.sem <- struct{}{}:
				break
			}
			if err := k.onMessage(k.acc, r); err != nil {
				<-k.sem
				k.Log.Errorf("Scan parser error: %v", err)
			}

			return nil
		})
		if err != nil {
			k.cancel()
			k.Log.Errorf("Scan encountered an error: %v", err)
			k.cons = nil
		}
	}()

	return nil
}

// onMessage is called for new messages consumed from Kinesis
func (k *KinesisConsumer) onMessage(acc telegraf.TrackingAccumulator, r *consumer.Record) error {
	data, err := k.contentDecodingFunc(r.Data)
	if err != nil {
		return err
	}
	metrics, err := k.parser.Parse(data)
	if err != nil {
		return err
	}

	if len(metrics) == 0 {
		once.Do(func() {
			k.Log.Debug(internal.NoMetricsCreatedMsg)
		})
	}

	k.consumedTex.Lock()
	seqnr := *r.SequenceNumber
	id := acc.AddTrackingMetricGroup(metrics)
	k.records[id] = iterator{shard: r.ShardID, seqnr: seqnr}
	k.consumed[r.ShardID] = seqnr
	k.consumedTex.Unlock()

	return nil
}

// onDelivery is called for every metric successfully delivered to the outputs
func (k *KinesisConsumer) onDelivery(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case info := <-k.acc.Delivered():
			// Store the metric iterator in DynamoDB if configured
			if k.iteratorStore != nil {
				k.storeDelivered(info.ID())
			}

			// Reduce the number of undelivered messages by reading from the channel
			<-k.sem
		}
	}
}

func (k *KinesisConsumer) storeDelivered(id telegraf.TrackingID) {
	k.consumedTex.Lock()
	defer k.consumedTex.Unlock()

	// Find the iterator belonging to the delivered message
	iter, ok := k.records[id]
	if !ok {
		k.Log.Debugf("No iterator found for delivered metric %v!", id)
		return
	}

	// Remove metric
	delete(k.records, id)

	// Store the iterator in the database
	k.iteratorStore.set(k.StreamName, iter.shard, iter.seqnr)
}

func init() {
	inputs.Add("kinesis_consumer", func() telegraf.Input {
		return &KinesisConsumer{}
	})
}
