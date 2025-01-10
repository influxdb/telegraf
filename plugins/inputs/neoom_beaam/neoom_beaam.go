//go:generate ../../../tools/config_includer/generator
//go:generate ../../../tools/readme_config_includer/generator
package neoom_beaam

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	chttp "github.com/influxdata/telegraf/plugins/common/http"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type NeoomBeaam struct {
	Address       string          `toml:"address"`
	Token         config.Secret   `toml:"token"`
	RefreshConfig bool            `toml:"refresh_configuration"`
	Log           telegraf.Logger `toml:"-"`
	chttp.HTTPClientConfig

	source string
	config site
	client *http.Client
}

func (*NeoomBeaam) SampleConfig() string {
	return sampleConfig
}

func (n *NeoomBeaam) Init() error {
	if n.Address == "" {
		n.Address = "https://10.10.10.10"
	}
	n.Address = strings.TrimRight(n.Address, "/")

	u, err := url.Parse(n.Address)
	if err != nil {
		return fmt.Errorf("parsing of address %q failed: %w", n.Address, err)
	}
	n.source = u.Hostname()

	return nil
}

func (n *NeoomBeaam) Start(telegraf.Accumulator) error {
	// Create the client
	ctx := context.Background()
	client, err := n.HTTPClientConfig.CreateClient(ctx, n.Log)
	if err != nil {
		return fmt.Errorf("creating client failed: %w", err)
	}
	n.client = client

	// Initialize configuration
	return n.updateConfiguration()
}

func (n *NeoomBeaam) Gather(acc telegraf.Accumulator) error {
	// Refresh the config if requested
	if n.RefreshConfig {
		if err := n.updateConfiguration(); err != nil {
			return err
		}
	}

	// Query the energy flow
	if err := n.queryEnergyFlow(acc); err != nil {
		acc.AddError(fmt.Errorf("querying site state failed: %w", err))
	}

	// Query all known things
	for _, thing := range n.config.Things {
		if err := n.queryThing(acc, thing); err != nil {
			acc.AddError(fmt.Errorf("querying thing %q (%s) failed: %w", thing.Name, thing.id, err))
		}
	}

	return nil
}

func (n *NeoomBeaam) Stop() {
	if n.client != nil {
		n.client.CloseIdleConnections()
	}
}

func (n *NeoomBeaam) updateConfiguration() error {
	endpoint := n.Address + "/api/v1/site/configuration"
	request, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("creating configuration request failed: %w", err)
	}

	if !n.Token.Empty() {
		token, err := n.Token.Get()
		if err != nil {
			return fmt.Errorf("getting token failed: %w", err)
		}
		bearer := "Bearer " + strings.TrimSpace(token.String())
		token.Destroy()
		request.Header.Set("Authorization", bearer)
	}
	request.Header.Set("Accept", "application/json")

	// Update the configuration
	response, err := n.client.Do(request)
	if err != nil {
		return &internal.StartupError{
			Err:   fmt.Errorf("querying configuration failed: %w", err),
			Retry: true,
		}
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("reading body failed: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return &internal.StartupError{
			Err:   fmt.Errorf("configuration query returned %q: %s", response.Status, string(body)),
			Retry: 400 <= response.StatusCode && response.StatusCode <= 499,
		}
	}

	if err := json.Unmarshal(body, &n.config); err != nil {
		return fmt.Errorf("decoding configuration failed: %w", err)
	}

	for id, thing := range n.config.Things {
		thing.id = id
		n.config.Things[id] = thing
	}

	return nil
}

func (n *NeoomBeaam) queryEnergyFlow(acc telegraf.Accumulator) error {
	// Create the request
	endpoint := n.Address + "/api/v1/site/state"
	request, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("creating request failed: %w", err)
	}

	if !n.Token.Empty() {
		token, err := n.Token.Get()
		if err != nil {
			return fmt.Errorf("getting token failed: %w", err)
		}
		bearer := "Bearer " + strings.TrimSpace(token.String())
		token.Destroy()
		request.Header.Set("Authorization", bearer)
	}
	request.Header.Set("Accept", "application/json")

	// Execute query
	response, err := n.client.Do(request)
	if err != nil {
		return fmt.Errorf("querying failed: %w", err)
	}
	defer response.Body.Close()

	// Handle response
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("reading body failed: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("query returned %q: %s", response.Status, string(body))
	}

	// Decode the data and create metric
	var data siteState
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("decoding failed: %w", err)
	}

	for _, s := range data.EnergyFlow.States {
		if s.Value == nil {
			n.Log.Debugf("omitting data point %q (%s) due to 'null' value", s.Key, s.DataPointID)
			continue
		}

		dp, ok := n.config.EnergyFlow.DataPoints[s.DataPointID]
		if !ok {
			n.Log.Errorf("no data point definition for ID %q", s.DataPointID)
			continue
		}

		tags := map[string]string{
			"source":    n.source,
			"datapoint": s.Key,
			"unit":      dp.Unit,
		}
		fields := map[string]interface{}{
			"value": s.Value,
		}
		ts := time.Unix(0, int64(s.Timestamp*float64(time.Millisecond)))
		acc.AddFields("neoom_beaam_energy_flow", fields, tags, ts)
	}

	return nil
}

func (n *NeoomBeaam) queryThing(acc telegraf.Accumulator, thing thingDefinition) error {
	// Create the request
	endpoint := n.Address + "/api/v1/things/" + thing.id + "/states"
	request, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("creating request failed: %w", err)
	}

	if !n.Token.Empty() {
		token, err := n.Token.Get()
		if err != nil {
			return fmt.Errorf("getting token failed: %w", err)
		}
		bearer := "Bearer " + strings.TrimSpace(token.String())
		token.Destroy()
		request.Header.Set("Authorization", bearer)
	}
	request.Header.Set("Accept", "application/json")

	// Execute query
	response, err := n.client.Do(request)
	if err != nil {
		return fmt.Errorf("querying failed: %w", err)
	}
	defer response.Body.Close()

	// Handle response
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("reading body failed: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("query returned %q: %s", response.Status, string(body))
	}

	// Decode the data and create metric
	var data thingState
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("decoding failed: %w", err)
	}

	for _, s := range data.States {
		if s.Value == nil {
			n.Log.Debugf("omitting data point %q (%s) due to 'null' value", s.Key, s.DataPointID)
			continue
		}

		dp, ok := thing.DataPoints[s.DataPointID]
		if !ok {
			n.Log.Errorf("no data point definition for ID %q", s.DataPointID)
			continue
		}

		tags := map[string]string{
			"source":    n.source,
			"thing":     thing.Name,
			"datapoint": s.Key,
			"unit":      dp.Unit,
		}
		var fields map[string]interface{}
		if elements, ok := s.Value.([]interface{}); ok {
			fields = make(map[string]interface{}, len(elements))
			for i, v := range elements {
				fields["value_"+strconv.Itoa(i)] = v
			}
		} else {
			fields = map[string]interface{}{
				"value": s.Value,
			}
		}

		ts := time.Unix(0, int64(s.Timestamp*float64(time.Millisecond)))
		acc.AddFields("neoom_beaam_thing", fields, tags, ts)
	}

	return nil
}

// Register the plugin
func init() {
	inputs.Add("neoom_beaam", func() telegraf.Input {
		return &NeoomBeaam{
			HTTPClientConfig: chttp.HTTPClientConfig{
				Timeout:               config.Duration(5 * time.Second),
				ResponseHeaderTimeout: config.Duration(5 * time.Second),
			},
		}
	})
}
