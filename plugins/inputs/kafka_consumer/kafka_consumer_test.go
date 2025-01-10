package kafka_consumer

import (
	"context"
	"fmt"
	"math"
	"net"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	kafkacontainer "github.com/testcontainers/testcontainers-go/modules/kafka"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/plugins/common/kafka"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/outputs"
	outputs_kafka "github.com/influxdata/telegraf/plugins/outputs/kafka"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/plugins/parsers/value"
	serializers_influx "github.com/influxdata/telegraf/plugins/serializers/influx"
	"github.com/influxdata/telegraf/testutil"
)

type fakeConsumerGroup struct {
	brokers []string
	group   string
	config  *sarama.Config

	handler sarama.ConsumerGroupHandler
	errors  chan error
}

func (g *fakeConsumerGroup) Consume(_ context.Context, _ []string, handler sarama.ConsumerGroupHandler) error {
	g.handler = handler
	return g.handler.Setup(nil)
}

func (g *fakeConsumerGroup) Errors() <-chan error {
	return g.errors
}

func (g *fakeConsumerGroup) Close() error {
	close(g.errors)
	return nil
}

type fakeCreator struct {
	consumerGroup *fakeConsumerGroup
}

func (c *fakeCreator) create(brokers []string, group string, cfg *sarama.Config) (consumerGroup, error) {
	c.consumerGroup.brokers = brokers
	c.consumerGroup.group = group
	c.consumerGroup.config = cfg
	return c.consumerGroup, nil
}

func TestInit(t *testing.T) {
	tests := []struct {
		name      string
		plugin    *KafkaConsumer
		initError bool
		check     func(t *testing.T, plugin *KafkaConsumer)
	}{
		{
			name:   "default config",
			plugin: &KafkaConsumer{Log: testutil.Logger{}},
			check: func(t *testing.T, plugin *KafkaConsumer) {
				require.Equal(t, defaultConsumerGroup, plugin.ConsumerGroup)
				require.Equal(t, defaultMaxUndeliveredMessages, plugin.MaxUndeliveredMessages)
				require.Equal(t, "Telegraf", plugin.config.ClientID)
				require.Equal(t, sarama.OffsetOldest, plugin.config.Consumer.Offsets.Initial)
				require.Equal(t, 100*time.Millisecond, plugin.config.Consumer.MaxProcessingTime)
			},
		},
		{
			name: "parses valid version string",
			plugin: &KafkaConsumer{
				ReadConfig: kafka.ReadConfig{
					Config: kafka.Config{
						Version: "1.0.0",
					},
				},
				Log: testutil.Logger{},
			},
			check: func(t *testing.T, plugin *KafkaConsumer) {
				require.Equal(t, plugin.config.Version, sarama.V1_0_0_0)
			},
		},
		{
			name: "invalid version string",
			plugin: &KafkaConsumer{
				ReadConfig: kafka.ReadConfig{
					Config: kafka.Config{
						Version: "100",
					},
				},
				Log: testutil.Logger{},
			},
			initError: true,
		},
		{
			name: "custom client_id",
			plugin: &KafkaConsumer{
				ReadConfig: kafka.ReadConfig{
					Config: kafka.Config{
						ClientID: "custom",
					},
				},
				Log: testutil.Logger{},
			},
			check: func(t *testing.T, plugin *KafkaConsumer) {
				require.Equal(t, "custom", plugin.config.ClientID)
			},
		},
		{
			name: "custom offset",
			plugin: &KafkaConsumer{
				Offset: "newest",
				Log:    testutil.Logger{},
			},
			check: func(t *testing.T, plugin *KafkaConsumer) {
				require.Equal(t, sarama.OffsetNewest, plugin.config.Consumer.Offsets.Initial)
			},
		},
		{
			name: "invalid offset",
			plugin: &KafkaConsumer{
				Offset: "middle",
				Log:    testutil.Logger{},
			},
			initError: true,
		},
		{
			name: "default tls without tls config",
			plugin: &KafkaConsumer{
				Log: testutil.Logger{},
			},
			check: func(t *testing.T, plugin *KafkaConsumer) {
				require.False(t, plugin.config.Net.TLS.Enable)
			},
		},
		{
			name: "enabled tls without tls config",
			plugin: &KafkaConsumer{
				ReadConfig: kafka.ReadConfig{
					Config: kafka.Config{
						EnableTLS: func(b bool) *bool { return &b }(true),
					},
				},
				Log: testutil.Logger{},
			},
			check: func(t *testing.T, plugin *KafkaConsumer) {
				require.True(t, plugin.config.Net.TLS.Enable)
			},
		},
		{
			name: "default tls with a tls config",
			plugin: &KafkaConsumer{
				ReadConfig: kafka.ReadConfig{
					Config: kafka.Config{
						ClientConfig: tls.ClientConfig{
							InsecureSkipVerify: true,
						},
					},
				},
				Log: testutil.Logger{},
			},
			check: func(t *testing.T, plugin *KafkaConsumer) {
				require.True(t, plugin.config.Net.TLS.Enable)
			},
		},
		{
			name: "Insecure tls",
			plugin: &KafkaConsumer{
				ReadConfig: kafka.ReadConfig{
					Config: kafka.Config{
						ClientConfig: tls.ClientConfig{
							InsecureSkipVerify: true,
						},
					},
				},
				Log: testutil.Logger{},
			},
			check: func(t *testing.T, plugin *KafkaConsumer) {
				require.True(t, plugin.config.Net.TLS.Enable)
			},
		},
		{
			name: "custom max_processing_time",
			plugin: &KafkaConsumer{
				MaxProcessingTime: config.Duration(1000 * time.Millisecond),
				Log:               testutil.Logger{},
			},
			check: func(t *testing.T, plugin *KafkaConsumer) {
				require.Equal(t, 1000*time.Millisecond, plugin.config.Consumer.MaxProcessingTime)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cg := &fakeConsumerGroup{}
			tt.plugin.consumerCreator = &fakeCreator{consumerGroup: cg}
			err := tt.plugin.Init()
			if tt.initError {
				require.Error(t, err)
				return
			}
			// No error path
			require.NoError(t, err)

			tt.check(t, tt.plugin)
		})
	}
}

