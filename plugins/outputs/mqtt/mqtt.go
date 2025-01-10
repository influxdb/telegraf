//go:generate ../../../tools/readme_config_includer/generator
package mqtt

import (
	// Blank import to support go:embed compile directive
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/common/mqtt"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

type message struct {
	topic   string
	payload []byte
}

type MQTT struct {
	TopicPrefix     string          `toml:"topic_prefix" deprecated:"1.25.0;1.35.0;use 'topic' instead"`
	Topic           string          `toml:"topic"`
	BatchMessage    bool            `toml:"batch" deprecated:"1.25.2;1.35.0;use 'layout = \"batch\"' instead"`
	Layout          string          `toml:"layout"`
	HomieDeviceName string          `toml:"homie_device_name"`
	HomieNodeID     string          `toml:"homie_node_id"`
	Log             telegraf.Logger `toml:"-"`
	mqtt.MqttConfig

	client     mqtt.Client
	serializer telegraf.Serializer
	generator  *TopicNameGenerator

	homieDeviceNameGenerator *HomieGenerator
	homieNodeIDGenerator     *HomieGenerator
	homieSeen                map[string]map[string]bool

	sync.Mutex
}

func (*MQTT) SampleConfig() string {
	return sampleConfig
}

func (m *MQTT) Init() error {
	if len(m.Servers) == 0 {
		return errors.New("no servers specified")
	}

	if m.PersistentSession && m.ClientID == "" {
		return errors.New("persistent_session requires client_id")
	}
	if m.QoS > 2 || m.QoS < 0 {
		return fmt.Errorf("qos value must be 0, 1, or 2: %d", m.QoS)
	}

	var err error
	m.generator, err = NewTopicNameGenerator(m.TopicPrefix, m.Topic)
	if err != nil {
		return err
	}

	switch m.Layout {
	case "":
		// For backward compatibility
		if m.BatchMessage {
			m.Layout = "batch"
		} else {
			m.Layout = "non-batch"
		}
	case "non-batch", "batch", "field":
	case "homie-v4":
		if m.HomieDeviceName == "" {
			return errors.New("missing 'homie_device_name' option")
		}

		m.homieDeviceNameGenerator, err = NewHomieGenerator(m.HomieDeviceName)
		if err != nil {
			return fmt.Errorf("creating device name generator failed: %w", err)
		}

		if m.HomieNodeID == "" {
			return errors.New("missing 'homie_node_id' option")
		}

		m.homieNodeIDGenerator, err = NewHomieGenerator(m.HomieNodeID)
		if err != nil {
			return fmt.Errorf("creating node ID name generator failed: %w", err)
		}
	default:
		return fmt.Errorf("invalid layout %q", m.Layout)
	}

	return nil
}

func (m *MQTT) Connect() error {
	m.Lock()
	defer m.Unlock()

	m.homieSeen = make(map[string]map[string]bool)

	client, err := mqtt.NewClient(&m.MqttConfig)
	if err != nil {
		return err
	}
	m.client = client

	_, err = m.client.Connect()
	return err
}

func (m *MQTT) SetSerializer(serializer telegraf.Serializer) {
	m.serializer = serializer
}

func (m *MQTT) Close() error {
	// Unregister devices if Homie layout was used. Usually we should do this
	// using a "will" message, but this can only be done at connect time where,
	// due to the dynamic nature of Telegraf messages, we do not know the topics
	// to issue that "will" yet.
	if len(m.homieSeen) > 0 {
		for topic := range m.homieSeen {
			//nolint:errcheck // We will ignore potential errors as we cannot do anything here
			m.client.Publish(topic+"/$state", []byte("lost"))
		}
		// Give the messages some time to settle
		time.Sleep(100 * time.Millisecond)
	}
	return m.client.Close()
}

func (m *MQTT) Write(metrics []telegraf.Metric) error {
	m.Lock()
	defer m.Unlock()
	if len(metrics) == 0 {
		return nil
	}

	hostname, ok := metrics[0].Tags()["host"]
	if !ok {
		hostname = ""
	}

	// Group the metrics to topics and serialize them
	var topicMessages []message
	switch m.Layout {
	case "batch":
		topicMessages = m.collectBatch(hostname, metrics)
	case "non-batch":
		topicMessages = m.collectNonBatch(hostname, metrics)
	case "field":
		topicMessages = m.collectField(hostname, metrics)
	case "homie-v4":
		topicMessages = m.collectHomieV4(hostname, metrics)
	default:
		return fmt.Errorf("unknown layout %q", m.Layout)
	}

	for _, msg := range topicMessages {
		if err := m.client.Publish(msg.topic, msg.payload); err != nil {
			// We do receive a timeout error if the remote broker is down,
			// so let's retry the metrics in this case and drop them otherwise.
			if errors.Is(err, internal.ErrTimeout) {
				return fmt.Errorf("could not publish message to MQTT server: %w", err)
			}
			m.Log.Warnf("Could not publish message to MQTT server: %v", err)
		}
	}

	return nil
}

func (m *MQTT) collectNonBatch(hostname string, metrics []telegraf.Metric) []message {
	collection := make([]message, 0, len(metrics))
	for _, metric := range metrics {
		topic, err := m.generator.Generate(hostname, metric)
		if err != nil {
			m.Log.Warnf("Generating topic name failed: %v", err)
			m.Log.Debugf("metric was: %v", metric)
			continue
		}

		buf, err := m.serializer.Serialize(metric)
		if err != nil {
			m.Log.Warnf("Could not serialize metric for topic %q: %v", topic, err)
			m.Log.Debugf("metric was: %v", metric)
			continue
		}
		collection = append(collection, message{topic, buf})
	}

	return collection
}

func (m *MQTT) collectBatch(hostname string, metrics []telegraf.Metric) []message {
	metricsCollection := make(map[string][]telegraf.Metric)
	for _, metric := range metrics {
		topic, err := m.generator.Generate(hostname, metric)
		if err != nil {
			m.Log.Warnf("Generating topic name failed: %v", err)
			m.Log.Debugf("metric was: %v", metric)
			continue
		}
		metricsCollection[topic] = append(metricsCollection[topic], metric)
	}

	collection := make([]message, 0, len(metricsCollection))
	for topic, ms := range metricsCollection {
		buf, err := m.serializer.SerializeBatch(ms)
		if err != nil {
			m.Log.Warnf("Could not serialize metric batch for topic %q: %v", topic, err)
			continue
		}
		collection = append(collection, message{topic, buf})
	}
	return collection
}

func (m *MQTT) collectField(hostname string, metrics []telegraf.Metric) []message {
	var collection []message
	for _, metric := range metrics {
		topic, err := m.generator.Generate(hostname, metric)
		if err != nil {
			m.Log.Warnf("Generating topic name failed: %v", err)
			m.Log.Debugf("metric was: %v", metric)
			continue
		}

		for n, v := range metric.Fields() {
			buf, err := internal.ToString(v)
			if err != nil {
				m.Log.Warnf("Could not serialize metric for topic %q field %q: %v", topic, n, err)
				m.Log.Debugf("metric was: %v", metric)
				continue
			}
			collection = append(collection, message{topic + "/" + n, []byte(buf)})
		}
	}

	return collection
}

func (m *MQTT) collectHomieV4(hostname string, metrics []telegraf.Metric) []message {
	var collection []message
	for _, metric := range metrics {
		topic, err := m.generator.Generate(hostname, metric)
		if err != nil {
			m.Log.Warnf("Generating topic name failed: %v", err)
			m.Log.Debugf("metric was: %v", metric)
			continue
		}

		msgs, nodeID, err := m.collectHomieDeviceMessages(topic, metric)
		if err != nil {
			m.Log.Warn(err.Error())
			m.Log.Debugf("metric was: %v", metric)
			continue
		}
		path := topic + "/" + nodeID
		collection = append(collection, msgs...)

		for _, tag := range metric.TagList() {
			propID := normalizeID(tag.Key)
			collection = append(collection,
				message{path + "/" + propID, []byte(tag.Value)},
				message{path + "/" + propID + "/$name", []byte(tag.Key)},
				message{path + "/" + propID + "/$datatype", []byte("string")},
			)
		}

		for _, field := range metric.FieldList() {
			v, dt, err := convertType(field.Value)
			if err != nil {
				m.Log.Warnf("Could not serialize metric for topic %q field %q: %v", topic, field.Key, err)
				m.Log.Debugf("metric was: %v", metric)
				continue
			}
			propID := normalizeID(field.Key)
			collection = append(collection,
				message{path + "/" + propID, []byte(v)},
				message{path + "/" + propID + "/$name", []byte(field.Key)},
				message{path + "/" + propID + "/$datatype", []byte(dt)},
			)
		}
	}

	return collection
}

func init() {
	outputs.Add("mqtt", func() telegraf.Output {
		return &MQTT{
			MqttConfig: mqtt.MqttConfig{
				KeepAlive:     30,
				Timeout:       config.Duration(5 * time.Second),
				AutoReconnect: true,
			},
		}
	})
}
