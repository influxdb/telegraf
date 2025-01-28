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
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"

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
	PollInterval           config.Duration `toml:"poll_interval"`
	ShardUpdateInterval    config.Duration `toml:"shard_update_interval"`
	DynamoDB               *dynamoDB       `toml:"checkpoint_dynamodb"`
	MaxUndeliveredMessages int             `toml:"max_undelivered_messages"`
	ContentEncoding        string          `toml:"content_encoding"`
	Log                    telegraf.Logger `toml:"-"`
	common_aws.CredentialConfig

	acc    telegraf.TrackingAccumulator
	parser telegraf.Parser

	cfg      aws.Config
	consumer *consumer
	cancel   context.CancelFunc
	sem      chan struct{}

	iteratorStore *store

	records    map[telegraf.TrackingID]iterator
	recordsTex sync.Mutex

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

func (k *KinesisConsumer) SetParser(parser telegraf.Parser) {
	k.parser = parser
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

	// Check input params
	if k.StreamName == "" {
		return errors.New("stream name cannot be empty")
	}

	f, err := getDecodingFunc(k.ContentEncoding)
	if err != nil {
		return err
	}
	k.contentDecodingFunc = f

	if k.DynamoDB != nil {
		if k.DynamoDB.Interval <= 0 {
			k.DynamoDB.Interval = config.Duration(10 * time.Second)
		}
		k.iteratorStore = newStore(k.DynamoDB.AppName, k.DynamoDB.TableName, time.Duration(k.DynamoDB.Interval), k.Log)
	}

	k.records = make(map[telegraf.TrackingID]iterator, k.MaxUndeliveredMessages)
	k.sem = make(chan struct{}, k.MaxUndeliveredMessages)

	// Setup the client to connect to the Kinesis service
	cfg, err := k.CredentialConfig.Credentials()
	if err != nil {
		return err
	}
	if k.EndpointURL != "" {
		cfg.BaseEndpoint = &k.EndpointURL
	}
	if k.Log.Level().Includes(telegraf.Trace) {
		logWrapper := &telegrafLoggerWrapper{k.Log}
		cfg.Logger = logWrapper
		cfg.ClientLogMode = aws.LogRetries
	}
	k.cfg = cfg

	return nil
}

func (k *KinesisConsumer) Start(acc telegraf.Accumulator) error {
	k.acc = acc.WithTracking(k.MaxUndeliveredMessages)

	// Start the store if necessary
	if k.iteratorStore != nil {
		if err := k.iteratorStore.run(context.Background()); err != nil {
			return fmt.Errorf("starting DynamoDB store failed: %w", err)
		}
	}

	ctx := context.Background()
	ctx, k.cancel = context.WithCancel(ctx)

	// Setup the consumer
	k.consumer = &consumer{
		config:              k.cfg,
		stream:              k.StreamName,
		iterType:            types.ShardIteratorType(k.ShardIteratorType),
		pollInterval:        time.Duration(k.PollInterval),
		shardUpdateInterval: time.Duration(k.ShardUpdateInterval),
		log:                 k.Log,
		onMessage: func(ctx context.Context, shard string, r *types.Record) {
			// Checking for number of messages in flight and wait for a free
			// slot in case there are too many
			select {
			case <-ctx.Done():
				return
			case k.sem <- struct{}{}:
				break
			}

			if err := k.onMessage(k.acc, shard, r); err != nil {
				seqnr := *r.SequenceNumber
				k.Log.Errorf("Processing message with sequence number %q in shard %s failed: %v", seqnr, shard, err)
				<-k.sem
			}
		},
	}

	// Link in the backing iterator store
	if k.iteratorStore != nil {
		k.consumer.position = func(shard string) string {
			seqnr, err := k.iteratorStore.get(ctx, k.StreamName, shard)
			if err != nil && !errors.Is(err, errNotFound) {
				k.Log.Errorf("retrieving sequence number for shard %q failed: %s", shard, err)
			}

			return seqnr
		}
	}
	if err := k.consumer.init(); err != nil {
		return fmt.Errorf("initializing consumer failed: %w", err)
	}

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
		k.consumer.start(ctx)
	}()

	return nil
}

func (*KinesisConsumer) Gather(telegraf.Accumulator) error {
	return nil
}

func (k *KinesisConsumer) Stop() {
	k.cancel()
	k.wg.Wait()
	k.consumer.stop()

	if k.iteratorStore != nil {
		k.iteratorStore.stop()
	}
}

// onMessage is called for new messages consumed from Kinesis
func (k *KinesisConsumer) onMessage(acc telegraf.TrackingAccumulator, shard string, r *types.Record) error {
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

	seqnr := *r.SequenceNumber

	k.recordsTex.Lock()
	defer k.recordsTex.Unlock()

	id := acc.AddTrackingMetricGroup(metrics)
	k.records[id] = iterator{shard: shard, seqnr: seqnr}

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
	k.recordsTex.Lock()
	defer k.recordsTex.Unlock()

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
		return &KinesisConsumer{
			PollInterval:        config.Duration(250 * time.Millisecond),
			ShardUpdateInterval: config.Duration(30 * time.Second),
		}
	})
}