func TestStartStop(t *testing.T) {
	cg := &fakeConsumerGroup{errors: make(chan error)}
	plugin := &KafkaConsumer{
		consumerCreator: &fakeCreator{consumerGroup: cg},
		Log:             testutil.Logger{},
	}
	err := plugin.Init()
	require.NoError(t, err)

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))

	plugin.Stop()
}

type FakeConsumerGroupSession struct {
	ctx context.Context
}

func (*FakeConsumerGroupSession) Claims() map[string][]int32 {
	panic("not implemented")
}

func (*FakeConsumerGroupSession) MemberID() string {
	panic("not implemented")
}

func (*FakeConsumerGroupSession) GenerationID() int32 {
	panic("not implemented")
}

func (*FakeConsumerGroupSession) MarkOffset(string, int32, int64, string) {
	panic("not implemented")
}

func (*FakeConsumerGroupSession) ResetOffset(string, int32, int64, string) {
	panic("not implemented")
}

func (*FakeConsumerGroupSession) MarkMessage(*sarama.ConsumerMessage, string) {
}

func (s *FakeConsumerGroupSession) Context() context.Context {
	return s.ctx
}

func (*FakeConsumerGroupSession) Commit() {
}

type FakeConsumerGroupClaim struct {
	messages chan *sarama.ConsumerMessage
}

func (*FakeConsumerGroupClaim) Topic() string {
	panic("not implemented")
}

func (*FakeConsumerGroupClaim) Partition() int32 {
	panic("not implemented")
}

func (*FakeConsumerGroupClaim) InitialOffset() int64 {
	panic("not implemented")
}

func (*FakeConsumerGroupClaim) HighWaterMarkOffset() int64 {
	panic("not implemented")
}

func (c *FakeConsumerGroupClaim) Messages() <-chan *sarama.ConsumerMessage {
	return c.messages
}

