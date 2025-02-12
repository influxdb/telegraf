package inlong

import (
	"context"
	"testing"
	"time"

	"github.com/apache/inlong/inlong-sdk/dataproxy-sdk-twins/dataproxy-sdk-golang/dataproxy"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/serializers/csv"
)

type MockProducer struct {
	groupID    string
	managerURL string
}

func (*MockProducer) Send(context.Context, dataproxy.Message) error {
	return nil
}

func (*MockProducer) SendAsync(context.Context, dataproxy.Message, dataproxy.Callback) {
}

func (*MockProducer) Close() {
}

func (*MockProducer) SendMessage(context.Context, dataproxy.Message) error {
	return nil
}

func NewMockProducer(groupID, managerURL string) (dataproxy.Client, error) {
	p := &MockProducer{}
	p.groupID = groupID
	p.managerURL = managerURL
	return p, nil
}

func TestInlong_Connect(t *testing.T) {
	i := &Inlong{producerFunc: NewMockProducer}
	require.NoError(t, i.Connect())
}

func TestInlong_Write(t *testing.T) {
	s := &csv.Serializer{Header: true}
	require.NoError(t, s.Init())
	producer := &MockProducer{}
	i := &Inlong{
		producer:   producer,
		serializer: s,
	}
	m := metric.New(
		"cpu",
		map[string]string{
			"topic": "test-topic",
		},
		map[string]interface{}{
			"value": 42.0,
		},
		time.Unix(0, 0),
	)
	var metrics []telegraf.Metric
	metrics = append(metrics, m)
	require.NoError(t, i.Write(metrics))
}
