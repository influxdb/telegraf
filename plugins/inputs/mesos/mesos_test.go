package mesos

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

var masterMetrics map[string]interface{}
var masterTestServer *httptest.Server
var slaveMetrics map[string]interface{}

var slaveTestServer *httptest.Server

// master metrics that will be returned by generateMetrics()
var masterMetricNames = []string{
	// resources
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
	// master
	"master/elected",
	"master/uptime_secs",
	// system
	"system/cpus_total",
	"system/load_15min",
	"system/load_5min",
	"system/load_1min",
	"system/mem_free_bytes",
	"system/mem_total_bytes",
	// agents
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
	// frameworks
	"master/frameworks_active",
	"master/frameworks_connected",
	"master/frameworks_disconnected",
	"master/frameworks_inactive",
	"master/outstanding_offers",
	// framework offers
	"master/frameworks/marathon/abc-123/calls",
	"master/frameworks/marathon/abc-123/calls/accept",
	"master/frameworks/marathon/abc-123/events",
	"master/frameworks/marathon/abc-123/events/error",
	"master/frameworks/marathon/abc-123/offers/sent",
	"master/frameworks/marathon/abc-123/operations",
	"master/frameworks/marathon/abc-123/operations/create",
	"master/frameworks/marathon/abc-123/roles/*/suppressed",
	"master/frameworks/marathon/abc-123/subscribed",
	"master/frameworks/marathon/abc-123/tasks/active/task_killing",
	"master/frameworks/marathon/abc-123/tasks/active/task_dropped",
	"master/frameworks/marathon/abc-123/tasks/terminal/task_dropped",
	"master/frameworks/marathon/abc-123/unknown/unknown", // test case for unknown metric type
	// tasks
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
	// messages
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
	// evgqueue
	"master/event_queue_dispatches",
	"master/event_queue_http_requests",
	"master/event_queue_messages",
	"master/operator_event_stream_subscribers",
	// registrar
	"registrar/log/ensemble_size",
	"registrar/log/recovered",
	"registrar/queued_operations",
	"registrar/registry_size_bytes",
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
	"registrar/state_store_ms/count",
	// allocator
	"allocator/mesos/allocation_run_ms",
	"allocator/mesos/allocation_run_ms/count",
	"allocator/mesos/allocation_run_ms/max",
	"allocator/mesos/allocation_run_ms/min",
	"allocator/mesos/allocation_run_ms/p50",
	"allocator/mesos/allocation_run_ms/p90",
	"allocator/mesos/allocation_run_ms/p95",
	"allocator/mesos/allocation_run_ms/p99",
	"allocator/mesos/allocation_run_ms/p999",
	"allocator/mesos/allocation_run_ms/p9999",
	"allocator/mesos/allocation_runs",
	"allocator/mesos/allocation_run_latency_ms",
	"allocator/mesos/allocation_run_latency_ms/count",
	"allocator/mesos/allocation_run_latency_ms/max",
	"allocator/mesos/allocation_run_latency_ms/min",
	"allocator/mesos/allocation_run_latency_ms/p50",
	"allocator/mesos/allocation_run_latency_ms/p90",
	"allocator/mesos/allocation_run_latency_ms/p95",
	"allocator/mesos/allocation_run_latency_ms/p99",
	"allocator/mesos/allocation_run_latency_ms/p999",
	"allocator/mesos/allocation_run_latency_ms/p9999",
	"allocator/mesos/roles/*/shares/dominant",
	"allocator/mesos/event_queue_dispatches",
	"allocator/mesos/offer_filters/roles/*/active",
	"allocator/mesos/quota/roles/*/resources/disk/offered_or_allocated",
	"allocator/mesos/quota/roles/*/resources/mem/guarantee",
	"allocator/mesos/quota/roles/*/resources/disk/guarantee",
	"allocator/mesos/resources/cpus/offered_or_allocated",
	"allocator/mesos/resources/cpus/total",
	"allocator/mesos/resources/disk/offered_or_allocated",
	"allocator/mesos/resources/disk/total",
	"allocator/mesos/resources/mem/offered_or_allocated",
	"allocator/mesos/resources/mem/total",
}

// slave metrics that will be returned by generateMetrics()
var slaveMetricNames = []string{
	// resources
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
	// agent
	"slave/registered",
	"slave/uptime_secs",
	// system
	"system/cpus_total",
	"system/load_15min",
	"system/load_5min",
	"system/load_1min",
	"system/mem_free_bytes",
	"system/mem_total_bytes",
	// executors
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
	// tasks
	"slave/tasks_failed",
	"slave/tasks_finished",
	"slave/tasks_killed",
	"slave/tasks_lost",
	"slave/tasks_running",
	"slave/tasks_staging",
	"slave/tasks_starting",
	// messages
	"slave/invalid_framework_messages",
	"slave/invalid_status_updates",
	"slave/valid_framework_messages",
	"slave/valid_status_updates",
}