func TestConsumerGroupHandlerLifecycle(t *testing.T) {
	acc := &testutil.Accumulator{}

	parser := value.Parser{
		MetricName: "cpu",
		DataType:   "int",
	}
	cg := newConsumerGroupHandler(acc, 1, &parser, testutil.Logger{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session := &FakeConsumerGroupSession{
		ctx: ctx,
	}
	var claim FakeConsumerGroupClaim
	var err error

	err = cg.Setup(session)
	require.NoError(t, err)

	cancel()
	// This produces a flappy testcase probably due to a race between context cancellation and consumption.
	// Furthermore, it is not clear what the outcome of this test should be...
	// err = cg.ConsumeClaim(session, &claim)
	// require.NoError(t, err)
	// So stick with the line below for now.
	//nolint:errcheck // see above
	cg.ConsumeClaim(session, &claim)

	err = cg.Cleanup(session)
	require.NoError(t, err)
}

func TestConsumerGroupHandlerConsumeClaim(t *testing.T) {
	acc := &testutil.Accumulator{}
	parser := value.Parser{
		MetricName: "cpu",
		DataType:   "int",
	}
	require.NoError(t, parser.Init())
	cg := newConsumerGroupHandler(acc, 1, &parser, testutil.Logger{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session := &FakeConsumerGroupSession{ctx: ctx}
	claim := &FakeConsumerGroupClaim{
		messages: make(chan *sarama.ConsumerMessage, 1),
	}

	err := cg.Setup(session)
	require.NoError(t, err)

	claim.messages <- &sarama.ConsumerMessage{
		Topic: "telegraf",
		Value: []byte("42"),
	}

	go func() {
		err := cg.ConsumeClaim(session, claim)
		if err == nil {
			t.Error("An error was expected.")
			return
		}
		if err.Error() != "context canceled" {
			t.Errorf("Expected 'context canceled' error, got: %v", err)
			return
		}
	}()

	acc.Wait(1)
	cancel()

	err = cg.Cleanup(session)
	require.NoError(t, err)

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"cpu",
			map[string]string{},
			map[string]interface{}{
				"value": 42,
			},
			time.Now(),
		),
	}

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestConsumerGroupHandlerHandle(t *testing.T) {
	tests := []struct {
		name                string
		maxMessageLen       int
		topicTag            string
		msg                 *sarama.ConsumerMessage
		expected            []telegraf.Metric
		expectedHandleError string
	}{
		{
			name: "happy path",
			msg: &sarama.ConsumerMessage{
				Topic: "telegraf",
				Value: []byte("42"),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"value": 42,
					},
					time.Now(),
				),
			},
		},
		{
			name:          "message to long",
			maxMessageLen: 4,
			msg: &sarama.ConsumerMessage{
				Topic: "telegraf",
				Value: []byte("12345"),
			},
			expectedHandleError: "message exceeds max_message_len (actual 5, max 4)",
		},
		{
			name: "parse error",
			msg: &sarama.ConsumerMessage{
				Topic: "telegraf",
				Value: []byte("not an integer"),
			},
			expectedHandleError: "strconv.Atoi: parsing \"integer\": invalid syntax",
		},
		{
			name:     "add topic tag",
			topicTag: "topic",
			msg: &sarama.ConsumerMessage{
				Topic: "telegraf",
				Value: []byte("42"),
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"topic": "telegraf",
					},
					map[string]interface{}{
						"value": 42,
					},
					time.Now(),
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := &testutil.Accumulator{}
			parser := value.Parser{
				MetricName: "cpu",
				DataType:   "int",
			}
			require.NoError(t, parser.Init())
			cg := newConsumerGroupHandler(acc, 1, &parser, testutil.Logger{})
			cg.maxMessageLen = tt.maxMessageLen
			cg.topicTag = tt.topicTag

			ctx := context.Background()
			session := &FakeConsumerGroupSession{ctx: ctx}

			require.NoError(t, cg.reserve(ctx))
			err := cg.handle(session, tt.msg)
			if tt.expectedHandleError != "" {
				require.Error(t, err)
				require.EqualValues(t, tt.expectedHandleError, err.Error())
			} else {
				require.NoError(t, err)
			}

			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
		})
	}
}

