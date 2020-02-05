package kafka

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/serializers"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

type topicSuffixTestpair struct {
	topicSuffix   TopicSuffix
	expectedTopic string
}

type TopicRoutingTestPair struct {
	routingRules  []TopicRouting
	expectedTopic string
}

func TestConnectAndWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	brokers := []string{testutil.GetLocalHost() + ":9092"}
	s, _ := serializers.NewInfluxSerializer()
	k := &Kafka{
		Brokers:    brokers,
		Topic:      "Test",
		serializer: s,
	}

	// Verify that we can connect to the Kafka broker
	err := k.Connect()
	require.NoError(t, err)

	// Verify that we can successfully write data to the kafka broker
	err = k.Write(testutil.MockMetrics())
	require.NoError(t, err)
	k.Close()
}

func TestTopicSuffixes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	topic := "Test"

	metric := testutil.TestMetric(1)
	metricTagName := "tag1"
	metricTagValue := metric.Tags()[metricTagName]
	metricName := metric.Name()

	var testcases = []topicSuffixTestpair{
		// This ensures empty separator is okay
		{TopicSuffix{Method: "measurement"},
			topic + metricName},
		{TopicSuffix{Method: "measurement", Separator: "sep"},
			topic + "sep" + metricName},
		{TopicSuffix{Method: "tags", Keys: []string{metricTagName}, Separator: "_"},
			topic + "_" + metricTagValue},
		{TopicSuffix{Method: "tags", Keys: []string{metricTagName, metricTagName, metricTagName}, Separator: "___"},
			topic + "___" + metricTagValue + "___" + metricTagValue + "___" + metricTagValue},
		{TopicSuffix{Method: "tags", Keys: []string{metricTagName, metricTagName, metricTagName}},
			topic + metricTagValue + metricTagValue + metricTagValue},
		// This ensures non-existing tags are ignored
		{TopicSuffix{Method: "tags", Keys: []string{"non_existing_tag", "non_existing_tag"}, Separator: "___"},
			topic},
		{TopicSuffix{Method: "tags", Keys: []string{metricTagName, "non_existing_tag"}, Separator: "___"},
			topic + "___" + metricTagValue},
		// This ensures backward compatibility
		{TopicSuffix{},
			topic},
	}

	for _, testcase := range testcases {
		topicSuffix := testcase.topicSuffix
		expectedTopic := testcase.expectedTopic
		k := &Kafka{
			Topic:       topic,
			TopicSuffix: topicSuffix,
		}

		topic := k.GetTopicName(metric)
		require.Equal(t, expectedTopic, topic)
	}
}
func TestKafkaTopicRouting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	topic := "FallbackTopic"
	metric := testutil.TestMetric(1)
	metric.SetName("test_measurement_1")

	var testcases = []TopicRoutingTestPair{
		{[]TopicRouting{
			TopicRouting{Method: "measurement", MatchType: "exact", MatchValue: []string{"test_measurement_1"}, Topic: "measurement_topic_1"}},
			"measurement_topic_1"},
		{[]TopicRouting{
			TopicRouting{Method: "measurement", MatchType: "substring", MatchValue: []string{"measurement_1"}, Topic: "measurement_topic_2"}},
			"measurement_topic_2"},
		{[]TopicRouting{
			TopicRouting{Method: "measurement", MatchType: "exact", MatchValue: []string{"failed_match"}, Topic: "measurement_topic_2"}},
			"FallbackTopic"},
		{[]TopicRouting{
			TopicRouting{Method: "measurement", MatchType: "substring", MatchValue: []string{"failed_match"}, Topic: "measurement_topic_2"}},
			"FallbackTopic"},
		{[]TopicRouting{
			TopicRouting{Method: "measurement", MatchType: "exact", MatchValue: []string{"failed_exact_match"}, Topic: "measurement_topic_1"},
			TopicRouting{Method: "measurement", MatchType: "substring", MatchValue: []string{"measurement_1"}, Topic: "second_rule_match_success"}},
			"second_rule_match_success"},
		{[]TopicRouting{
			TopicRouting{Method: "measurement", MatchType: "substring", MatchValue: []string{"measurement_1"}, Topic: "first_rule_match_success"},
			TopicRouting{Method: "measurement", MatchType: "exact", MatchValue: []string{"test_measurement_1"}, Topic: "measurement_topic_2"}},
			"first_rule_match_success"},
		// This ensures backward compatibility
		{[]TopicRouting{},
			"FallbackTopic"},
	}

	for _, testcase := range testcases {
		TopicRouting := testcase.routingRules
		expectedTopic := testcase.expectedTopic

		k := &Kafka{
			Topic:             topic,
			TopicRoutingRules: TopicRouting,
		}

		topic := k.GetTopicName(metric)
		require.Equal(t, expectedTopic, topic)
	}
}

func TestValidateTopicSuffixMethod(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	err := ValidateTopicSuffixMethod("invalid_topic_suffix_method")
	require.Error(t, err, "Topic suffix method used should be invalid.")

	for _, method := range ValidTopicSuffixMethods {
		err := ValidateTopicSuffixMethod(method)
		require.NoError(t, err, "Topic suffix method used should be valid.")
	}
}

func TestRoutingKey(t *testing.T) {
	tests := []struct {
		name   string
		kafka  *Kafka
		metric telegraf.Metric
		check  func(t *testing.T, routingKey string)
	}{
		{
			name: "static routing key",
			kafka: &Kafka{
				RoutingKey: "static",
			},
			metric: func() telegraf.Metric {
				m, _ := metric.New(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 0),
				)
				return m
			}(),
			check: func(t *testing.T, routingKey string) {
				require.Equal(t, "static", routingKey)
			},
		},
		{
			name: "random routing key",
			kafka: &Kafka{
				RoutingKey: "random",
			},
			metric: func() telegraf.Metric {
				m, _ := metric.New(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"value": 42.0,
					},
					time.Unix(0, 0),
				)
				return m
			}(),
			check: func(t *testing.T, routingKey string) {
				require.Equal(t, 36, len(routingKey))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := tt.kafka.routingKey(tt.metric)
			require.NoError(t, err)
			tt.check(t, key)
		})
	}
}
