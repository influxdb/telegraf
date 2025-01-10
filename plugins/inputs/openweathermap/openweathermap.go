//go:generate ../../../tools/readme_config_includer/generator
package openweathermap

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// https://openweathermap.org/current#severalid
// Limit for the number of city IDs per request.
const maxIDsPerBatch int = 20

type OpenWeatherMap struct {
	AppID           string          `toml:"app_id"`
	CityID          []string        `toml:"city_id"`
	Lang            string          `toml:"lang"`
	Fetch           []string        `toml:"fetch"`
	BaseURL         string          `toml:"base_url"`
	ResponseTimeout config.Duration `toml:"response_timeout"`
	Units           string          `toml:"units"`
	QueryStyle      string          `toml:"query_style"`

	client        *http.Client
	cityIDBatches []string
	baseParsedURL *url.URL
}

func (*OpenWeatherMap) SampleConfig() string {
	return sampleConfig
}

func (n *OpenWeatherMap) Init() error {
	// Set the default for the base-URL if not given
	if n.BaseURL == "" {
		n.BaseURL = "https://api.openweathermap.org/"
	}

	// Check the query-style setting
	switch n.QueryStyle {
	case "":
		n.QueryStyle = "batch"
	case "batch", "individual":
		// Do nothing, those are valid
	default:
		return fmt.Errorf("unknown query-style: %s", n.QueryStyle)
	}

	// Check the unit setting
	switch n.Units {
	case "":
		n.Units = "metric"
	case "imperial", "standard", "metric":
		// Do nothing, those are valid
	default:
		return fmt.Errorf("unknown units: %s", n.Units)
	}

	// Check the language setting
	switch n.Lang {
	case "":
		n.Lang = "en"
	case "ar", "bg", "ca", "cz", "de", "el", "en", "fa", "fi", "fr", "gl",
		"hr", "hu", "it", "ja", "kr", "la", "lt", "mk", "nl", "pl",
		"pt", "ro", "ru", "se", "sk", "sl", "es", "tr", "ua", "vi",
		"zh_cn", "zh_tw":
		// Do nothing, those are valid
	default:
		return fmt.Errorf("unknown language: %s", n.Lang)
	}

	// Check the properties to fetch
	if len(n.Fetch) == 0 {
		n.Fetch = []string{"weather", "forecast"}
	}
	for _, fetch := range n.Fetch {
		switch fetch {
		case "forecast", "weather":
			// Do nothing, those are valid
		default:
			return fmt.Errorf("unknown property to fetch: %s", fetch)
		}
	}

	// Split the city IDs into batches smaller than the maximum size
	nBatches := len(n.CityID) / maxIDsPerBatch
	if len(n.CityID)%maxIDsPerBatch != 0 {
		nBatches++
	}
	batches := make([][]string, nBatches)
	for i, id := range n.CityID {
		batch := i / maxIDsPerBatch
		batches[batch] = append(batches[batch], id)
	}
	n.cityIDBatches = make([]string, 0, nBatches)
	for _, batch := range batches {
		n.cityIDBatches = append(n.cityIDBatches, strings.Join(batch, ","))
	}

	// Parse the base-URL used later to construct the property API endpoint
	u, err := url.Parse(n.BaseURL)
	if err != nil {
		return err
	}
	n.baseParsedURL = u

	// Create an HTTP client to be used in each collection interval
	n.client = &http.Client{
		Transport: &http.Transport{},
		Timeout:   time.Duration(n.ResponseTimeout),
	}

	return nil
}

func (n *OpenWeatherMap) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup
	for _, fetch := range n.Fetch {
		switch fetch {
		case "forecast":
			for _, cityID := range n.CityID {
				wg.Add(1)
				go func(city string) {
					defer wg.Done()
					acc.AddError(n.gatherForecast(acc, city))
				}(cityID)
			}
		case "weather":
			switch n.QueryStyle {
			case "individual":
				for _, cityID := range n.CityID {
					wg.Add(1)
					go func(city string) {
						defer wg.Done()
						acc.AddError(n.gatherWeather(acc, city))
					}(cityID)
				}
			case "batch":
				for _, cityIDs := range n.cityIDBatches {
					wg.Add(1)
					go func(cities string) {
						defer wg.Done()
						acc.AddError(n.gatherWeatherBatch(acc, cities))
					}(cityIDs)
				}
			}
		}
	}

	wg.Wait()
	return nil
}

func (n *OpenWeatherMap) gatherWeather(acc telegraf.Accumulator, city string) error {
	// Query the data and decode the response
	addr := n.formatURL("/data/2.5/weather", city)
	buf, err := n.gatherURL(addr)
	if err != nil {
		return fmt.Errorf("querying %q failed: %w", addr, err)
	}

	var e weatherEntry
	if err := json.Unmarshal(buf, &e); err != nil {
		return fmt.Errorf("parsing JSON response failed: %w", err)
	}

	// Construct the metric
	tm := time.Unix(e.Dt, 0)

	fields := map[string]interface{}{
		"cloudiness":   e.Clouds.All,
		"humidity":     e.Main.Humidity,
		"pressure":     e.Main.Pressure,
		"rain":         e.rain(),
		"snow":         e.snow(),
		"sunrise":      time.Unix(e.Sys.Sunrise, 0).UnixNano(),
		"sunset":       time.Unix(e.Sys.Sunset, 0).UnixNano(),
		"temperature":  e.Main.Temp,
		"feels_like":   e.Main.Feels,
		"visibility":   e.Visibility,
		"wind_degrees": e.Wind.Deg,
		"wind_speed":   e.Wind.Speed,
	}
	tags := map[string]string{
		"city":     e.Name,
		"city_id":  strconv.FormatInt(e.ID, 10),
		"country":  e.Sys.Country,
		"forecast": "*",
	}

	if len(e.Weather) > 0 {
		fields["condition_description"] = e.Weather[0].Description
		fields["condition_icon"] = e.Weather[0].Icon
		tags["condition_id"] = strconv.FormatInt(e.Weather[0].ID, 10)
		tags["condition_main"] = e.Weather[0].Main
	}

	acc.AddFields("weather", fields, tags, tm)

	return nil
}

