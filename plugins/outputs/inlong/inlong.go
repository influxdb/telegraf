package inlong

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/apache/inlong/inlong-sdk/dataproxy-sdk-twins/dataproxy-sdk-golang/dataproxy"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

const managerURLSuffix = "/inlong/manager/openapi/dataproxy/getIpList"

type Inlong struct {
	GroupID    string          `toml:"group_id"`
	StreamID   string          `toml:"stream_id"`
	ManagerURL string          `toml:"manager_url"`
	Log        telegraf.Logger `toml:"-"`

	producerFunc func(groupId string, managerUrl string) (dataproxy.Client, error)
	producer     dataproxy.Client
	serializer   telegraf.Serializer
}

func (*Inlong) SampleConfig() string {
	return sampleConfig
}

func (i *Inlong) SetSerializer(serializer telegraf.Serializer) {
	i.serializer = serializer
}

func (i *Inlong) Connect() error {
	producer, err := i.producerFunc(i.GroupID, i.ManagerURL+managerURLSuffix)
	if err != nil {
		return &internal.StartupError{Err: err, Retry: true}
	}
	i.producer = producer
	return nil
}

func (i *Inlong) Close() error {
	i.producer.Close()
	return nil
}

func (i *Inlong) Write(metrics []telegraf.Metric) error {
	for _, metric := range metrics {
		b, err := i.serializer.Serialize(metric)
		if err != nil {
			return fmt.Errorf("could not serialize metric: %w", err)
		}
		err = i.producer.Send(context.Background(), dataproxy.Message{
			GroupID:  i.GroupID,
			StreamID: i.StreamID,
			Payload:  b,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func init() {
	outputs.Add("inlong", func() telegraf.Output {
		return &Inlong{
			producerFunc: newProducer,
		}
	})
}

func newProducer(groupID, managerURL string) (dataproxy.Client, error) {
	producer, err := dataproxy.NewClient(
		dataproxy.WithGroupID(groupID),
		dataproxy.WithURL(managerURL),
	)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	return producer, nil
}
