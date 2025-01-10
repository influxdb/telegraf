//go:build linux

package systemd_units

import (
	"context"
	"fmt"
	"math"
	"os/user"
	"strings"
	"testing"
	"time"

	sdbus "github.com/coreos/go-systemd/v22/dbus"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
)

type properties struct {
	uf            *sdbus.UnitFile
	utype         string
	state         *sdbus.UnitStatus
	ufPreset      string
	ufState       string
	ufActiveEnter uint64
	properties    map[string]interface{}
}

func TestDefaultPattern(t *testing.T) {
	plugin := &SystemdUnits{}
	require.NoError(t, plugin.Init())
	require.Equal(t, "*", plugin.Pattern)
}

func TestDefaultScope(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		return
	}

	tests := []struct {
		name          string
		scope         string
		expectedScope string
		expectedUser  string
	}{
		{
			name:          "default scope",
			scope:         "",
			expectedScope: "system",
			expectedUser:  "",
		},
		{
			name:          "system scope",
			scope:         "system",
			expectedScope: "system",
			expectedUser:  "",
		},
		{
			name:          "user scope",
			scope:         "user",
			expectedScope: "user",
			expectedUser:  u.Username,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &SystemdUnits{
				Scope: tt.scope,
			}
			require.NoError(t, plugin.Init())
			require.Equal(t, tt.expectedScope, plugin.scope)
			require.Equal(t, tt.expectedUser, plugin.user)
		})
	}
}

func TestListFiles(t *testing.T) {
	tests := []struct {
		name        string
		properties  map[string]properties
		line        string
		expected    []telegraf.Metric
		expectedErr string
	}{
		{
			name: "example loaded active running",
			line: "example.service                loaded active running  example service description",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "loaded",
						ActiveState: "active",
						SubState:    "running",
					},
				},
			},
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "example.service",
						"load":   "loaded",
						"active": "active",
						"sub":    "running",
					},
					map[string]interface{}{
						"load_code":   0,
						"active_code": 0,
						"sub_code":    0,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "example loaded active exited",
			line: "example.service                loaded active exited  example service description",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "loaded",
						ActiveState: "active",
						SubState:    "exited",
					},
				},
			},
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "example.service",
						"load":   "loaded",
						"active": "active",
						"sub":    "exited",
					},
					map[string]interface{}{
						"load_code":   0,
						"active_code": 0,
						"sub_code":    4,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "example loaded failed failed",
			line: "example.service                loaded failed failed  example service description",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "loaded",
						ActiveState: "failed",
						SubState:    "failed",
					},
				},
			},
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "example.service",
						"load":   "loaded",
						"active": "failed",
						"sub":    "failed",
					},
					map[string]interface{}{
						"load_code":   0,
						"active_code": 3,
						"sub_code":    12,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "example not-found inactive dead",
			line: "example.service                not-found inactive dead  example service description",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "not-found",
						ActiveState: "inactive",
						SubState:    "dead",
					},
				},
			},
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "example.service",
						"load":   "not-found",
						"active": "inactive",
						"sub":    "dead",
					},
					map[string]interface{}{
						"load_code":   2,
						"active_code": 2,
						"sub_code":    1,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "example unknown unknown unknown",
			line: "example.service                unknown unknown unknown  example service description",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "unknown",
						ActiveState: "unknown",
						SubState:    "unknown",
					},
				},
			},
			expectedErr: "parsing field 'load' failed, value not in map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run a meta-test for finding regressions compared to metrics
			// emitted by previous versions
			old, err := oldParseListUnits(tt.line)
			if tt.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			testutil.RequireMetricsEqual(t, old, tt.expected, testutil.IgnoreTime())

			// Setup plugin. Do NOT call Start() as this would connect to
			// the real systemd daemon.
			plugin := &SystemdUnits{
				Pattern: "examp*",
				Timeout: config.Duration(time.Second),
				Log:     testutil.Logger{},
			}
			require.NoError(t, plugin.Init())

			// Create a fake client to inject data
			client := &fakeClient{
				units:     tt.properties,
				connected: true,
			}
			client.fixPropertyTypes()
			plugin.client = client
			defer plugin.Stop()

			// Run gather
			var acc testutil.Accumulator
			err = acc.GatherError(plugin.Gather)
			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)

			// Do the comparison
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, tt.expected, actual, testutil.IgnoreTime())
		})
	}
}