func (n *OpenWeatherMap) gatherWeatherBatch(acc telegraf.Accumulator, cities string) error {
	// Query the data and decode the response
	addr := n.formatURL("/data/2.5/group", cities)
	buf, err := n.gatherURL(addr)
	if err != nil {
		return fmt.Errorf("querying %q failed: %w", addr, err)
	}

	var status status
	if err := json.Unmarshal(buf, &status); err != nil {
		return fmt.Errorf("parsing JSON response failed: %w", err)
	}

	// Construct the metrics
	for _, e := range status.List {
		tm := time.Unix(e.Dt, 0)

		fields := map[string]interface{}{
			"cloudiness":   e.Clouds.All,
			"humidity":     e.Main.Humidity,
			"pressure":     e.Main.Pressure,
			"rain":         e.rain(),
			"snow":         e.snow(),
			"sunrise":      time.Unix(e.Sys.Sunrise, 0).UnixNano(),
			"sunset":       time.Unix(e.Sys.Sunset, 0).UnixNano(),
			"temperature":  e.Main.Temp,
			"feels_like":   e.Main.Feels,
			"visibility":   e.Visibility,
			"wind_degrees": e.Wind.Deg,
			"wind_speed":   e.Wind.Speed,
		}
		tags := map[string]string{
			"city":     e.Name,
			"city_id":  strconv.FormatInt(e.ID, 10),
			"country":  e.Sys.Country,
			"forecast": "*",
		}

		if len(e.Weather) > 0 {
			fields["condition_description"] = e.Weather[0].Description
			fields["condition_icon"] = e.Weather[0].Icon
			tags["condition_id"] = strconv.FormatInt(e.Weather[0].ID, 10)
			tags["condition_main"] = e.Weather[0].Main
		}

		acc.AddFields("weather", fields, tags, tm)
	}

	return nil
}

func (n *OpenWeatherMap) gatherForecast(acc telegraf.Accumulator, city string) error {
	// Query the data and decode the response
	addr := n.formatURL("/data/2.5/forecast", city)
	buf, err := n.gatherURL(addr)
	if err != nil {
		return fmt.Errorf("querying %q failed: %w", addr, err)
	}

	var status status
	if err := json.Unmarshal(buf, &status); err != nil {
		return fmt.Errorf("parsing JSON response failed: %w", err)
	}

	// Construct the metric
	tags := map[string]string{
		"city_id":  strconv.FormatInt(status.City.ID, 10),
		"forecast": "*",
		"city":     status.City.Name,
		"country":  status.City.Country,
	}
	for i, e := range status.List {
		tm := time.Unix(e.Dt, 0)
		fields := map[string]interface{}{
			"cloudiness":   e.Clouds.All,
			"humidity":     e.Main.Humidity,
			"pressure":     e.Main.Pressure,
			"rain":         e.rain(),
			"snow":         e.snow(),
			"temperature":  e.Main.Temp,
			"feels_like":   e.Main.Feels,
			"wind_degrees": e.Wind.Deg,
			"wind_speed":   e.Wind.Speed,
		}
		if len(e.Weather) > 0 {
			fields["condition_description"] = e.Weather[0].Description
			fields["condition_icon"] = e.Weather[0].Icon
			tags["condition_id"] = strconv.FormatInt(e.Weather[0].ID, 10)
			tags["condition_main"] = e.Weather[0].Main
		}
		tags["forecast"] = fmt.Sprintf("%dh", (i+1)*3)
		acc.AddFields("weather", fields, tags, tm)
	}

	return nil
}

func (n *OpenWeatherMap) formatURL(path, city string) string {
	v := url.Values{
		"id":    []string{city},
		"APPID": []string{n.AppID},
		"lang":  []string{n.Lang},
		"units": []string{n.Units},
	}

	relative := &url.URL{
		Path:     path,
		RawQuery: v.Encode(),
	}

	return n.baseParsedURL.ResolveReference(relative).String()
}

func (n *OpenWeatherMap) gatherURL(addr string) ([]byte, error) {
	resp, err := n.client.Get(addr)
	if err != nil {
		return nil, fmt.Errorf("error making HTTP request to %q: %w", addr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP status %s", addr, resp.Status)
	}

	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}

	if mediaType != "application/json" {
		return nil, fmt.Errorf("%s returned unexpected content type %s", addr, mediaType)
	}

	return io.ReadAll(resp.Body)
}

func init() {
	inputs.Add("openweathermap", func() telegraf.Input {
		return &OpenWeatherMap{
			ResponseTimeout: config.Duration(5 * time.Second),
		}
	})
}
