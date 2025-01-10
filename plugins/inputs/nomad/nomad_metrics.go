package nomad

import (
	"time"
)

type metricsSummary struct {
	Timestamp string         `json:"timestamp"`
	Gauges    []gaugeValue   `json:"gauges"`
	Points    []pointValue   `json:"points"`
	Counters  []sampledValue `json:"counters"`
	Samples   []sampledValue `json:"samples"`
}

type gaugeValue struct {
	Name  string  `json:"name"`
	Hash  string  `json:"-"`
	Value float32 `json:"value"`

	Labels        []label           `json:"-"`
	DisplayLabels map[string]string `json:"Labels"`
}

type pointValue struct {
	Name   string    `json:"name"`
	Points []float32 `json:"points"`
}

type sampledValue struct {
	Name string `json:"name"`
	Hash string `json:"-"`
	*AggregateSample
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"`

	Labels        []label           `json:"-"`
	DisplayLabels map[string]string `json:"Labels"`
}

// AggregateSample needs to be exported, because JSON decode cannot set embedded pointer to unexported struct
type AggregateSample struct {
	Count       int       `json:"count"`
	Rate        float64   `json:"rate"`
	Sum         float64   `json:"sum"`
	SumSq       float64   `json:"-"`
	Min         float64   `json:"min"`
	Max         float64   `json:"max"`
	LastUpdated time.Time `json:"-"`
}

type label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