func TestShow(t *testing.T) {
	enter := time.Now().UnixMicro()
	tests := []struct {
		name        string
		properties  map[string]properties
		expected    []telegraf.Metric
		expectedErr string
	}{
		{
			name: "example loaded active running",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "loaded",
						ActiveState: "active",
						SubState:    "running",
					},
					ufPreset:      "disabled",
					ufState:       "enabled",
					ufActiveEnter: uint64(enter),
					properties: map[string]interface{}{
						"Id":                "example.service",
						"StatusErrno":       0,
						"NRestarts":         1,
						"MemoryCurrent":     1000,
						"MemoryPeak":        2000,
						"MemorySwapCurrent": 3000,
						"MemorySwapPeak":    4000,
						"MemoryAvailable":   5000,
						"MainPID":           9999,
					},
				},
			},
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "example.service",
						"load":   "loaded",
						"active": "active",
						"sub":    "running",
						"state":  "enabled",
						"preset": "disabled",
					},
					map[string]interface{}{
						"load_code":                 0,
						"active_code":               0,
						"sub_code":                  0,
						"status_errno":              0,
						"restarts":                  1,
						"mem_current":               uint64(1000),
						"mem_peak":                  uint64(2000),
						"swap_current":              uint64(3000),
						"swap_peak":                 uint64(4000),
						"mem_avail":                 uint64(5000),
						"pid":                       9999,
						"active_enter_timestamp_us": uint64(enter),
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "example loaded active exited",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "loaded",
						ActiveState: "active",
						SubState:    "exited",
					},
					ufPreset:      "disabled",
					ufState:       "enabled",
					ufActiveEnter: 0,
					properties: map[string]interface{}{
						"Id":          "example.service",
						"StatusErrno": 0,
						"NRestarts":   0,
					},
				},
			},
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "example.service",
						"load":   "loaded",
						"active": "active",
						"sub":    "exited",
						"state":  "enabled",
						"preset": "disabled",
					},
					map[string]interface{}{
						"load_code":                 0,
						"active_code":               0,
						"sub_code":                  4,
						"status_errno":              0,
						"restarts":                  0,
						"mem_current":               uint64(0),
						"mem_peak":                  uint64(0),
						"swap_current":              uint64(0),
						"swap_peak":                 uint64(0),
						"mem_avail":                 uint64(0),
						"active_enter_timestamp_us": uint64(0),
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "example loaded failed failed",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "loaded",
						ActiveState: "failed",
						SubState:    "failed",
					},
					ufPreset:      "disabled",
					ufState:       "enabled",
					ufActiveEnter: uint64(enter),
					properties: map[string]interface{}{
						"Id":                "example.service",
						"StatusErrno":       10,
						"NRestarts":         1,
						"MemoryCurrent":     1000,
						"MemoryPeak":        2000,
						"MemorySwapCurrent": 3000,
						"MemorySwapPeak":    4000,
						"MemoryAvailable":   5000,
					},
				},
			},
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "example.service",
						"load":   "loaded",
						"active": "failed",
						"sub":    "failed",
						"state":  "enabled",
						"preset": "disabled",
					},
					map[string]interface{}{
						"load_code":                 0,
						"active_code":               3,
						"sub_code":                  12,
						"status_errno":              10,
						"restarts":                  1,
						"mem_current":               uint64(1000),
						"mem_peak":                  uint64(2000),
						"swap_current":              uint64(3000),
						"swap_peak":                 uint64(4000),
						"mem_avail":                 uint64(5000),
						"active_enter_timestamp_us": uint64(enter),
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "example not-found inactive dead",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "not-found",
						ActiveState: "inactive",
						SubState:    "dead",
					},
					ufPreset:      "disabled",
					ufState:       "enabled",
					ufActiveEnter: uint64(0),
					properties: map[string]interface{}{
						"Id": "example.service",
					},
				},
			},
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "example.service",
						"load":   "not-found",
						"active": "inactive",
						"sub":    "dead",
						"state":  "enabled",
						"preset": "disabled",
					},
					map[string]interface{}{
						"load_code":                 2,
						"active_code":               2,
						"sub_code":                  1,
						"mem_current":               uint64(0),
						"mem_peak":                  uint64(0),
						"swap_current":              uint64(0),
						"swap_peak":                 uint64(0),
						"mem_avail":                 uint64(0),
						"active_enter_timestamp_us": uint64(0),
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "example unknown unknown unknown",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "unknown",
						ActiveState: "unknown",
						SubState:    "unknown",
					},
					ufPreset: "unknown",
					ufState:  "unknown",
					properties: map[string]interface{}{
						"Id": "example.service",
					},
				},
			},
			expectedErr: "parsing field 'load' failed, value not in map",
		},
		{
			name: "example loaded but inactive with unset fields",
			properties: map[string]properties{
				"example.service": {
					utype: "Service",
					state: &sdbus.UnitStatus{
						Name:        "example.service",
						LoadState:   "loaded",
						ActiveState: "inactive",
						SubState:    "dead",
					},
					ufPreset:      "disabled",
					ufState:       "disabled",
					ufActiveEnter: uint64(0),
					properties: map[string]interface{}{
						"Id":                "example.service",
						"StatusErrno":       0,
						"NRestarts":         0,
						"MemoryCurrent":     uint64(math.MaxUint64),
						"MemoryPeak":        uint64(math.MaxUint64),
						"MemorySwapCurrent": uint64(math.MaxUint64),
						"MemorySwapPeak":    uint64(math.MaxUint64),
						"MemoryAvailable":   uint64(math.MaxUint64),
					},
				},
			},
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "example.service",
						"load":   "loaded",
						"active": "inactive",
						"sub":    "dead",
						"state":  "disabled",
						"preset": "disabled",
					},
					map[string]interface{}{
						"load_code":                 0,
						"active_code":               int64(2),
						"sub_code":                  1,
						"status_errno":              0,
						"restarts":                  0,
						"mem_current":               uint64(0),
						"mem_peak":                  uint64(0),
						"swap_current":              uint64(0),
						"swap_peak":                 uint64(0),
						"mem_avail":                 uint64(0),
						"active_enter_timestamp_us": uint64(0),
					},
					time.Unix(0, 0),
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup plugin. Do NOT call Start() as this would connect to
			// the real systemd daemon.
			plugin := &SystemdUnits{
				Pattern: "examp*",
				Details: true,
				Timeout: config.Duration(time.Second),
				Log:     testutil.Logger{},
			}
			require.NoError(t, plugin.Init())

			// Create a fake client to inject data
			client := &fakeClient{
				units:     tt.properties,
				connected: true,
			}
			client.fixPropertyTypes()
			plugin.client = client
			defer plugin.Stop()

			// Run gather
			var acc testutil.Accumulator
			err := acc.GatherError(plugin.Gather)
			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)

			// Do the comparison
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, tt.expected, actual, testutil.IgnoreTime())
		})
	}
}

