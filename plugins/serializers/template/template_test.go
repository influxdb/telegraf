package template

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
)

var basicMetric = metric.New(
	"cpu",
	map[string]string{
		"host": "localhost",
		"cpu":  "CPU0",
	},
	map[string]interface{}{
		"x": 42.0,
		"y": 43.0,
	},
	time.Unix(100, 0),
)

var tests = []struct {
	name      string
	input     telegraf.Metric
	template  string
	output    []byte
	errReason string
}{
	{
		name:     "name",
		input:    basicMetric,
		template: "{{ .Name }}",
		output:   []byte("cpu"),
	},
	{
		name:     "time",
		input:    basicMetric,
		template: "{{ .Time.Unix }}",
		output:   []byte("100"),
	},
	{
		name:     "specific field",
		input:    basicMetric,
		template: `{{ .Field "x" }}`,
		output:   []byte("42"),
	},
	{
		name:     "specific tag",
		input:    basicMetric,
		template: `{{ .Tag "cpu" }}`,
		output:   []byte("CPU0"),
	},
	{
		name:     "all fields",
		input:    basicMetric,
		template: `{{ range $k, $v := .Fields }}{{$k}}={{$v}},{{end}}`,
		output:   []byte("x=42,y=43,"),
	},
	{
		name:     "all tags",
		input:    basicMetric,
		template: `{{ range $k, $v := .Tags }}{{$k}}={{$v}},{{end}}`,
		output:   []byte("cpu=CPU0,host=localhost,"),
	},
	{
		name:     "string",
		input:    basicMetric,
		template: "{{ .String }}",
		output:   []byte("cpu map[cpu:CPU0 host:localhost] map[x:42 y:43] 100000000000"),
	},
	{
		name: "complex",
		input: metric.New(
			"cpu",
			map[string]string{
				"tag1": "tag",
			},
			map[string]interface{}{
				"value": 42.0,
			},
			time.Unix(0, 0),
		),
		template: `{{ .Name }} {{ range $k, $v := .Fields}}{{$k}}={{$v}}{{end}} {{ .Tag "tag1" }} {{.Time.UnixNano}} literal`,
		output:   []byte("cpu value=42 tag 0 literal"),
	},
}

func TestSerializer(t *testing.T) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serializer := &Serializer{
				Template: tt.template,
			}
			require.NoError(t, serializer.Init())
			output, err := serializer.Serialize(tt.input)
			if tt.errReason != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errReason)
			}
			require.Equal(t, string(tt.output), string(output))
		})
	}
}

func TestSerializeBatch(t *testing.T) {
	m := metric.New(
		"cpu",
		map[string]string{},
		map[string]interface{}{
			"value": 42.0,
		},
		time.Unix(0, 0),
	)
	metrics := []telegraf.Metric{m, m}
	s := &Serializer{Template: `{{ range $index, $metric := . }}{{$index}}: {{$metric.Name}} {{$metric.Field "value"}}
{{end}}`}
	require.NoError(t, s.Init())
	buf, err := s.SerializeBatch(metrics)
	require.NoError(t, err)
	require.Equal(
		t,
		string(buf),
		`0: cpu 42
1: cpu 42
`,
	)
}
