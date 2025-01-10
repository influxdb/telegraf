//go:generate ../../../tools/readme_config_includer/generator
package mesos

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
	parsers_json "github.com/influxdata/telegraf/plugins/parsers/json"
)

//go:embed sample.conf
var sampleConfig string

type role string

const (
	master role = "master"
	slave  role = "slave"
)

var allMetrics = map[role][]string{
	master: {"resources", "master", "system", "agents", "frameworks", "framework_offers", "tasks", "messages", "evqueue", "registrar", "allocator"},
	slave:  {"resources", "agent", "system", "executors", "tasks", "messages"},
}

type Mesos struct {
	Timeout    int      `toml:"timeout"`
	Masters    []string `toml:"masters"`
	MasterCols []string `toml:"master_collections"`
	Slaves     []string `toml:"slaves"`
	SlaveCols  []string `toml:"slave_collections"`
	tls.ClientConfig

	Log telegraf.Logger `toml:"-"`

	initialized bool
	client      *http.Client
	masterURLs  []*url.URL
	slaveURLs   []*url.URL
}

func (*Mesos) SampleConfig() string {
	return sampleConfig
}

func (m *Mesos) Gather(acc telegraf.Accumulator) error {
	if !m.initialized {
		err := m.initialize()
		if err != nil {
			return err
		}
		m.initialized = true
	}

	var wg sync.WaitGroup

	for _, mstr := range m.masterURLs {
		wg.Add(1)
		go func(mstr *url.URL) {
			acc.AddError(m.gatherMainMetrics(mstr, master, acc))
			wg.Done()
		}(mstr)
	}

	for _, slv := range m.slaveURLs {
		wg.Add(1)
		go func(slv *url.URL) {
			acc.AddError(m.gatherMainMetrics(slv, slave, acc))
			wg.Done()
		}(slv)
	}

	wg.Wait()

	return nil
}

func (m *Mesos) parseURL(s string, role role) (*url.URL, error) {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		host, port, err := net.SplitHostPort(s)
		// no port specified
		if err != nil {
			host = s
			switch role {
			case master:
				port = "5050"
			case slave:
				port = "5051"
			}
		}

		s = "http://" + host + ":" + port
		m.Log.Warnf("using %q as connection URL; please update your configuration to use an URL", s)
	}

	return url.Parse(s)
}

func (m *Mesos) initialize() error {
	if len(m.MasterCols) == 0 {
		m.MasterCols = allMetrics[master]
	}

	if len(m.SlaveCols) == 0 {
		m.SlaveCols = allMetrics[slave]
	}

	if m.Timeout == 0 {
		m.Log.Info("Missing timeout value, setting default value (100ms)")
		m.Timeout = 100
	}

	rawQuery := "timeout=" + strconv.Itoa(m.Timeout) + "ms"

	m.masterURLs = make([]*url.URL, 0, len(m.Masters))
	for _, mstr := range m.Masters {
		u, err := m.parseURL(mstr, master)
		if err != nil {
			return err
		}

		u.RawQuery = rawQuery
		m.masterURLs = append(m.masterURLs, u)
	}

	m.slaveURLs = make([]*url.URL, 0, len(m.Slaves))
	for _, slv := range m.Slaves {
		u, err := m.parseURL(slv, slave)
		if err != nil {
			return err
		}

		u.RawQuery = rawQuery
		m.slaveURLs = append(m.slaveURLs, u)
	}

	client, err := m.createHTTPClient()
	if err != nil {
		return err
	}
	m.client = client

	return nil
}

func (m *Mesos) createHTTPClient() (*http.Client, error) {
	tlsCfg, err := m.ClientConfig.TLSConfig()
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: tlsCfg,
		},
		Timeout: 4 * time.Second,
	}

	return client, nil
}

// metricsDiff() returns set names for removal
func metricsDiff(role role, w []string) []string {
	b := make([]string, 0, len(allMetrics[role]))
	s := make(map[string]bool)

	if len(w) == 0 {
		return b
	}

	for _, v := range w {
		s[v] = true
	}

	for _, d := range allMetrics[role] {
		if _, ok := s[d]; !ok {
			b = append(b, d)
		}
	}

	return b
}