func TestMultiInstance(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected []telegraf.Metric
	}{
		{
			name:    "multiple without concrete instance",
			pattern: "examp* user@*",
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "example.service",
						"load":   "loaded",
						"active": "active",
						"sub":    "running",
					},
					map[string]interface{}{
						"load_code":   0,
						"active_code": 0,
						"sub_code":    0,
					},
					time.Unix(0, 0),
				),
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "user@1000.service",
						"load":   "loaded",
						"active": "active",
						"sub":    "running",
					},
					map[string]interface{}{
						"load_code":   0,
						"active_code": 0,
						"sub_code":    0,
					},
					time.Unix(0, 0),
				),
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "user@1001.service",
						"load":   "loaded",
						"active": "active",
						"sub":    "exited",
					},
					map[string]interface{}{
						"load_code":   0,
						"active_code": 0,
						"sub_code":    4,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:    "multiple without instance prefix",
			pattern: "user@1*",
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "user@1000.service",
						"load":   "loaded",
						"active": "active",
						"sub":    "running",
					},
					map[string]interface{}{
						"load_code":   0,
						"active_code": 0,
						"sub_code":    0,
					},
					time.Unix(0, 0),
				),
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "user@1001.service",
						"load":   "loaded",
						"active": "active",
						"sub":    "exited",
					},
					map[string]interface{}{
						"load_code":   0,
						"active_code": 0,
						"sub_code":    4,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:    "multiple with concrete instance",
			pattern: "user@1001.service",
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "user@1001.service",
						"load":   "loaded",
						"active": "active",
						"sub":    "exited",
					},
					map[string]interface{}{
						"load_code":   0,
						"active_code": 0,
						"sub_code":    4,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:    "static but loaded instance",
			pattern: "shadow*",
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "shadow.service",
						"load":   "loaded",
						"active": "inactive",
						"sub":    "dead",
					},
					map[string]interface{}{
						"load_code":   0,
						"active_code": 2,
						"sub_code":    1,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name:    "static but not loaded instance",
			pattern: "cups*",
			expected: []telegraf.Metric{
				metric.New(
					"systemd_units",
					map[string]string{
						"name":   "cups-lpd@.service",
						"load":   "stub",
						"active": "inactive",
						"sub":    "dead",
					},
					map[string]interface{}{
						"load_code":   1,
						"active_code": 2,
						"sub_code":    1,
					},
					time.Unix(0, 0),
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup plugin. Do NOT call Start() as this would connect to
			// the real systemd daemon.
			plugin := &SystemdUnits{
				Pattern:         tt.pattern,
				CollectDisabled: true,
				Timeout:         config.Duration(time.Second),
				Log:             testutil.Logger{},
			}
			require.NoError(t, plugin.Init())

			// Create a fake client to inject data
			client := &fakeClient{
				units: map[string]properties{
					"example.service": {
						utype: "Service",
						state: &sdbus.UnitStatus{
							Name:        "example.service",
							LoadState:   "loaded",
							ActiveState: "active",
							SubState:    "running",
						},
					},
					"user-runtime-dir@1000.service": {
						uf: &sdbus.UnitFile{
							Path: "user-runtime-dir@.service",
							Type: "static",
						},
						utype: "Service",
						state: &sdbus.UnitStatus{
							Name:        "user-runtime-dir@1000.service",
							LoadState:   "loaded",
							ActiveState: "active",
							SubState:    "exited",
						},
					},
					"user@1000.service": {
						uf: &sdbus.UnitFile{
							Path: "user@.service",
							Type: "static",
						},
						utype: "Service",
						state: &sdbus.UnitStatus{
							Name:        "user@1000.service",
							LoadState:   "loaded",
							ActiveState: "active",
							SubState:    "running",
						},
					},
					"user@1001.service": {
						uf: &sdbus.UnitFile{
							Path: "user@.service",
							Type: "static",
						},
						utype: "Service",
						state: &sdbus.UnitStatus{
							Name:        "user@1001.service",
							LoadState:   "loaded",
							ActiveState: "active",
							SubState:    "exited",
						},
					},
					"shadow.service": {
						uf: &sdbus.UnitFile{
							Path: "shadow.service",
							Type: "static",
						},
						utype: "Service",
						state: &sdbus.UnitStatus{
							Name:        "shadow.service",
							LoadState:   "loaded",
							ActiveState: "inactive",
							SubState:    "dead",
						},
					},
					"cups-lpd@.service": {
						uf: &sdbus.UnitFile{
							Path: "cups-lpd@.service",
							Type: "static",
						},
						utype: "Service",
					},
				},
				connected: true,
			}
			client.fixPropertyTypes()
			plugin.client = client
			defer plugin.Stop()

			// Run gather
			var acc testutil.Accumulator
			require.NoError(t, acc.GatherError(plugin.Gather))

			// Do the comparison
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, tt.expected, actual, testutil.IgnoreTime(), testutil.SortMetrics())
		})
	}
}

