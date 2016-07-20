package opentsdb

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/outputs"
)

type OpenTSDB struct {
	Prefix string

	Host string
	Port int

	UseHttp bool
	BatchSize int

	Debug bool
}

var sanitizedChars = strings.NewReplacer("@", "-", "*", "-", " ", "_",
	`%`, "-", "#", "-", "$", "-", ":", "_")

var sampleConfig = `
  ## prefix for metrics keys
  prefix = "my.specific.prefix."

  ## Telnet Mode ##
  ## DNS name of the OpenTSDB server
  host = "opentsdb.example.com"

  ## Port of the OpenTSDB server in telnet mode
  port = 4242

  ## Use Http PUT API
  useHttp = false

  ## Number of data points to send to OpenTSDB in Http requests.
  ## Not used when useHttp is false.
  batchSize = 50

  ## Debug true - Prints OpenTSDB communication
  debug = false
`
type TagSet map[string]string

func (t TagSet) ToLineFormat() string {
	var line string
	for k, v := range t {
		line += fmt.Sprintf(" %s=%s", k, v)
	}

	return strings.TrimLeft(line, " ")
}

func (o *OpenTSDB) Connect() error {
	// Test Connection to OpenTSDB Server
	uri := fmt.Sprintf("%s:%d", o.Host, o.Port)
	tcpAddr, err := net.ResolveTCPAddr("tcp", uri)
	if err != nil {
		return fmt.Errorf("OpenTSDB: TCP address cannot be resolved")
	}
	connection, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return fmt.Errorf("OpenTSDB: Telnet connect fail")
	}
	defer connection.Close()
	return nil
}

func (o *OpenTSDB) Write(metrics []telegraf.Metric) error {
	if len(metrics) == 0 {
		return nil
	}

	if o.UseHttp {
		return o.WriteHttp(metrics)
	} else {
		return o.WriteTelnet(metrics)
	}
}

func (o *OpenTSDB) WriteHttp(metrics []telegraf.Metric) error {
	http := openTSDBHttp{
		Host: o.Host,
		Port: o.Port,
		BatchSize: o.BatchSize,
		Debug: o.Debug,
	}

	for _, m := range metrics {
		now := m.UnixNano() / 1000000000
		tags := cleanTags(m.Tags())

		for fieldName, value := range m.Fields() {
			metricValue, buildError := buildValue(value)
			if buildError != nil {
				fmt.Printf("OpenTSDB: %s\n", buildError.Error())
				continue
			}

            metric := &HttpMetric{
                Metric: sanitizedChars.Replace(fmt.Sprintf("%s%s_%s",
                        o.Prefix, m.Name(), fieldName)),
				Tags: tags,
				Timestamp: now,
				Value: metricValue,
            }

			if err:= http.sendDataPoint(metric); err != nil {
				return err
			}
		}
	}

	if err:= http.flush(); err != nil {
		return err
	}

	return nil
}

func (o *OpenTSDB) WriteTelnet(metrics []telegraf.Metric) error {
	// Send Data with telnet / socket communication
	uri := fmt.Sprintf("%s:%d", o.Host, o.Port)
	tcpAddr, _ := net.ResolveTCPAddr("tcp", uri)
	connection, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return fmt.Errorf("OpenTSDB: Telnet connect fail")
	}
	defer connection.Close()

	for _, m := range metrics {
		now := m.UnixNano() / 1000000000
		tags := cleanTags(m.Tags()).ToLineFormat()

		for fieldName, value := range m.Fields() {
			metricValue, buildError := buildValue(value)
			if buildError != nil {
				fmt.Printf("OpenTSDB: %s\n", buildError.Error())
				continue
			}

			messageLine := fmt.Sprintf("put %s %v %s %s\n",
				sanitizedChars.Replace(fmt.Sprintf("%s%s_%s",o.Prefix, m.Name(), fieldName)),
				now, metricValue, tags)

			if o.Debug {
				fmt.Print(messageLine)
			}
			_, err := connection.Write([]byte(messageLine))
			if err != nil {
				return fmt.Errorf("OpenTSDB: Telnet writing error %s", err.Error())
			}
		}
	}

	return nil
}

func cleanTags(tags map[string]string) TagSet {
	tagSet := make(map[string]string, len(tags))
	for k, v := range tags {
		tagSet[sanitizedChars.Replace(k)] = sanitizedChars.Replace(v)
	}
	return tagSet
}

func buildValue(v interface{}) (string, error) {
	var retv string
	switch p := v.(type) {
	case int64:
		retv = IntToString(int64(p))
	case uint64:
		retv = UIntToString(uint64(p))
	case float64:
		retv = FloatToString(float64(p))
	default:
		return retv, fmt.Errorf("unexpected type %T with value %v for OpenTSDB", v, v)
	}
	return retv, nil
}

func IntToString(input_num int64) string {
	return strconv.FormatInt(input_num, 10)
}

func UIntToString(input_num uint64) string {
	return strconv.FormatUint(input_num, 10)
}

func FloatToString(input_num float64) string {
	return strconv.FormatFloat(input_num, 'f', 6, 64)
}

func (o *OpenTSDB) SampleConfig() string {
	return sampleConfig
}

func (o *OpenTSDB) Description() string {
	return "Configuration for OpenTSDB server to send metrics to"
}

func (o *OpenTSDB) Close() error {
	return nil
}

func init() {
	outputs.Add("opentsdb", func() telegraf.Output {
		return &OpenTSDB{}
	})
}
