//go:generate ../../../tools/readme_config_includer/generator
package kinesis

import (
	"context"
	_ "embed"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/gofrs/uuid/v5"

	"github.com/influxdata/telegraf"
	common_aws "github.com/influxdata/telegraf/plugins/common/aws"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

// Limit set by AWS (https://docs.aws.amazon.com/kinesis/latest/APIReference/API_PutRecords.html)
const maxRecordsPerRequest uint32 = 500

type (
	KinesisOutput struct {
		StreamName         string     `toml:"streamname"`
		PartitionKey       string     `toml:"partitionkey" deprecated:"1.5.0;1.35.0;use 'partition.key' instead"`
		RandomPartitionKey bool       `toml:"use_random_partitionkey" deprecated:"1.5.0;1.35.0;use 'partition.method' instead"`
		Partition          *Partition `toml:"partition"`
		Debug              bool       `toml:"debug"`

		Log        telegraf.Logger `toml:"-"`
		serializer telegraf.Serializer
		svc        kinesisClient

		common_aws.CredentialConfig
	}

	Partition struct {
		Method  string `toml:"method"`
		Key     string `toml:"key"`
		Default string `toml:"default"`
	}
)

type kinesisClient interface {
	PutRecords(context.Context, *kinesis.PutRecordsInput, ...func(*kinesis.Options)) (*kinesis.PutRecordsOutput, error)
}

func (*KinesisOutput) SampleConfig() string {
	return sampleConfig
}

func (k *KinesisOutput) Connect() error {
	if k.Partition == nil {
		k.Log.Error("Deprecated partitionkey configuration in use, please consider using outputs.kinesis.partition")
	}

	// We attempt first to create a session to Kinesis using an IAMS role, if that fails it will fall through to using
	// environment variables, and then Shared Credentials.
	if k.Debug {
		k.Log.Infof("Establishing a connection to Kinesis in %s", k.Region)
	}

	cfg, err := k.CredentialConfig.Credentials()
	if err != nil {
		return err
	}

	if k.EndpointURL != "" {
		cfg.BaseEndpoint = &k.EndpointURL
	}

	svc := kinesis.NewFromConfig(cfg)

	_, err = svc.DescribeStreamSummary(context.Background(), &kinesis.DescribeStreamSummaryInput{
		StreamName: aws.String(k.StreamName),
	})
	k.svc = svc
	return err
}

func (k *KinesisOutput) Close() error {
	return nil
}

func (k *KinesisOutput) SetSerializer(serializer telegraf.Serializer) {
	k.serializer = serializer
}

func (k *KinesisOutput) writeKinesis(r []types.PutRecordsRequestEntry) time.Duration {
	start := time.Now()
	payload := &kinesis.PutRecordsInput{
		Records:    r,
		StreamName: aws.String(k.StreamName),
	}

	resp, err := k.svc.PutRecords(context.Background(), payload)
	if err != nil {
		k.Log.Errorf("Unable to write to Kinesis : %s", err.Error())
		return time.Since(start)
	}

	if k.Debug {
		k.Log.Infof("Wrote: '%+v'", resp)
	}

	failed := *resp.FailedRecordCount
	if failed > 0 {
		k.Log.Errorf("Unable to write %+v of %+v record(s) to Kinesis", failed, len(r))
	}

	return time.Since(start)
}

func (k *KinesisOutput) getPartitionKey(metric telegraf.Metric) string {
	if k.Partition != nil {
		switch k.Partition.Method {
		case "static":
			return k.Partition.Key
		case "random":
			u, err := uuid.NewV4()
			if err != nil {
				return k.Partition.Default
			}
			return u.String()
		case "measurement":
			return metric.Name()
		case "tag":
			if t, ok := metric.GetTag(k.Partition.Key); ok {
				return t
			} else if len(k.Partition.Default) > 0 {
				return k.Partition.Default
			}
			// Default partition name if default is not set
			return "telegraf"
		default:
			k.Log.Errorf("You have configured a Partition method of %q which is not supported", k.Partition.Method)
		}
	}
	if k.RandomPartitionKey {
		u, err := uuid.NewV4()
		if err != nil {
			return k.Partition.Default
		}
		return u.String()
	}
	return k.PartitionKey
}

func (k *KinesisOutput) Write(metrics []telegraf.Metric) error {
	var sz uint32

	if len(metrics) == 0 {
		return nil
	}

	r := make([]types.PutRecordsRequestEntry, 0, len(metrics))
	for _, metric := range metrics {
		sz++

		values, err := k.serializer.Serialize(metric)
		if err != nil {
			k.Log.Debugf("Could not serialize metric: %v", err)
			continue
		}

		partitionKey := k.getPartitionKey(metric)

		d := types.PutRecordsRequestEntry{
			Data:         values,
			PartitionKey: aws.String(partitionKey),
		}

		r = append(r, d)
		if sz == maxRecordsPerRequest {
			elapsed := k.writeKinesis(r)
			k.Log.Debugf("Wrote a %d point batch to Kinesis in %+v.", sz, elapsed)
			sz = 0
			r = nil
		}
	}
	if sz > 0 {
		elapsed := k.writeKinesis(r)
		k.Log.Debugf("Wrote a %d point batch to Kinesis in %+v.", sz, elapsed)
	}

	return nil
}

func init() {
	outputs.Add("kinesis", func() telegraf.Output {
		return &KinesisOutput{}
	})
}
