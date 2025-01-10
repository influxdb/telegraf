//go:generate ../../../tools/readme_config_includer/generator
package mock

import (
	_ "embed"
	"math"
	"math/rand"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Mock struct {
	MetricName string            `toml:"metric_name"`
	Tags       map[string]string `toml:"tags"`

	Constant []*constant `toml:"constant"`
	Random   []*random   `toml:"random"`
	Step     []*step     `toml:"step"`
	Stock    []*stock    `toml:"stock"`
	SineWave []*sineWave `toml:"sine_wave"`

	counter int64
	rand    *rand.Rand
}

type constant struct {
	Name  string      `toml:"name"`
	Value interface{} `toml:"value"`
}

type random struct {
	Name string  `toml:"name"`
	Min  float64 `toml:"min"`
	Max  float64 `toml:"max"`
}

type sineWave struct {
	Name      string  `toml:"name"`
	Amplitude float64 `toml:"amplitude"`
	Period    float64 `toml:"period"`
	Phase     float64 `toml:"phase"`
	BaseLine  float64 `toml:"base_line"`
}

type step struct {
	Name  string  `toml:"name"`
	Start float64 `toml:"start"`
	Step  float64 `toml:"step"`

	Min float64 `toml:"min" deprecated:"1.28.2;1.35.0;use 'start' instead"`
	Max float64 `toml:"max" deprecated:"1.28.2;1.35.0;use 'step' instead"`

	latest float64
}

type stock struct {
	Name       string  `toml:"name"`
	Price      float64 `toml:"price"`
	Volatility float64 `toml:"volatility"`

	latest float64
}

func (*Mock) SampleConfig() string {
	return sampleConfig
}

func (m *Mock) Init() error {
	m.rand = rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // G404: not security critical

	// backward compatibility
	for _, step := range m.Step {
		if step.Min != 0 && step.Start == 0 {
			step.Start = step.Min
		}
		if step.Max != 0 && step.Step == 0 {
			step.Step = step.Max
		}
	}

	return nil
}

func (m *Mock) Gather(acc telegraf.Accumulator) error {
	fields := make(map[string]interface{})
	m.generateRandomFloat64(fields)
	m.generateStockPrice(fields)
	m.generateSineWave(fields)
	m.generateStep(fields)

	for _, c := range m.Constant {
		fields[c.Name] = c.Value
	}

	tags := make(map[string]string)
	for key, value := range m.Tags {
		tags[key] = value
	}

	acc.AddFields(m.MetricName, fields, tags)

	m.counter++

	return nil
}

// Generate random value between min and max, inclusively
func (m *Mock) generateRandomFloat64(fields map[string]interface{}) {
	for _, random := range m.Random {
		fields[random.Name] = random.Min + m.rand.Float64()*(random.Max-random.Min)
	}
}

// Create sine waves
func (m *Mock) generateSineWave(fields map[string]interface{}) {
	for _, field := range m.SineWave {
		fields[field.Name] = math.Sin((float64(m.counter)+field.Phase)*field.Period*math.Pi)*field.Amplitude + field.BaseLine
	}
}

// Begin at start value and then add step value every tick
func (m *Mock) generateStep(fields map[string]interface{}) {
	for _, step := range m.Step {
		if m.counter == 0 {
			step.latest = step.Start
		} else {
			step.latest += step.Step
		}

		fields[step.Name] = step.latest
	}
}

// Begin at start price and then generate random value
func (m *Mock) generateStockPrice(fields map[string]interface{}) {
	for _, stock := range m.Stock {
		if stock.latest == 0.0 {
			stock.latest = stock.Price
		} else {
			noise := 2 * (m.rand.Float64() - 0.5)
			stock.latest = stock.latest + (stock.latest * stock.Volatility * noise)

			// avoid going below zero
			if stock.latest < 1.0 {
				stock.latest = 1.0
			}
		}

		fields[stock.Name] = stock.latest
	}
}

func init() {
	inputs.Add("mock", func() telegraf.Input {
		return &Mock{}
	})
}
