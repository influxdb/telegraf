//go:generate ../../../tools/readme_config_includer/generator
package fireboard

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Fireboard struct {
	AuthToken   string          `toml:"auth_token"`
	URL         string          `toml:"url"`
	HTTPTimeout config.Duration `toml:"http_timeout"`

	client *http.Client
}

type rtt struct {
	Temp       float64 `json:"temp"`
	Channel    int64   `json:"channel"`
	DegreeType int     `json:"degreetype"`
	Created    string  `json:"created"`
}

type fireboardStats struct {
	Title       string `json:"title"`
	UUID        string `json:"uuid"`
	LatestTemps []rtt  `json:"latest_temps"`
}

func (*Fireboard) SampleConfig() string {
	return sampleConfig
}

func (r *Fireboard) Init() error {
	if len(r.AuthToken) == 0 {
		return errors.New("you must specify an authToken")
	}
	if len(r.URL) == 0 {
		r.URL = "https://fireboard.io/api/v1/devices.json"
	}
	// Have a default timeout of 4s
	if r.HTTPTimeout == 0 {
		r.HTTPTimeout = config.Duration(time.Second * 4)
	}

	r.client.Timeout = time.Duration(r.HTTPTimeout)

	return nil
}

func (r *Fireboard) Gather(acc telegraf.Accumulator) error {
	// Perform the GET request to the fireboard servers
	req, err := http.NewRequest("GET", r.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+r.AuthToken)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Successful responses will always return status code 200
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("fireboard server responded with %d [Forbidden], verify your authToken", resp.StatusCode)
		}
		return fmt.Errorf("fireboard responded with unexpected status code %d", resp.StatusCode)
	}
	// Decode the response JSON into a new stats struct
	var stats []fireboardStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return fmt.Errorf("unable to decode fireboard response: %w", err)
	}
	// Range over all devices, gathering stats. Returns early in case of any error.
	for _, s := range stats {
		gatherTemps(s, acc)
	}
	return nil
}

// Return text description of degree type (scale)
func scale(n int) string {
	switch n {
	case 1:
		return "Celcius"
	case 2:
		return "Fahrenheit"
	default:
		return ""
	}
}

// Gathers stats from a single device, adding them to the accumulator
func gatherTemps(s fireboardStats, acc telegraf.Accumulator) {
	// Construct lookup for scale values

	for _, t := range s.LatestTemps {
		tags := map[string]string{
			"title":   s.Title,
			"uuid":    s.UUID,
			"channel": strconv.FormatInt(t.Channel, 10),
			"scale":   scale(t.DegreeType),
		}
		fields := map[string]interface{}{
			"temperature": t.Temp,
		}
		acc.AddFields("fireboard", fields, tags)
	}
}

func newFireboard() *Fireboard {
	tr := &http.Transport{ResponseHeaderTimeout: 3 * time.Second}
	client := &http.Client{
		Transport: tr,
		Timeout:   4 * time.Second,
	}
	return &Fireboard{client: client}
}

func init() {
	inputs.Add("fireboard", func() telegraf.Input {
		return newFireboard()
	})
}