func BenchmarkAllUnitsIntegration(b *testing.B) {
	plugin := &SystemdUnits{
		CollectDisabled: true,
		Timeout:         config.Duration(3 * time.Second),
	}
	require.NoError(b, plugin.Init())

	acc := &testutil.Accumulator{Discard: true}
	require.NoError(b, plugin.Start(acc))
	require.NoError(b, acc.GatherError(plugin.Gather))
	require.NotZero(b, acc.NMetrics())
	b.Logf("produced %d metrics", acc.NMetrics())

	for n := 0; n < b.N; n++ {
		//nolint:errcheck // skip check in benchmarking
		_ = plugin.Gather(acc)
	}
}

func BenchmarkAllLoadedUnitsIntegration(b *testing.B) {
	plugin := &SystemdUnits{
		Timeout: config.Duration(3 * time.Second),
	}
	require.NoError(b, plugin.Init())

	acc := &testutil.Accumulator{Discard: true}
	require.NoError(b, plugin.Start(acc))
	require.NoError(b, acc.GatherError(plugin.Gather))
	require.NotZero(b, acc.NMetrics())
	b.Logf("produced %d metrics", acc.NMetrics())

	for n := 0; n < b.N; n++ {
		//nolint:errcheck // skip check in benchmarking
		_ = plugin.Gather(acc)
	}
}

