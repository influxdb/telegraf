package mqtt_consumer

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
)

type fakeClient struct {
	connectF           func() mqtt.Token
	subscribeMultipleF func() mqtt.Token
	addRouteF          func(callback mqtt.MessageHandler)
	disconnectF        func()

	connectCallCount    int
	subscribeCallCount  int
	addRouteCallCount   int
	disconnectCallCount int

	connected bool
}

func (c *fakeClient) Connect() mqtt.Token {
	c.connectCallCount++
	token := c.connectF()
	c.connected = token.Error() == nil
	return token
}

func (c *fakeClient) SubscribeMultiple(map[string]byte, mqtt.MessageHandler) mqtt.Token {
	c.subscribeCallCount++
	return c.subscribeMultipleF()
}

func (c *fakeClient) AddRoute(_ string, callback mqtt.MessageHandler) {
	c.addRouteCallCount++
	c.addRouteF(callback)
}

func (c *fakeClient) Disconnect(uint) {
	c.disconnectCallCount++
	c.disconnectF()
	c.connected = false
}

func (c *fakeClient) IsConnected() bool {
	return c.connected
}

type fakeParser struct{}

// fakeParser satisfies telegraf.Parser
var _ telegraf.Parser = &fakeParser{}

func (*fakeParser) Parse([]byte) ([]telegraf.Metric, error) {
	panic("not implemented")
}

func (*fakeParser) ParseLine(string) (telegraf.Metric, error) {
	panic("not implemented")
}

func (*fakeParser) SetDefaultTags(map[string]string) {
	panic("not implemented")
}

type fakeToken struct {
	sessionPresent bool
	complete       chan struct{}
}

// fakeToken satisfies mqtt.Token
var _ mqtt.Token = &fakeToken{}

func (*fakeToken) Wait() bool {
	return true
}

func (*fakeToken) WaitTimeout(time.Duration) bool {
	return true
}

func (*fakeToken) Error() error {
	return nil
}

func (t *fakeToken) SessionPresent() bool {
	return t.sessionPresent
}

func (t *fakeToken) Done() <-chan struct{} {
	return t.complete
}

// Test the basic lifecycle transitions of the plugin.
func TestLifecycleSanity(t *testing.T) {
	var acc testutil.Accumulator

	plugin := newMQTTConsumer(func(*mqtt.ClientOptions) client {
		return &fakeClient{
			connectF: func() mqtt.Token {
				return &fakeToken{}
			},
			addRouteF: func(mqtt.MessageHandler) {
			},
			subscribeMultipleF: func() mqtt.Token {
				return &fakeToken{}
			},
			disconnectF: func() {
			},
		}
	})
	plugin.Log = testutil.Logger{}
	plugin.Servers = []string{"tcp://127.0.0.1"}

	parser := &fakeParser{}
	plugin.SetParser(parser)

	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Start(&acc))
	require.NoError(t, plugin.Gather(&acc))
	plugin.Stop()
}

// Test that default client has random ID
func TestRandomClientID(t *testing.T) {
	var err error

	m1 := newMQTTConsumer(nil)
	m1.Log = testutil.Logger{}
	err = m1.Init()
	require.NoError(t, err)

	m2 := newMQTTConsumer(nil)
	m2.Log = testutil.Logger{}
	err = m2.Init()
	require.NoError(t, err)

	require.NotEqual(t, m1.opts.ClientID, m2.opts.ClientID)
}

// PersistentSession requires ClientID
func TestPersistentClientIDFail(t *testing.T) {
	plugin := newMQTTConsumer(nil)
	plugin.Log = testutil.Logger{}
	plugin.PersistentSession = true

	err := plugin.Init()
	require.Error(t, err)
}

type message struct {
	topic string
	qos   byte
}

func (*message) Duplicate() bool {
	panic("not implemented")
}

func (m *message) Qos() byte {
	return m.qos
}

func (*message) Retained() bool {
	panic("not implemented")
}

func (m *message) Topic() string {
	return m.topic
}

func (*message) MessageID() uint16 {
	panic("not implemented")
}

func (*message) Payload() []byte {
	return []byte("cpu time_idle=42i")
}

func (*message) Ack() {
	panic("not implemented")
}

