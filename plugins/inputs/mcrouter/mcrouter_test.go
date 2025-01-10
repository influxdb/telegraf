package mcrouter

import (
	"bufio"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/influxdata/telegraf/testutil"
)

func TestAddressParsing(t *testing.T) {
	var acceptTests = [][3]string{
		{"tcp://localhost:8086", "localhost:8086", "tcp"},
		{"tcp://localhost", "localhost:" + defaultServerURL.Port(), "tcp"},
		{"tcp://localhost:", "localhost:" + defaultServerURL.Port(), "tcp"},
		{"tcp://:8086", defaultServerURL.Hostname() + ":8086", "tcp"},
		{"tcp://:", defaultServerURL.Host, "tcp"},
	}

	var rejectTests = []string{
		"tcp://",
	}

	for _, args := range acceptTests {
		address, protocol, err := parseAddress(args[0])

		require.NoError(t, err, args[0])
		require.Equal(t, args[1], address, args[0])
		require.Equal(t, args[2], protocol, args[0])
	}

	for _, addr := range rejectTests {
		address, protocol, err := parseAddress(addr)

		require.Error(t, err, addr)
		require.Empty(t, address, addr)
		require.Empty(t, protocol, addr)
	}
}

func TestMcrouterGeneratesMetricsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	servicePort := "11211"
	container := testutil.Container{
		Image:        "memcached",
		ExposedPorts: []string{servicePort},
		WaitingFor:   wait.ForListeningPort(nat.Port(servicePort)),
	}
	err := container.Start()
	require.NoError(t, err, "failed to start container")
	defer container.Terminate()

	m := &Mcrouter{
		Servers: []string{
			fmt.Sprintf("tcp://%s:%s", container.Address, container.Ports[servicePort]),
		},
	}

	var acc testutil.Accumulator

	// wait for the uptime stat to show up
	require.Eventually(t, func() bool {
		err = acc.GatherError(m.Gather)
		require.NoError(t, err)
		return acc.HasInt64Field("mcrouter", "uptime")
	}, 5*time.Second, 10*time.Millisecond)

	intMetrics := []string{
		"uptime",
		"cmd_set",
		"cmd_get",
	}

	floatMetrics := []string{
		"rusage_system",
		"rusage_user",
	}

	for _, metric := range intMetrics {
		require.True(t, acc.HasInt64Field("mcrouter", metric), metric)
	}

	for _, metric := range floatMetrics {
		require.True(t, acc.HasFloatField("mcrouter", metric), metric)
	}
}

func TestMcrouterParseMetrics(t *testing.T) {
	filePath := "testdata/mcrouter_stats"
	file, err := os.Open(filePath)
	require.NoErrorf(t, err, "could not read from file %s", filePath)

	r := bufio.NewReader(file)
	scanner := bufio.NewScanner(r)
	values, err := parseResponse(scanner)
	require.NoError(t, err, "Error parsing mcrouter response")

	tests := []struct {
		key   string
		value string
	}{
		{"uptime", "166"},
		{"num_servers", "1"},
		{"num_servers_new", "1"},
		{"num_servers_up", "0"},
		{"num_servers_down", "0"},
		{"num_servers_closed", "0"},
		{"num_clients", "1"},
		{"num_suspect_servers", "0"},
		{"destination_batches_sum", "0"},
		{"destination_requests_sum", "0"},
		{"outstanding_route_get_reqs_queued", "0"},
		{"outstanding_route_update_reqs_queued", "0"},
		{"outstanding_route_get_avg_queue_size", "0"},
		{"outstanding_route_update_avg_queue_size", "0"},
		{"outstanding_route_get_avg_wait_time_sec", "0"},
		{"outstanding_route_update_avg_wait_time_sec", "0"},
		{"retrans_closed_connections", "0"},
		{"destination_pending_reqs", "0"},
		{"destination_inflight_reqs", "0"},
		{"destination_batch_size", "0"},
		{"asynclog_requests", "0"},
		{"proxy_reqs_processing", "1"},
		{"proxy_reqs_waiting", "0"},
		{"client_queue_notify_period", "0"},
		{"rusage_system", "0.040966"},
		{"rusage_user", "0.020483"},
		{"ps_num_minor_faults", "2490"},
		{"ps_num_major_faults", "11"},
		{"ps_user_time_sec", "0.02"},
		{"ps_system_time_sec", "0.04"},
		{"ps_vsize", "697741312"},
		{"ps_rss", "10563584"},
		{"fibers_allocated", "0"},
		{"fibers_pool_size", "0"},
		{"fibers_stack_high_watermark", "0"},
		{"successful_client_connections", "18"},
		{"duration_us", "0"},
		{"destination_max_pending_reqs", "0"},
		{"destination_max_inflight_reqs", "0"},
		{"retrans_per_kbyte_max", "0"},
		{"cmd_get_count", "0"},
		{"cmd_delete_out", "0"},
		{"cmd_lease_get", "0"},
		{"cmd_set", "0"},
		{"cmd_get_out_all", "0"},
		{"cmd_get_out", "0"},
		{"cmd_lease_set_count", "0"},
		{"cmd_other_out_all", "0"},
		{"cmd_lease_get_out", "0"},
		{"cmd_set_count", "0"},
		{"cmd_lease_set_out", "0"},
		{"cmd_delete_count", "0"},
		{"cmd_other", "0"},
		{"cmd_delete", "0"},
		{"cmd_get", "0"},
		{"cmd_lease_set", "0"},
		{"cmd_set_out", "0"},
		{"cmd_lease_get_count", "0"},
		{"cmd_other_out", "0"},
		{"cmd_lease_get_out_all", "0"},
		{"cmd_set_out_all", "0"},
		{"cmd_other_count", "0"},
		{"cmd_delete_out_all", "0"},
		{"cmd_lease_set_out_all", "0"},
	}

	for _, test := range tests {
		value, ok := values[test.key]
		if !ok {
			t.Errorf("Did not find key for metric %s in values", test.key)
			continue
		}
		if value != test.value {
			t.Errorf("Metric: %s, Expected: %s, actual: %s",
				test.key, test.value, value)
		}
	}
}