// Fake client implementation
type fakeClient struct {
	units     map[string]properties
	connected bool
}

func (c *fakeClient) fixPropertyTypes() {
	for unit, u := range c.units {
		for k, value := range u.properties {
			if strings.HasPrefix(k, "Memory") {
				//nolint:errcheck // will cause issues later in tests
				u.properties[k], _ = internal.ToUint64(value)
			}
		}
		c.units[unit] = u
	}
}

func (c *fakeClient) Connected() bool {
	return c.connected
}

func (c *fakeClient) Close() {
	c.connected = false
}

func (c *fakeClient) ListUnitFilesByPatternsContext(_ context.Context, _, pattern []string) ([]sdbus.UnitFile, error) {
	f := filter.MustCompile(pattern)

	var files []sdbus.UnitFile
	seen := make(map[string]bool)
	for name, props := range c.units {
		var uf sdbus.UnitFile
		if props.uf != nil && f.Match(props.uf.Path) {
			uf = sdbus.UnitFile{
				Path: "/usr/lib/systemd/system/" + props.uf.Path,
				Type: props.uf.Type,
			}
		} else if props.uf == nil && f.Match(name) {
			uf = sdbus.UnitFile{
				Path: "/usr/lib/systemd/system/" + name,
				Type: "enabled",
			}
		} else {
			continue
		}

		if !seen[uf.Path] {
			files = append(files, uf)
		}
		seen[uf.Path] = true
	}

	return files, nil
}

func (c *fakeClient) ListUnitsByNamesContext(_ context.Context, units []string) ([]sdbus.UnitStatus, error) {
	var states []sdbus.UnitStatus
	for name, u := range c.units {
		for _, requestedName := range units {
			if name == requestedName && u.state != nil {
				states = append(states, *u.state)
				break
			}
		}
	}

	return states, nil
}

func (c *fakeClient) GetUnitTypePropertiesContext(_ context.Context, unit, unitType string) (map[string]interface{}, error) {
	u, found := c.units[unit]
	if !found {
		return nil, nil
	}
	if u.utype != unitType {
		return nil, fmt.Errorf("unknown interface 'org.freedesktop.systemd1.%s", unitType)
	}
	return u.properties, nil
}

func (c *fakeClient) GetUnitPropertiesContext(_ context.Context, unit string) (map[string]interface{}, error) {
	u, found := c.units[unit]
	if !found {
		return nil, nil
	}

	return map[string]interface{}{
		"UnitFileState":        u.ufState,
		"UnitFilePreset":       u.ufPreset,
		"ActiveEnterTimestamp": u.ufActiveEnter,
	}, nil
}

func (c *fakeClient) ListUnitsContext(_ context.Context) ([]sdbus.UnitStatus, error) {
	units := make([]sdbus.UnitStatus, 0, len(c.units))
	for _, u := range c.units {
		if u.state != nil {
			units = append(units, *u.state)
		}
	}
	return units, nil
}

// Slightly adapted version of 'parseListUnits()' function of 'subcommand_list.go'
func oldParseListUnits(line string) ([]telegraf.Metric, error) {
	data := strings.Fields(line)
	if len(data) < 4 {
		return nil, fmt.Errorf("parsing line failed (expected at least 4 fields): %s", line)
	}
	name := data[0]
	load := data[1]
	active := data[2]
	sub := data[3]
	tags := map[string]string{
		"name":   name,
		"load":   load,
		"active": active,
		"sub":    sub,
	}

	var (
		loadCode   int
		activeCode int
		subCode    int
		ok         bool
	)
	if loadCode, ok = loadMap[load]; !ok {
		return nil, fmt.Errorf("parsing field 'load' failed, value not in map: %s", load)
	}
	if activeCode, ok = activeMap[active]; !ok {
		return nil, fmt.Errorf("parsing field field 'active' failed, value not in map: %s", active)
	}
	if subCode, ok = subMap[sub]; !ok {
		return nil, fmt.Errorf("parsing field field 'sub' failed, value not in map: %s", sub)
	}
	fields := map[string]interface{}{
		"load_code":   loadCode,
		"active_code": activeCode,
		"sub_code":    subCode,
	}

	return []telegraf.Metric{metric.New("systemd_units", tags, fields, time.Now())}, nil
}
