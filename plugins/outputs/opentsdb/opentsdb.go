package opentsdb

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
	"encoding/json"
	//"os"
	"io"
	"net/http"
	"bytes"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/outputs"
)

type OpenTSDB struct {
	Prefix string

	Host string
	Port int

	UseHttp bool

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

  ## Debug true - Prints OpenTSDB communication
  debug = false
`

type MetricLine struct {
	Metric    string
	Timestamp int64
	Value     string
	Tags      string
}

type HttpMetric struct {
	Metric    string `json:"metric"`
	Timestamp int64 `json:"timestamp"`
	Value     string `json:"value"`
	Tags      map[string]string `json:"tags"`
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

	b := new(bytes.Buffer)

	enc := json.NewEncoder(b)



	io.WriteString(b, "[")

	i := 0

	for _, m := range metrics {
		now := m.UnixNano() / 1000000000
		for fieldName, value := range m.Fields() {
			metricValue, buildError := buildValue(value)
			if buildError != nil {
				fmt.Printf("OpenTSDB: %s\n", buildError.Error())
				continue
			}

			if i > 0 {
				io.WriteString(b, ",")
			}

            metric := &HttpMetric{
                    Metric: sanitizedChars.Replace(fmt.Sprintf("%s%s_%s",
                            o.Prefix, m.Name(), fieldName)),
					Tags: m.Tags(),
					Timestamp: now,
					Value: metricValue,
            }

	        if err := enc.Encode(metric); err != nil {
				return fmt.Errorf("Metric serialization error %s", err.Error())
	        }

			i++;
		}
	}

	io.WriteString(b, "]")

	uri := fmt.Sprintf("http://%s:%d/api/put", o.Host, o.Port)
	resp, err := http.Post(uri, "applicaton/json", b)
	if err != nil {
		return fmt.Errorf("Error when sending metrics: %s", err.Error())
	}

	if resp.StatusCode == 200 {
		fmt.Println("Sent metrics !")
	}

	return nil
}

func (o *OpenTSDB) WriteTelnet(metrics []telegraf.Metric) error {
	now := time.Now()

	// Send Data with telnet / socket communication
	uri := fmt.Sprintf("%s:%d", o.Host, o.Port)
	tcpAddr, _ := net.ResolveTCPAddr("tcp", uri)
	connection, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return fmt.Errorf("OpenTSDB: Telnet connect fail")
	}
	defer connection.Close()

	for _, m := range metrics {
		for _, metric := range buildMetrics(m, now, o.Prefix) {
			messageLine := fmt.Sprintf("put %s %v %s %s\n",
				metric.Metric, metric.Timestamp, metric.Value, metric.Tags)
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

func buildTags(mTags map[string]string) []string {
	tags := make([]string, len(mTags))
	index := 0
	for k, v := range mTags {
		tags[index] = sanitizedChars.Replace(fmt.Sprintf("%s=%s", k, v))
		index++
	}
	sort.Strings(tags)
	return tags
}

func buildMetrics(m telegraf.Metric, now time.Time, prefix string) []*MetricLine {
	ret := []*MetricLine{}
	for fieldName, value := range m.Fields() {
		metric := &MetricLine{
			Metric: sanitizedChars.Replace(fmt.Sprintf("%s%s_%s",
				prefix, m.Name(), fieldName)),
			Timestamp: now.Unix(),
		}

		metricValue, buildError := buildValue(value)
		if buildError != nil {
			fmt.Printf("OpenTSDB: %s\n", buildError.Error())
			continue
		}
		metric.Value = metricValue
		tagsSlice := buildTags(m.Tags())
		metric.Tags = fmt.Sprint(strings.Join(tagsSlice, " "))
		ret = append(ret, metric)
	}
	return ret
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