func generateMetrics() {
	masterMetrics = make(map[string]interface{})
	for _, k := range masterMetricNames {
		masterMetrics[k] = rand.Float64()
	}

	slaveMetrics = make(map[string]interface{})
	for _, k := range slaveMetricNames {
		slaveMetrics[k] = rand.Float64()
	}
}

func TestMain(m *testing.M) {
	generateMetrics()

	masterRouter := http.NewServeMux()
	masterRouter.HandleFunc("/metrics/snapshot", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(masterMetrics) //nolint:errcheck // ignore the returned error as we cannot do anything about it anyway
	})
	masterTestServer = httptest.NewServer(masterRouter)

	slaveRouter := http.NewServeMux()
	slaveRouter.HandleFunc("/metrics/snapshot", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(slaveMetrics) //nolint:errcheck // ignore the returned error as we cannot do anything about it anyway
	})
	slaveTestServer = httptest.NewServer(slaveRouter)

	rc := m.Run()

	masterTestServer.Close()
	slaveTestServer.Close()
	os.Exit(rc)
}

func TestMesosMaster(t *testing.T) {
	var acc testutil.Accumulator

	m := Mesos{
		Log:     testutil.Logger{},
		Masters: []string{masterTestServer.Listener.Addr().String()},
		Timeout: 10,
	}

	require.NoError(t, acc.GatherError(m.Gather))

	acc.AssertContainsFields(t, "mesos", masterMetrics)
}

func TestMasterFilter(t *testing.T) {
	m := Mesos{
		Log: testutil.Logger{},
		MasterCols: []string{
			"resources", "master", "registrar", "allocator",
		},
	}
	b := []string{
		"system", "agents", "frameworks",
		"messages", "evqueue", "tasks",
	}

	m.filterMetrics(master, &masterMetrics)

	// Assert expected metrics are present.
	for _, v := range m.MasterCols {
		for _, x := range m.getMetrics(master, v) {
			_, ok := masterMetrics[x]
			require.Truef(t, ok, "Didn't find key %s, it should present.", x)
		}
	}
	// m.MasterCols includes "allocator", so allocator metrics should be present.
	// allocator metrics have unpredictable names, so we can't rely on the list of metrics returned from
	// getMetrics(). We have to find them by checking name prefixes.
	for _, x := range masterMetricNames {
		if strings.HasPrefix(x, "allocator/") {
			_, ok := masterMetrics[x]
			require.Truef(t, ok, "Didn't find key %s, it should present.", x)
		}
	}

	// Assert unexpected metrics are not present.
	for _, v := range b {
		for _, x := range m.getMetrics(master, v) {
			_, ok := masterMetrics[x]
			require.Falsef(t, ok, "Found key %s, it should be gone.", x)
		}
	}
	// m.MasterCols does not include "framework_offers", so framework_offers metrics should not be present.
	// framework_offers metrics have unpredictable names, so we can't rely on the list of metrics returned from
	// getMetrics(). We have to find them by checking name prefixes.
	for k := range masterMetrics {
		if strings.HasPrefix(k, "master/frameworks/") || strings.HasPrefix(k, "frameworks/") {
			require.Failf(t, "Found key %s, it should be gone.", k)
		}
	}
}

func TestMesosSlave(t *testing.T) {
	var acc testutil.Accumulator

	m := Mesos{
		Log:    testutil.Logger{},
		Slaves: []string{slaveTestServer.Listener.Addr().String()},
		// SlaveTasks: true,
		Timeout: 10,
	}

	require.NoError(t, acc.GatherError(m.Gather))

	acc.AssertContainsFields(t, "mesos", slaveMetrics)
}

func TestSlaveFilter(t *testing.T) {
	m := Mesos{
		Log: testutil.Logger{},
		SlaveCols: []string{
			"resources", "agent", "tasks",
		},
	}
	b := []string{
		"system", "executors", "messages",
	}

	m.filterMetrics(slave, &slaveMetrics)

	for _, v := range b {
		for _, x := range m.getMetrics(slave, v) {
			_, ok := slaveMetrics[x]
			require.Falsef(t, ok, "Found key %s, it should be gone.", x)
		}
	}
	for _, v := range m.MasterCols {
		for _, x := range m.getMetrics(slave, v) {
			_, ok := slaveMetrics[x]
			require.Truef(t, ok, "Didn't find key %s, it should present.", x)
		}
	}
}

func TestWithPathDoesNotModify(t *testing.T) {
	u, err := url.Parse("http://localhost:5051")
	require.NoError(t, err)
	v := withPath(u, "/xyzzy")
	require.Equal(t, "http://localhost:5051", u.String())
	require.Equal(t, "http://localhost:5051/xyzzy", v.String())
}

func TestURLTagDoesNotModify(t *testing.T) {
	u, err := url.Parse("http://a:b@localhost:5051?timeout=1ms")
	require.NoError(t, err)
	v := urlTag(u)
	require.Equal(t, "http://a:b@localhost:5051?timeout=1ms", u.String())
	require.Equal(t, "http://localhost:5051", v)
}