// masterBlocks serves as kind of metrics registry grouping them in sets
func (m *Mesos) getMetrics(role role, group string) []string {
	metrics := make(map[string][]string)

	if role == master {
		metrics["resources"] = []string{
			"master/cpus_percent",
			"master/cpus_used",
			"master/cpus_total",
			"master/cpus_revocable_percent",
			"master/cpus_revocable_total",
			"master/cpus_revocable_used",
			"master/disk_percent",
			"master/disk_used",
			"master/disk_total",
			"master/disk_revocable_percent",
			"master/disk_revocable_total",
			"master/disk_revocable_used",
			"master/gpus_percent",
			"master/gpus_used",
			"master/gpus_total",
			"master/gpus_revocable_percent",
			"master/gpus_revocable_total",
			"master/gpus_revocable_used",
			"master/mem_percent",
			"master/mem_used",
			"master/mem_total",
			"master/mem_revocable_percent",
			"master/mem_revocable_total",
			"master/mem_revocable_used",
		}

		metrics["master"] = []string{
			"master/elected",
			"master/uptime_secs",
		}

		metrics["system"] = []string{
			"system/cpus_total",
			"system/load_15min",
			"system/load_5min",
			"system/load_1min",
			"system/mem_free_bytes",
			"system/mem_total_bytes",
		}

		metrics["agents"] = []string{
			"master/slave_registrations",
			"master/slave_removals",
			"master/slave_reregistrations",
			"master/slave_shutdowns_scheduled",
			"master/slave_shutdowns_canceled",
			"master/slave_shutdowns_completed",
			"master/slaves_active",
			"master/slaves_connected",
			"master/slaves_disconnected",
			"master/slaves_inactive",
			"master/slave_unreachable_canceled",
			"master/slave_unreachable_completed",
			"master/slave_unreachable_scheduled",
			"master/slaves_unreachable",
		}

		metrics["frameworks"] = []string{
			"master/frameworks_active",
			"master/frameworks_connected",
			"master/frameworks_disconnected",
			"master/frameworks_inactive",
			"master/outstanding_offers",
		}

		// framework_offers and allocator metrics have unpredictable names, so they can't be listed here.
		// These empty groups are included to prevent the "unknown metrics group" info log below.
		// filterMetrics() filters these metrics by looking for names with the corresponding prefix.
		metrics["framework_offers"] = make([]string, 0)
		metrics["allocator"] = make([]string, 0)

		metrics["tasks"] = []string{
			"master/tasks_error",
			"master/tasks_failed",
			"master/tasks_finished",
			"master/tasks_killed",
			"master/tasks_lost",
			"master/tasks_running",
			"master/tasks_staging",
			"master/tasks_starting",
			"master/tasks_dropped",
			"master/tasks_gone",
			"master/tasks_gone_by_operator",
			"master/tasks_killing",
			"master/tasks_unreachable",
		}

		metrics["messages"] = []string{
			"master/invalid_executor_to_framework_messages",
			"master/invalid_framework_to_executor_messages",
			"master/invalid_status_update_acknowledgements",
			"master/invalid_status_updates",
			"master/dropped_messages",
			"master/messages_authenticate",
			"master/messages_deactivate_framework",
			"master/messages_decline_offers",
			"master/messages_executor_to_framework",
			"master/messages_exited_executor",
			"master/messages_framework_to_executor",
			"master/messages_kill_task",
			"master/messages_launch_tasks",
			"master/messages_reconcile_tasks",
			"master/messages_register_framework",
			"master/messages_register_slave",
			"master/messages_reregister_framework",
			"master/messages_reregister_slave",
			"master/messages_resource_request",
			"master/messages_revive_offers",
			"master/messages_status_update",
			"master/messages_status_update_acknowledgement",
			"master/messages_unregister_framework",
			"master/messages_unregister_slave",
			"master/messages_update_slave",
			"master/recovery_slave_removals",
			"master/slave_removals/reason_registered",
			"master/slave_removals/reason_unhealthy",
			"master/slave_removals/reason_unregistered",
			"master/valid_framework_to_executor_messages",
			"master/valid_status_update_acknowledgements",
			"master/valid_status_updates",
			"master/task_lost/source_master/reason_invalid_offers",
			"master/task_lost/source_master/reason_slave_removed",
			"master/task_lost/source_slave/reason_executor_terminated",
			"master/valid_executor_to_framework_messages",
			"master/invalid_operation_status_update_acknowledgements",
			"master/messages_operation_status_update_acknowledgement",
			"master/messages_reconcile_operations",
			"master/messages_suppress_offers",
			"master/valid_operation_status_update_acknowledgements",
		}

		metrics["evqueue"] = []string{
			"master/event_queue_dispatches",
			"master/event_queue_http_requests",
			"master/event_queue_messages",
			"master/operator_event_stream_subscribers",
		}

		metrics["registrar"] = []string{
			"registrar/state_fetch_ms",
			"registrar/state_store_ms",
			"registrar/state_store_ms/max",
			"registrar/state_store_ms/min",
			"registrar/state_store_ms/p50",
			"registrar/state_store_ms/p90",
			"registrar/state_store_ms/p95",
			"registrar/state_store_ms/p99",
			"registrar/state_store_ms/p999",
			"registrar/state_store_ms/p9999",
			"registrar/log/ensemble_size",
			"registrar/log/recovered",
			"registrar/queued_operations",
			"registrar/registry_size_bytes",
			"registrar/state_store_ms/count",
		}
	} else if role == slave {
		metrics["resources"] = []string{
			"slave/cpus_percent",
			"slave/cpus_used",
			"slave/cpus_total",
			"slave/cpus_revocable_percent",
			"slave/cpus_revocable_total",
			"slave/cpus_revocable_used",
			"slave/disk_percent",
			"slave/disk_used",
			"slave/disk_total",
			"slave/disk_revocable_percent",
			"slave/disk_revocable_total",
			"slave/disk_revocable_used",
			"slave/gpus_percent",
			"slave/gpus_used",
			"slave/gpus_total",
			"slave/gpus_revocable_percent",
			"slave/gpus_revocable_total",
			"slave/gpus_revocable_used",
			"slave/mem_percent",
			"slave/mem_used",
			"slave/mem_total",
			"slave/mem_revocable_percent",
			"slave/mem_revocable_total",
			"slave/mem_revocable_used",
		}

		metrics["agent"] = []string{
			"slave/registered",
			"slave/uptime_secs",
		}

		metrics["system"] = []string{
			"system/cpus_total",
			"system/load_15min",
			"system/load_5min",
			"system/load_1min",
			"system/mem_free_bytes",
			"system/mem_total_bytes",
		}

		metrics["executors"] = []string{
			"containerizer/mesos/container_destroy_errors",
			"slave/container_launch_errors",
			"slave/executors_preempted",
			"slave/frameworks_active",
			"slave/executor_directory_max_allowed_age_secs",
			"slave/executors_registering",
			"slave/executors_running",
			"slave/executors_terminated",
			"slave/executors_terminating",
			"slave/recovery_errors",
		}

		metrics["tasks"] = []string{
			"slave/tasks_failed",
			"slave/tasks_finished",
			"slave/tasks_killed",
			"slave/tasks_lost",
			"slave/tasks_running",
			"slave/tasks_staging",
			"slave/tasks_starting",
		}

		metrics["messages"] = []string{
			"slave/invalid_framework_messages",
			"slave/invalid_status_updates",
			"slave/valid_framework_messages",
			"slave/valid_status_updates",
		}
	}

	ret, ok := metrics[group]
	if !ok {
		m.Log.Infof("Unknown role %q metrics group: %s", role, group)
		return nil
	}

	return ret
}

