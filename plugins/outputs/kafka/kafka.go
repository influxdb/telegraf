//go:generate ../../../tools/readme_config_includer/generator
package kafka

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"github.com/gofrs/uuid/v5"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/common/kafka"
	"github.com/influxdata/telegraf/plugins/common/proxy"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

var ValidTopicSuffixMethods = []string{
	"",
	"measurement",
	"tags",
}

var zeroTime = time.Unix(0, 0)

type Kafka struct {
	Brokers           []string        `toml:"brokers"`
	Topic             string          `toml:"topic"`
	TopicTag          string          `toml:"topic_tag"`
	ExcludeTopicTag   bool            `toml:"exclude_topic_tag"`
	TopicSuffix       TopicSuffix     `toml:"topic_suffix"`
	RoutingTag        string          `toml:"routing_tag"`
	RoutingKey        string          `toml:"routing_key"`
	ProducerTimestamp string          `toml:"producer_timestamp"`
	MetricNameHeader  string          `toml:"metric_name_header"`
	Log               telegraf.Logger `toml:"-"`
	proxy.Socks5ProxyConfig
	kafka.WriteConfig

	// Legacy TLS config options
	// TLS client certificate
	Certificate string
	// TLS client key
	Key string
	// TLS certificate authority
	CA string

	saramaConfig *sarama.Config
	producerFunc func(addrs []string, config *sarama.Config) (sarama.SyncProducer, error)
	producer     sarama.SyncProducer

	serializer telegraf.Serializer
}

type TopicSuffix struct {
	Method    string   `toml:"method"`
	Keys      []string `toml:"keys"`
	Separator string   `toml:"separator"`
}

func ValidateTopicSuffixMethod(method string) error {
	for _, validMethod := range ValidTopicSuffixMethods {
		if method == validMethod {
			return nil
		}
	}
	return fmt.Errorf("unknown topic suffix method provided: %s", method)
}

func (*Kafka) SampleConfig() string {
	return sampleConfig
}

func (k *Kafka) GetTopicName(metric telegraf.Metric) (telegraf.Metric, string) {
	topic := k.Topic
	if k.TopicTag != "" {
		if t, ok := metric.GetTag(k.TopicTag); ok {
			topic = t

			// If excluding the topic tag, a copy is required to avoid modifying
			// the metric buffer.
			if k.ExcludeTopicTag {
				metric = metric.Copy()
				metric.Accept()
				metric.RemoveTag(k.TopicTag)
			}
		}
	}

	var topicName string
	switch k.TopicSuffix.Method {
	case "measurement":
		topicName = topic + k.TopicSuffix.Separator + metric.Name()
	case "tags":
		var topicNameComponents []string
		topicNameComponents = append(topicNameComponents, topic)
		for _, tag := range k.TopicSuffix.Keys {
			tagValue := metric.Tags()[tag]
			if tagValue != "" {
				topicNameComponents = append(topicNameComponents, tagValue)
			}
		}
		topicName = strings.Join(topicNameComponents, k.TopicSuffix.Separator)
	default:
		topicName = topic
	}
	return metric, topicName
}

func (k *Kafka) SetSerializer(serializer telegraf.Serializer) {
	k.serializer = serializer
}

func (k *Kafka) Init() error {
	kafka.SetLogger(k.Log.Level())

	if err := ValidateTopicSuffixMethod(k.TopicSuffix.Method); err != nil {
		return err
	}
	config := sarama.NewConfig()

	if err := k.SetConfig(config, k.Log); err != nil {
		return err
	}

	// Legacy support ssl config
	if k.Certificate != "" {
		k.TLSCert = k.Certificate
		k.TLSCA = k.CA
		k.TLSKey = k.Key
	}

	if k.Socks5ProxyEnabled {
		config.Net.Proxy.Enable = true

		dialer, err := k.Socks5ProxyConfig.GetDialer()
		if err != nil {
			return fmt.Errorf("connecting to proxy server failed: %w", err)
		}
		config.Net.Proxy.Dialer = dialer
	}
	k.saramaConfig = config

	switch k.ProducerTimestamp {
	case "":
		k.ProducerTimestamp = "metric"
	case "metric", "now":
	default:
		return fmt.Errorf("unknown producer_timestamp option: %s", k.ProducerTimestamp)
	}

	return nil
}

func (k *Kafka) Connect() error {
	producer, err := k.producerFunc(k.Brokers, k.saramaConfig)
	if err != nil {
		return &internal.StartupError{Err: err, Retry: true}
	}
	k.producer = producer
	return nil
}

func (k *Kafka) Close() error {
	if k.producer == nil {
		return nil
	}
	return k.producer.Close()
}

func (k *Kafka) routingKey(metric telegraf.Metric) (string, error) {
	if k.RoutingTag != "" {
		key, ok := metric.GetTag(k.RoutingTag)
		if ok {
			return key, nil
		}
	}

	if k.RoutingKey == "random" {
		u, err := uuid.NewV4()
		if err != nil {
			return "", err
		}
		return u.String(), nil
	}

	return k.RoutingKey, nil
}

func (k *Kafka) Write(metrics []telegraf.Metric) error {
	msgs := make([]*sarama.ProducerMessage, 0, len(metrics))
	for _, metric := range metrics {
		metric, topic := k.GetTopicName(metric)

		buf, err := k.serializer.Serialize(metric)
		if err != nil {
			k.Log.Debugf("Could not serialize metric: %v", err)
			continue
		}

		m := &sarama.ProducerMessage{
			Topic: topic,
			Value: sarama.ByteEncoder(buf),
		}

		if k.MetricNameHeader != "" {
			m.Headers = []sarama.RecordHeader{
				{
					Key:   []byte(k.MetricNameHeader),
					Value: []byte(metric.Name()),
				},
			}
		}

		// Negative timestamps are not allowed by the Kafka protocol.
		if k.ProducerTimestamp == "metric" && !metric.Time().Before(zeroTime) {
			m.Timestamp = metric.Time()
		}

		key, err := k.routingKey(metric)
		if err != nil {
			return fmt.Errorf("could not generate routing key: %w", err)
		}

		if key != "" {
			m.Key = sarama.StringEncoder(key)
		}
		msgs = append(msgs, m)
	}

	err := k.producer.SendMessages(msgs)
	if err != nil {
		// We could have many errors, return only the first encountered.
		var errs sarama.ProducerErrors
		if errors.As(err, &errs) && len(errs) > 0 {
			// Just return the first error encountered
			firstErr := errs[0]
			if errors.Is(firstErr.Err, sarama.ErrMessageSizeTooLarge) {
				k.Log.Error("Message too large, consider increasing `max_message_bytes`; dropping batch")
				return nil
			}
			if errors.Is(firstErr.Err, sarama.ErrInvalidTimestamp) {
				k.Log.Error(
					"The timestamp of the message is out of acceptable range, consider increasing broker `message.timestamp.difference.max.ms`; " +
						"dropping batch",
				)
				return nil
			}
			return firstErr
		}
		return err
	}

	return nil
}

func init() {
	outputs.Add("kafka", func() telegraf.Output {
		return &Kafka{
			WriteConfig: kafka.WriteConfig{
				MaxRetry:     3,
				RequiredAcks: -1,
			},
			producerFunc: sarama.NewSyncProducer,
		}
	})
}