func TestTopicTag(t *testing.T) {
	tests := []struct {
		name          string
		topic         string
		topicTag      func() *string
		expectedError string
		topicParsing  []topicParsingConfig
		expected      []telegraf.Metric
	}{
		{
			name:  "default topic when topic tag is unset for backwards compatibility",
			topic: "telegraf",
			topicTag: func() *string {
				return nil
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"topic": "telegraf",
					},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:  "use topic tag when set",
			topic: "telegraf",
			topicTag: func() *string {
				tag := "topic_tag"
				return &tag
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"topic_tag": "telegraf",
					},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:  "no topic tag is added when topic tag is set to the empty string",
			topic: "telegraf",
			topicTag: func() *string {
				tag := ""
				return &tag
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:  "topic parsing configured",
			topic: "telegraf/123/test",
			topicTag: func() *string {
				tag := ""
				return &tag
			},
			topicParsing: []topicParsingConfig{
				{
					Topic:       "telegraf/123/test",
					Measurement: "_/_/measurement",
					Tags:        "testTag/_/_",
					Fields:      "_/testNumber/_",
					FieldTypes: map[string]string{
						"testNumber": "int",
					},
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"test",
					map[string]string{
						"testTag": "telegraf",
					},
					map[string]interface{}{
						"testNumber": 123,
						"time_idle":  42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:  "topic parsing configured with a mqtt wild card `+`",
			topic: "telegraf/123/test/hello",
			topicTag: func() *string {
				tag := ""
				return &tag
			},
			topicParsing: []topicParsingConfig{
				{
					Topic:       "telegraf/+/test/hello",
					Measurement: "_/_/measurement/_",
					Tags:        "testTag/_/_/_",
					Fields:      "_/testNumber/_/testString",
					FieldTypes: map[string]string{
						"testNumber": "int",
					},
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"test",
					map[string]string{
						"testTag": "telegraf",
					},
					map[string]interface{}{
						"testNumber": 123,
						"testString": "hello",
						"time_idle":  42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:  "topic parsing configured incorrectly",
			topic: "telegraf/123/test/hello",
			topicTag: func() *string {
				tag := ""
				return &tag
			},
			expectedError: "config error topic parsing: fields length does not equal topic length",
			topicParsing: []topicParsingConfig{
				{
					Topic:       "telegraf/+/test/hello",
					Measurement: "_/_/measurement/_",
					Tags:        "testTag/_/_/_",
					Fields:      "_/_/testNumber:int/_/testString:string",
					FieldTypes: map[string]string{
						"testNumber": "int",
					},
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"test",
					map[string]string{
						"testTag": "telegraf",
					},
					map[string]interface{}{
						"testNumber": 123,
						"testString": "hello",
						"time_idle":  42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:  "topic parsing configured without fields",
			topic: "telegraf/123/test/hello",
			topicTag: func() *string {
				tag := ""
				return &tag
			},
			topicParsing: []topicParsingConfig{
				{
					Topic:       "telegraf/+/test/hello",
					Measurement: "_/_/measurement/_",
					Tags:        "testTag/_/_/_",
					FieldTypes: map[string]string{
						"testNumber": "int",
					},
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"test",
					map[string]string{
						"testTag": "telegraf",
					},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:  "topic parsing configured without measurement",
			topic: "telegraf/123/test/hello",
			topicTag: func() *string {
				tag := ""
				return &tag
			},
			topicParsing: []topicParsingConfig{
				{
					Topic:  "telegraf/+/test/hello",
					Tags:   "testTag/_/_/_",
					Fields: "_/testNumber/_/testString",
					FieldTypes: map[string]string{
						"testNumber": "int",
					},
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{
						"testTag": "telegraf",
					},
					map[string]interface{}{
						"testNumber": 123,
						"testString": "hello",
						"time_idle":  42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:  "topic parsing configured topic with a prefix `/`",
			topic: "/telegraf/123/test/hello",
			topicTag: func() *string {
				tag := ""
				return &tag
			},
			topicParsing: []topicParsingConfig{
				{
					Topic:       "/telegraf/+/test/hello",
					Measurement: "/_/_/measurement/_",
					Tags:        "/testTag/_/_/_",
					Fields:      "/_/testNumber/_/testString",
					FieldTypes: map[string]string{
						"testNumber": "int",
					},
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"test",
					map[string]string{
						"testTag": "telegraf",
					},
					map[string]interface{}{
						"testNumber": 123,
						"testString": "hello",
						"time_idle":  42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:  "topic parsing with variable length",
			topic: "/telegraf/123/foo/test/hello",
			topicTag: func() *string {
				tag := ""
				return &tag
			},
			topicParsing: []topicParsingConfig{
				{
					Topic:       "/telegraf/#/test/hello",
					Measurement: "/#/measurement/_",
					Tags:        "/testTag/#/moreTag/_/_",
					Fields:      "/_/testNumber/#/testString",
					FieldTypes: map[string]string{
						"testNumber": "int",
					},
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"test",
					map[string]string{
						"testTag": "telegraf",
						"moreTag": "foo",
					},
					map[string]interface{}{
						"testNumber": 123,
						"testString": "hello",
						"time_idle":  42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:  "topic parsing with variable length too short",
			topic: "/telegraf/123",
			topicTag: func() *string {
				tag := ""
				return &tag
			},
			topicParsing: []topicParsingConfig{
				{
					Topic:       "/telegraf/#",
					Measurement: "/#/measurement/_",
					Tags:        "/testTag/#/moreTag/_/_",
					Fields:      "/_/testNumber/#/testString",
					FieldTypes: map[string]string{
						"testNumber": "int",
					},
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"cpu",
					map[string]string{},
					map[string]interface{}{
						"time_idle": 42,
					},
					time.Unix(0, 0),
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var handler mqtt.MessageHandler
			fClient := &fakeClient{
				connectF: func() mqtt.Token {
					return &fakeToken{}
				},
				addRouteF: func(callback mqtt.MessageHandler) {
					handler = callback
				},
				subscribeMultipleF: func() mqtt.Token {
					return &fakeToken{}
				},
				disconnectF: func() {
				},
			}

			plugin := newMQTTConsumer(func(*mqtt.ClientOptions) client {
				return fClient
			})
			plugin.Log = testutil.Logger{}
			plugin.Topics = []string{tt.topic}
			plugin.TopicTag = tt.topicTag()
			plugin.TopicParserConfig = tt.topicParsing

			parser := &influx.Parser{}
			require.NoError(t, parser.Init())
			plugin.SetParser(parser)

			err := plugin.Init()
			if tt.expectedError != "" {
				require.ErrorContains(t, err, tt.expectedError)
				return
			}
			require.NoError(t, err)

			var acc testutil.Accumulator
			require.NoError(t, plugin.Start(&acc))

			var m message
			m.topic = tt.topic

			handler(nil, &m)

			plugin.Stop()

			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
		})
	}
}

func TestAddRouteCalledForEachTopic(t *testing.T) {
	fClient := &fakeClient{
		connectF: func() mqtt.Token {
			return &fakeToken{}
		},
		addRouteF: func(mqtt.MessageHandler) {
		},
		subscribeMultipleF: func() mqtt.Token {
			return &fakeToken{}
		},
		disconnectF: func() {
		},
	}
	plugin := newMQTTConsumer(func(*mqtt.ClientOptions) client {
		return fClient
	})
	plugin.Log = testutil.Logger{}
	plugin.Topics = []string{"a", "b"}

	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))

	plugin.Stop()

	require.Equal(t, 2, fClient.addRouteCallCount)
}

func TestSubscribeCalledIfNoSession(t *testing.T) {
	fClient := &fakeClient{
		connectF: func() mqtt.Token {
			return &fakeToken{}
		},
		addRouteF: func(mqtt.MessageHandler) {
		},
		subscribeMultipleF: func() mqtt.Token {
			return &fakeToken{}
		},
		disconnectF: func() {
		},
	}
	plugin := newMQTTConsumer(func(*mqtt.ClientOptions) client {
		return fClient
	})
	plugin.Log = testutil.Logger{}
	plugin.Topics = []string{"b"}

	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))

	plugin.Stop()

	require.Equal(t, 1, fClient.subscribeCallCount)
}

func TestSubscribeNotCalledIfSession(t *testing.T) {
	fClient := &fakeClient{
		connectF: func() mqtt.Token {
			return &fakeToken{sessionPresent: true}
		},
		addRouteF: func(mqtt.MessageHandler) {
		},
		subscribeMultipleF: func() mqtt.Token {
			return &fakeToken{}
		},
		disconnectF: func() {
		},
	}
	plugin := newMQTTConsumer(func(*mqtt.ClientOptions) client {
		return fClient
	})
	plugin.Log = testutil.Logger{}
	plugin.Topics = []string{"b"}

	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	plugin.Stop()

	require.Equal(t, 0, fClient.subscribeCallCount)
}

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Startup the container
	conf, err := filepath.Abs(filepath.Join("testdata", "mosquitto.conf"))
	require.NoError(t, err, "missing file mosquitto.conf")

	const servicePort = "1883"
	container := testutil.Container{
		Image:        "eclipse-mosquitto:2",
		ExposedPorts: []string{servicePort},
		WaitingFor:   wait.ForListeningPort(servicePort),
		Files: map[string]string{
			"/mosquitto/config/mosquitto.conf": conf,
		},
	}
	require.NoError(t, container.Start(), "failed to start container")
	defer container.Terminate()

	// Setup the plugin and connect to the broker
	url := fmt.Sprintf("tcp://%s:%s", container.Address, container.Ports[servicePort])
	topic := "/telegraf/test"
	factory := func(o *mqtt.ClientOptions) client { return mqtt.NewClient(o) }
	plugin := &MQTTConsumer{
		Servers:                []string{url},
		Topics:                 []string{topic},
		MaxUndeliveredMessages: defaultMaxUndeliveredMessages,
		ConnectionTimeout:      config.Duration(5 * time.Second),
		KeepAliveInterval:      config.Duration(1 * time.Second),
		PingTimeout:            config.Duration(100 * time.Millisecond),
		Log:                    testutil.Logger{Name: "mqtt-integration-test"},
		clientFactory:          factory,
	}
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)
	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	// Setup a producer to send some metrics to the broker
	cfg, err := plugin.createOpts()
	require.NoError(t, err)
	client := mqtt.NewClient(cfg)
	token := client.Connect()
	token.Wait()
	require.NoError(t, token.Error())
	defer client.Disconnect(100)

	// Setup the metrics
	metrics := []string{
		"test,source=A value=0i 1712780301000000000",
		"test,source=B value=1i 1712780301000000100",
		"test,source=C value=2i 1712780301000000200",
	}
	expected := make([]telegraf.Metric, 0, len(metrics))
	for _, x := range metrics {
		metrics, err := parser.Parse([]byte(x))
		for i := range metrics {
			metrics[i].AddTag("topic", topic)
		}
		require.NoError(t, err)
		expected = append(expected, metrics...)
	}

	// Write metrics
	for _, x := range metrics {
		xtoken := client.Publish(topic, byte(plugin.QoS), false, []byte(x))
		require.NoError(t, xtoken.Error())
	}

	// Verify that the metrics were actually written
	require.Eventually(t, func() bool {
		return acc.NMetrics() >= uint64(len(expected))
	}, 3*time.Second, 100*time.Millisecond)

	client.Disconnect(100)
	plugin.Stop()
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics())
}

func TestStartupErrorBehaviorErrorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Startup the container
	conf, err := filepath.Abs(filepath.Join("testdata", "mosquitto.conf"))
	require.NoError(t, err, "missing file mosquitto.conf")

	const servicePort = "1883"
	container := testutil.Container{
		Image:        "eclipse-mosquitto:2",
		ExposedPorts: []string{servicePort},
		WaitingFor:   wait.ForListeningPort(servicePort),
		Files: map[string]string{
			"/mosquitto/config/mosquitto.conf": conf,
		},
	}
	require.NoError(t, container.Start(), "failed to start container")
	defer container.Terminate()

	// Pause the container for simulating connectivity issues
	require.NoError(t, container.Pause())
	defer container.Resume() //nolint:errcheck // Ignore the returned error as we cannot do anything about it anyway

	// Setup the plugin and connect to the broker
	url := fmt.Sprintf("tcp://%s:%s", container.Address, container.Ports[servicePort])
	topic := "/telegraf/test"
	factory := func(o *mqtt.ClientOptions) client { return mqtt.NewClient(o) }
	plugin := &MQTTConsumer{
		Servers:                []string{url},
		Topics:                 []string{topic},
		MaxUndeliveredMessages: defaultMaxUndeliveredMessages,
		ConnectionTimeout:      config.Duration(5 * time.Second),
		KeepAliveInterval:      config.Duration(1 * time.Second),
		PingTimeout:            config.Duration(100 * time.Millisecond),
		Log:                    testutil.Logger{Name: "mqtt-integration-test"},
		clientFactory:          factory,
	}
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	// Create a model to be able to use the startup retry strategy
	model := models.NewRunningInput(
		plugin,
		&models.InputConfig{
			Name:  "mqtt_consumer",
			Alias: "error-test", // required to get a unique error stats instance
		},
	)
	model.StartupErrors.Set(0)
	require.NoError(t, model.Init())

	// Starting the plugin will fail with an error because the container is paused.
	var acc testutil.Accumulator
	require.ErrorContains(t, model.Start(&acc), "network Error")
}

func TestStartupErrorBehaviorIgnoreIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Startup the container
	conf, err := filepath.Abs(filepath.Join("testdata", "mosquitto.conf"))
	require.NoError(t, err, "missing file mosquitto.conf")

	const servicePort = "1883"
	container := testutil.Container{
		Image:        "eclipse-mosquitto:2",
		ExposedPorts: []string{servicePort},
		WaitingFor:   wait.ForListeningPort(servicePort),
		Files: map[string]string{
			"/mosquitto/config/mosquitto.conf": conf,
		},
	}
	require.NoError(t, container.Start(), "failed to start container")
	defer container.Terminate()

	// Pause the container for simulating connectivity issues
	require.NoError(t, container.Pause())
	defer container.Resume() //nolint:errcheck // Ignore the returned error as we cannot do anything about it anyway

	// Setup the plugin and connect to the broker
	url := fmt.Sprintf("tcp://%s:%s", container.Address, container.Ports[servicePort])
	topic := "/telegraf/test"
	factory := func(o *mqtt.ClientOptions) client { return mqtt.NewClient(o) }
	plugin := &MQTTConsumer{
		Servers:                []string{url},
		Topics:                 []string{topic},
		MaxUndeliveredMessages: defaultMaxUndeliveredMessages,
		ConnectionTimeout:      config.Duration(5 * time.Second),
		KeepAliveInterval:      config.Duration(1 * time.Second),
		PingTimeout:            config.Duration(100 * time.Millisecond),
		Log:                    testutil.Logger{Name: "mqtt-integration-test"},
		clientFactory:          factory,
	}
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	// Create a model to be able to use the startup retry strategy
	model := models.NewRunningInput(
		plugin,
		&models.InputConfig{
			Name:                 "mqtt_consumer",
			Alias:                "ignore-test", // required to get a unique error stats instance
			StartupErrorBehavior: "ignore",
		},
	)
	model.StartupErrors.Set(0)
	require.NoError(t, model.Init())

	// Starting the plugin will fail because the container is paused.
	// The model code should convert it to a fatal error for the agent to remove
	// the plugin.
	var acc testutil.Accumulator
	err = model.Start(&acc)
	require.ErrorContains(t, err, "network Error")
	var fatalErr *internal.FatalError
	require.ErrorAs(t, err, &fatalErr)
}

func TestStartupErrorBehaviorRetryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Startup the container
	conf, err := filepath.Abs(filepath.Join("testdata", "mosquitto.conf"))
	require.NoError(t, err, "missing file mosquitto.conf")

	const servicePort = "1883"
	container := testutil.Container{
		Image:        "eclipse-mosquitto:2",
		ExposedPorts: []string{servicePort},
		WaitingFor:   wait.ForListeningPort(servicePort),
		Files: map[string]string{
			"/mosquitto/config/mosquitto.conf": conf,
		},
	}
	require.NoError(t, container.Start(), "failed to start container")
	defer container.Terminate()

	// Pause the container for simulating connectivity issues
	require.NoError(t, container.Pause())
	defer container.Resume() //nolint:errcheck // Ignore the returned error as we cannot do anything about it anyway

	// Setup the plugin and connect to the broker
	url := fmt.Sprintf("tcp://%s:%s", container.Address, container.Ports[servicePort])
	topic := "/telegraf/test"
	factory := func(o *mqtt.ClientOptions) client { return mqtt.NewClient(o) }
	plugin := &MQTTConsumer{
		Servers:                []string{url},
		Topics:                 []string{topic},
		MaxUndeliveredMessages: defaultMaxUndeliveredMessages,
		ConnectionTimeout:      config.Duration(5 * time.Second),
		KeepAliveInterval:      config.Duration(1 * time.Second),
		PingTimeout:            config.Duration(100 * time.Millisecond),
		Log:                    testutil.Logger{Name: "mqtt-integration-test"},
		clientFactory:          factory,
	}
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	// Create a model to be able to use the startup retry strategy
	model := models.NewRunningInput(
		plugin,
		&models.InputConfig{
			Name:                 "mqtt_consumer",
			Alias:                "retry-test", // required to get a unique error stats instance
			StartupErrorBehavior: "retry",
		},
	)
	model.StartupErrors.Set(0)
	require.NoError(t, model.Init())

	var acc testutil.Accumulator
	require.NoError(t, model.Start(&acc))

	// There should be no metrics as the plugin is not fully started up yet
	require.Empty(t, acc.GetTelegrafMetrics())
	require.ErrorIs(t, model.Gather(&acc), internal.ErrNotConnected)
	require.Equal(t, int64(2), model.StartupErrors.Get())

	// Unpause the container, now writes should succeed
	require.NoError(t, container.Resume())
	require.NoError(t, model.Gather(&acc))
	defer model.Stop()

	// Setup a producer to send some metrics to the broker
	cfg, err := plugin.createOpts()
	require.NoError(t, err)
	client := mqtt.NewClient(cfg)
	token := client.Connect()
	token.Wait()
	require.NoError(t, token.Error())
	defer client.Disconnect(100)

	// Setup the metrics
	metrics := []string{
		"test,source=A value=0i 1712780301000000000",
		"test,source=B value=1i 1712780301000000100",
		"test,source=C value=2i 1712780301000000200",
	}
	expected := make([]telegraf.Metric, 0, len(metrics))
	for _, x := range metrics {
		metrics, err := parser.Parse([]byte(x))
		for i := range metrics {
			metrics[i].AddTag("topic", topic)
		}
		require.NoError(t, err)
		expected = append(expected, metrics...)
	}

	// Write metrics
	for _, x := range metrics {
		xtoken := client.Publish(topic, byte(plugin.QoS), false, []byte(x))
		require.NoError(t, xtoken.Error())
	}

	// Verify that the metrics were actually written
	require.Eventually(t, func() bool {
		return acc.NMetrics() >= uint64(len(expected))
	}, 3*time.Second, 100*time.Millisecond)

	client.Disconnect(100)
	plugin.Stop()
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics())
}

func TestReconnectIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Startup the container
	conf, err := filepath.Abs(filepath.Join("testdata", "mosquitto.conf"))
	require.NoError(t, err, "missing file mosquitto.conf")

	const servicePort = "1883"
	container := testutil.Container{
		Image:        "eclipse-mosquitto:2",
		ExposedPorts: []string{servicePort},
		WaitingFor:   wait.ForListeningPort(servicePort),
		Files: map[string]string{
			"/mosquitto/config/mosquitto.conf": conf,
		},
	}
	require.NoError(t, container.Start(), "failed to start container")
	defer container.Terminate()

	// Setup the plugin and connect to the broker
	url := fmt.Sprintf("tcp://%s:%s", container.Address, container.Ports[servicePort])
	topic := "/telegraf/test"
	factory := func(o *mqtt.ClientOptions) client { return mqtt.NewClient(o) }
	plugin := &MQTTConsumer{
		Servers:                []string{url},
		Topics:                 []string{topic},
		MaxUndeliveredMessages: defaultMaxUndeliveredMessages,
		ConnectionTimeout:      config.Duration(5 * time.Second),
		KeepAliveInterval:      config.Duration(1 * time.Second),
		PingTimeout:            config.Duration(100 * time.Millisecond),
		Log:                    testutil.Logger{Name: "mqtt-integration-test"},
		clientFactory:          factory,
	}
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)
	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	// Pause the container for simulating loosing connection
	require.NoError(t, container.Pause())
	defer container.Resume() //nolint:errcheck // Ignore the returned error as we cannot do anything about it anyway

	// Wait until we really lost the connection
	require.Eventually(t, func() bool {
		return !plugin.client.IsConnected()
	}, 5*time.Second, 100*time.Millisecond)

	// There should be no metrics as the plugin is not fully started up yet
	require.ErrorContains(t, plugin.Gather(&acc), "network Error")
	require.False(t, plugin.client.IsConnected())

	// Unpause the container, now we should be able to reconnect
	require.NoError(t, container.Resume())
	require.NoError(t, plugin.Gather(&acc))

	require.Eventually(t, func() bool {
		return plugin.Gather(&acc) == nil
	}, 5*time.Second, 200*time.Millisecond)
}
