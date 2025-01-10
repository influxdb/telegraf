//go:generate ../../../tools/readme_config_includer/generator
package kinesis_consumer

import (
	"context"
	_ "embed"
	"errors"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	consumer "github.com/harlow/kinesis-consumer"
	"github.com/harlow/kinesis-consumer/store/ddb"

	"github.com/influxdata/telegraf"
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

	checkpoint    consumer.Store
	checkpoints   map[string]checkpoint
	records       map[telegraf.TrackingID]string
	checkpointTex sync.Mutex
	recordsTex    sync.Mutex
	wg            sync.WaitGroup

	contentDecodingFunc decodingFunc

	lastSeqNum string
}

type dynamoDB struct {
	AppName   string `toml:"app_name"`
	TableName string `toml:"table_name"`
}

type checkpoint struct {
	streamName string
	shardID    string
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

	return nil
}

func (k *KinesisConsumer) SetParser(parser telegraf.Parser) {
	k.parser = parser
}

func (k *KinesisConsumer) Start(acc telegraf.Accumulator) error {
	return k.connect(acc)
}

func (k *KinesisConsumer) Gather(acc telegraf.Accumulator) error {
	if k.cons == nil {
		return k.connect(acc)
	}
	// Enforce writing of last received sequence number
	k.lastSeqNum = ""

	return nil
}

func (k *KinesisConsumer) Stop() {
	k.cancel()
	k.wg.Wait()
}

// GetCheckpoint wraps the checkpoint's GetCheckpoint function (called by consumer library)
func (k *KinesisConsumer) GetCheckpoint(streamName, shardID string) (string, error) {
	return k.checkpoint.GetCheckpoint(streamName, shardID)
}

// SetCheckpoint wraps the checkpoint's SetCheckpoint function (called by consumer library)
func (k *KinesisConsumer) SetCheckpoint(streamName, shardID, sequenceNumber string) error {
	if sequenceNumber == "" {
		return errors.New("sequence number should not be empty")
	}

	k.checkpointTex.Lock()
	k.checkpoints[sequenceNumber] = checkpoint{streamName: streamName, shardID: shardID}
	k.checkpointTex.Unlock()

	return nil
}

func (k *KinesisConsumer) connect(acc telegraf.Accumulator) error {
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

	k.checkpoint = &noopStore{}
	if k.DynamoDB != nil {
		var err error
		k.checkpoint, err = ddb.New(
			k.DynamoDB.AppName,
			k.DynamoDB.TableName,
			ddb.WithDynamoClient(dynamodb.NewFromConfig(cfg)),
			ddb.WithMaxInterval(time.Second*10),
		)
		if err != nil {
			return err
		}
	}

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

	k.acc = acc.WithTracking(k.MaxUndeliveredMessages)
	k.records = make(map[telegraf.TrackingID]string, k.MaxUndeliveredMessages)
	k.checkpoints = make(map[string]checkpoint, k.MaxUndeliveredMessages)
	k.sem = make(chan struct{}, k.MaxUndeliveredMessages)

	ctx := context.Background()
	ctx, k.cancel = context.WithCancel(ctx)

	k.wg.Add(1)
	go func() {
		defer k.wg.Done()
		k.onDelivery(ctx)
	}()

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

	k.recordsTex.Lock()
	id := acc.AddTrackingMetricGroup(metrics)
	k.records[id] = *r.SequenceNumber
	k.recordsTex.Unlock()

	return nil
}

func (k *KinesisConsumer) onDelivery(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case info := <-k.acc.Delivered():
			k.recordsTex.Lock()
			sequenceNum, ok := k.records[info.ID()]
			if !ok {
				k.recordsTex.Unlock()
				continue
			}
			<-k.sem
			delete(k.records, info.ID())
			k.recordsTex.Unlock()

			if !info.Delivered() {
				k.Log.Debug("Metric group failed to process")
				continue
			}

			if k.lastSeqNum != "" {
				continue
			}

			// Store the sequence number at least once per gather cycle using the checkpoint
			// storage (usually DynamoDB).
			k.checkpointTex.Lock()
			chk, ok := k.checkpoints[sequenceNum]
			if !ok {
				k.checkpointTex.Unlock()
				continue
			}
			delete(k.checkpoints, sequenceNum)
			k.checkpointTex.Unlock()

			k.Log.Tracef("persisting sequence number %q for stream %q and shard %q", sequenceNum)
			k.lastSeqNum = sequenceNum
			if err := k.checkpoint.SetCheckpoint(chk.streamName, chk.shardID, sequenceNum); err != nil {
				k.Log.Errorf("Setting checkpoint failed: %v", err)
			}
		}
	}
}

func init() {
	inputs.Add("kinesis_consumer", func() telegraf.Input {
		return &KinesisConsumer{}
	})
}