func (m *Mesos) filterMetrics(role role, metrics *map[string]interface{}) {
	var ok bool
	var selectedMetrics []string

	if role == master {
		selectedMetrics = m.MasterCols
	} else if role == slave {
		selectedMetrics = m.SlaveCols
	}

	for _, k := range metricsDiff(role, selectedMetrics) {
		switch k {
		// allocator and framework_offers metrics have unpredictable names, so we have to identify them by name prefix.
		case "allocator":
			for m := range *metrics {
				if strings.HasPrefix(m, "allocator/") {
					delete(*metrics, m)
				}
			}
		case "framework_offers":
			for m := range *metrics {
				if strings.HasPrefix(m, "master/frameworks/") || strings.HasPrefix(m, "frameworks/") {
					delete(*metrics, m)
				}
			}

		// All other metrics have predictable names. We can use getMetrics() to retrieve them.
		default:
			for _, v := range m.getMetrics(role, k) {
				if _, ok = (*metrics)[v]; ok {
					delete(*metrics, v)
				}
			}
		}
	}
}

func withPath(u *url.URL, path string) *url.URL {
	c := *u
	c.Path = path
	return &c
}

func urlTag(u *url.URL) string {
	c := *u
	c.Path = ""
	c.User = nil
	c.RawQuery = ""
	return c.String()
}

// This should not belong to the object
func (m *Mesos) gatherMainMetrics(u *url.URL, role role, acc telegraf.Accumulator) error {
	var jsonOut map[string]interface{}

	tags := map[string]string{
		"server": u.Hostname(),
		"url":    urlTag(u),
		"role":   string(role),
	}

	resp, err := m.client.Get(withPath(u, "/metrics/snapshot").String())

	if err != nil {
		return err
	}

	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	if err = json.Unmarshal(data, &jsonOut); err != nil {
		return errors.New("error decoding JSON response")
	}

	m.filterMetrics(role, &jsonOut)

	jf := parsers_json.JSONFlattener{}

	err = jf.FlattenJSON("", jsonOut)

	if err != nil {
		return err
	}

	if role == master {
		if jf.Fields["master/elected"] != 0.0 {
			tags["state"] = "leader"
		} else {
			tags["state"] = "standby"
		}
	}

	acc.AddFields("mesos", jf.Fields, tags)

	return nil
}

func init() {
	inputs.Add("mesos", func() telegraf.Input {
		return &Mesos{}
	})
}