func TestExponentialBackoff(t *testing.T) {
	var err error

	backoff := 10 * time.Millisecond
	limit := 3

	// get an unused port by listening on next available port, then closing it
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())

	// try to connect to kafka on that unused port
	brokers := []string{
		fmt.Sprintf("localhost:%d", port),
	}

	input := KafkaConsumer{
		Brokers:                brokers,
		Log:                    testutil.Logger{},
		Topics:                 []string{"topic"},
		MaxUndeliveredMessages: 1,

		ReadConfig: kafka.ReadConfig{
			Config: kafka.Config{
				MetadataRetryMax:     limit,
				MetadataRetryBackoff: config.Duration(backoff),
				MetadataRetryType:    "exponential",
			},
		},
	}
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	input.SetParser(parser)

	// time how long initialization (connection) takes
	start := time.Now()
	require.NoError(t, input.Init())

	acc := testutil.Accumulator{}
	require.Error(t, input.Start(&acc))
	elapsed := time.Since(start)
	t.Logf("elapsed %d", elapsed)

	var expectedRetryDuration time.Duration
	for i := 0; i < limit; i++ {
		expectedRetryDuration += backoff * time.Duration(math.Pow(2, float64(i)))
	}
	t.Logf("expected > %d", expectedRetryDuration)

	// Other than the expected retry delay, initializing and starting the
	// plugin, including initializing a sarama consumer takes some time.
	//
	// It would be nice to check that the actual time is within an expected
	// range, but we don't know how long the non-retry time should be.
	//
	// For now, just check that elapsed time isn't shorter than we expect the
	// retry delays to be
	require.GreaterOrEqual(t, elapsed, expectedRetryDuration)

	input.Stop()
}

func TestExponentialBackoffDefault(t *testing.T) {
	input := KafkaConsumer{
		Brokers:                []string{"broker"},
		Log:                    testutil.Logger{},
		Topics:                 []string{"topic"},
		MaxUndeliveredMessages: 1,

		ReadConfig: kafka.ReadConfig{
			Config: kafka.Config{
				MetadataRetryType: "exponential",
			},
		},
	}
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	input.SetParser(parser)
	require.NoError(t, input.Init())

	// We don't need to start the plugin here since we're only testing
	// initialization

	// if input.MetadataRetryBackoff isn't set, it should be 250 ms
	require.Equal(t, input.MetadataRetryBackoff, config.Duration(250*time.Millisecond))
}

func TestKafkaRoundTripIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	var tests = []struct {
		name                 string
		connectionStrategy   string
		topics               []string
		topicRegexps         []string
		topicRefreshInterval config.Duration
	}{
		{"connection strategy startup", "startup", []string{"Test"}, nil, config.Duration(0)},
		{"connection strategy defer", "defer", []string{"Test"}, nil, config.Duration(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			kafkaContainer, err := kafkacontainer.Run(ctx, "confluentinc/confluent-local:7.5.0")
			require.NoError(t, err)
			defer kafkaContainer.Terminate(ctx) //nolint:errcheck // ignored

			brokers, err := kafkaContainer.Brokers(ctx)
			require.NoError(t, err)

			// Make kafka output
			t.Logf("rt: starting output plugin")
			creator := outputs.Outputs["kafka"]
			output, ok := creator().(*outputs_kafka.Kafka)
			require.True(t, ok)

			s := &serializers_influx.Serializer{}
			require.NoError(t, s.Init())
			output.SetSerializer(s)
			output.Brokers = brokers
			output.Topic = "Test"
			output.Log = testutil.Logger{}

			require.NoError(t, output.Init())
			require.NoError(t, output.Connect())

			// Make kafka input
			t.Logf("rt: starting input plugin")
			input := KafkaConsumer{
				Brokers:                brokers,
				Log:                    testutil.Logger{},
				Topics:                 tt.topics,
				TopicRegexps:           tt.topicRegexps,
				MaxUndeliveredMessages: 1,
				ConnectionStrategy:     tt.connectionStrategy,
			}
			parser := &influx.Parser{}
			require.NoError(t, parser.Init())
			input.SetParser(parser)
			require.NoError(t, input.Init())

			acc := testutil.Accumulator{}
			require.NoError(t, input.Start(&acc))

			// Shove some metrics through
			expected := testutil.MockMetrics()
			t.Logf("rt: writing")
			require.NoError(t, output.Write(expected))

			// Check that they were received
			t.Logf("rt: expecting")
			acc.Wait(len(expected))
			testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics())

			t.Logf("rt: shutdown")
			require.NoError(t, output.Close())
			input.Stop()

			t.Logf("rt: done")
		})
	}
}

func TestKafkaTimestampSourceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	metrics := []telegraf.Metric{
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42},
			time.Unix(1704067200, 0),
		),
	}

	for _, source := range []string{"metric", "inner", "outer"} {
		t.Run(source, func(t *testing.T) {
			ctx := context.Background()
			kafkaContainer, err := kafkacontainer.Run(ctx, "confluentinc/confluent-local:7.5.0")
			require.NoError(t, err)
			defer kafkaContainer.Terminate(ctx) //nolint:errcheck // ignored

			brokers, err := kafkaContainer.Brokers(ctx)
			require.NoError(t, err)

			// Make kafka output
			creator := outputs.Outputs["kafka"]
			output, ok := creator().(*outputs_kafka.Kafka)
			require.True(t, ok)

			s := &serializers_influx.Serializer{}
			require.NoError(t, s.Init())
			output.SetSerializer(s)
			output.Brokers = brokers
			output.Topic = "Test"
			output.Log = &testutil.Logger{}

			require.NoError(t, output.Init())
			require.NoError(t, output.Connect())
			defer output.Close()

			// Make kafka input
			input := KafkaConsumer{
				Brokers:                brokers,
				Log:                    testutil.Logger{},
				Topics:                 []string{"Test"},
				MaxUndeliveredMessages: 1,
			}
			parser := &influx.Parser{}
			require.NoError(t, parser.Init())
			input.SetParser(parser)
			require.NoError(t, input.Init())

			var acc testutil.Accumulator
			require.NoError(t, input.Start(&acc))
			defer input.Stop()

			// Send the metrics and check that we got it back
			sendTimestamp := time.Now().Unix()
			require.NoError(t, output.Write(metrics))
			require.Eventually(t, func() bool { return acc.NMetrics() > 0 }, 5*time.Second, 100*time.Millisecond)
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, metrics, actual, testutil.IgnoreTime())

			// Check the timestamp
			m := actual[0]
			switch source {
			case "metric":
				require.EqualValues(t, 1704067200, m.Time().Unix())
			case "inner", "outer":
				require.GreaterOrEqual(t, sendTimestamp, m.Time().Unix())
			}
		})
	}
}

func TestStartupErrorBehaviorErrorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Startup the container
	ctx := context.Background()
	container, err := kafkacontainer.Run(ctx, "confluentinc/confluent-local:7.5.0")
	require.NoError(t, err)
	defer container.Terminate(ctx) //nolint:errcheck // ignored

	brokers, err := container.Brokers(ctx)
	require.NoError(t, err)

	// Pause the container for simulating connectivity issues
	containerID := container.GetContainerID()
	provider, err := testcontainers.NewDockerProvider()
	require.NoError(t, err)
	require.NoError(t, provider.Client().ContainerPause(ctx, containerID))
	//nolint:errcheck // Ignore the returned error as we cannot do anything about it anyway
	defer provider.Client().ContainerUnpause(ctx, containerID)

	// Setup the plugin and connect to the broker
	plugin := &KafkaConsumer{
		Brokers:                brokers,
		Log:                    testutil.Logger{},
		Topics:                 []string{"test"},
		MaxUndeliveredMessages: 1,
	}
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	// Create a model to be able to use the startup retry strategy
	model := models.NewRunningInput(
		plugin,
		&models.InputConfig{
			Name:  "kafka_consumer",
			Alias: "error-test",
		},
	)
	model.StartupErrors.Set(0)
	require.NoError(t, model.Init())

	// Speed up test
	plugin.config.Net.DialTimeout = 100 * time.Millisecond
	plugin.config.Net.WriteTimeout = 100 * time.Millisecond
	plugin.config.Net.ReadTimeout = 100 * time.Millisecond

	// Starting the plugin will fail with an error because the container is paused.
	var acc testutil.Accumulator
	require.ErrorContains(t, model.Start(&acc), "client has run out of available brokers to talk to")
}

func TestStartupErrorBehaviorIgnoreIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Startup the container
	ctx := context.Background()
	container, err := kafkacontainer.Run(ctx, "confluentinc/confluent-local:7.5.0")
	require.NoError(t, err)
	defer container.Terminate(ctx) //nolint:errcheck // ignored

	brokers, err := container.Brokers(ctx)
	require.NoError(t, err)

	// Pause the container for simulating connectivity issues
	containerID := container.GetContainerID()
	provider, err := testcontainers.NewDockerProvider()
	require.NoError(t, err)
	require.NoError(t, provider.Client().ContainerPause(ctx, containerID))
	//nolint:errcheck // Ignore the returned error as we cannot do anything about it anyway
	defer provider.Client().ContainerUnpause(ctx, containerID)

	// Setup the plugin and connect to the broker
	plugin := &KafkaConsumer{
		Brokers:                brokers,
		Log:                    testutil.Logger{},
		Topics:                 []string{"test"},
		MaxUndeliveredMessages: 1,
	}
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	// Create a model to be able to use the startup retry strategy
	model := models.NewRunningInput(
		plugin,
		&models.InputConfig{
			Name:                 "kafka_consumer",
			Alias:                "ignore-test",
			StartupErrorBehavior: "ignore",
		},
	)
	model.StartupErrors.Set(0)
	require.NoError(t, model.Init())

	// Speed up test
	plugin.config.Net.DialTimeout = 100 * time.Millisecond
	plugin.config.Net.WriteTimeout = 100 * time.Millisecond
	plugin.config.Net.ReadTimeout = 100 * time.Millisecond

	// Starting the plugin will fail because the container is paused.
	// The model code should convert it to a fatal error for the agent to remove
	// the plugin.
	var acc testutil.Accumulator
	err = model.Start(&acc)
	require.ErrorContains(t, err, "client has run out of available brokers to talk to")
	var fatalErr *internal.FatalError
	require.ErrorAs(t, err, &fatalErr)
}

func TestStartupErrorBehaviorRetryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Startup the container
	ctx := context.Background()
	container, err := kafkacontainer.Run(ctx, "confluentinc/confluent-local:7.5.0")
	require.NoError(t, err)
	defer container.Terminate(ctx) //nolint:errcheck // ignored

	brokers, err := container.Brokers(ctx)
	require.NoError(t, err)

	// Pause the container for simulating connectivity issues
	containerID := container.GetContainerID()
	provider, err := testcontainers.NewDockerProvider()
	require.NoError(t, err)
	require.NoError(t, provider.Client().ContainerPause(ctx, containerID))
	//nolint:errcheck // Ignore the returned error as we cannot do anything about it anyway
	defer provider.Client().ContainerUnpause(ctx, containerID)

	// Setup the plugin and connect to the broker
	plugin := &KafkaConsumer{
		Brokers:                brokers,
		Log:                    testutil.Logger{},
		Topics:                 []string{"test"},
		MaxUndeliveredMessages: 1,
	}
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	// Create a model to be able to use the startup retry strategy
	model := models.NewRunningInput(
		plugin,
		&models.InputConfig{
			Name:                 "kafka_consumer",
			Alias:                "retry-test",
			StartupErrorBehavior: "retry",
		},
	)
	model.StartupErrors.Set(0)
	require.NoError(t, model.Init())

	// Speed up test
	plugin.config.Net.DialTimeout = 100 * time.Millisecond
	plugin.config.Net.WriteTimeout = 100 * time.Millisecond
	plugin.config.Net.ReadTimeout = 100 * time.Millisecond

	// Starting the plugin will not fail but should retry to connect in every gather cycle
	var acc testutil.Accumulator
	require.NoError(t, model.Start(&acc))
	require.EqualValues(t, 1, model.StartupErrors.Get())

	// There should be no metrics as the plugin is not fully started up yet
	require.Empty(t, acc.GetTelegrafMetrics())
	require.ErrorIs(t, model.Gather(&acc), internal.ErrNotConnected)
	require.Equal(t, int64(2), model.StartupErrors.Get())

	// Unpause the container, now writes should succeed
	require.NoError(t, provider.Client().ContainerUnpause(ctx, containerID))
	require.NoError(t, model.Gather(&acc))
	defer model.Stop()
	require.Equal(t, int64(2), model.StartupErrors.Get())

	// Setup a writer
	creator := outputs.Outputs["kafka"]
	output, ok := creator().(*outputs_kafka.Kafka)
	require.True(t, ok)

	s := &serializers_influx.Serializer{}
	require.NoError(t, s.Init())
	output.SetSerializer(s)
	output.Brokers = brokers
	output.Topic = "test"
	output.Log = &testutil.Logger{}

	require.NoError(t, output.Init())
	require.NoError(t, output.Connect())
	defer output.Close()

	// Send some data to the broker so we have something to receive
	metrics := []telegraf.Metric{
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": 42},
			time.Unix(1704067200, 0),
		),
	}
	require.NoError(t, output.Write(metrics))

	// Verify that the metrics were actually written
	require.Eventually(t, func() bool {
		return acc.NMetrics() >= 1
	}, 3*time.Second, 100*time.Millisecond)
	testutil.RequireMetricsEqual(t, metrics, acc.GetTelegrafMetrics())
}
