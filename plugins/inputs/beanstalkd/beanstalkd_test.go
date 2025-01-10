package beanstalkd_test

import (
	"errors"
	"io"
	"net"
	"net/textproto"
	"testing"

	"github.com/influxdata/telegraf/plugins/inputs/beanstalkd"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestBeanstalkd(t *testing.T) {
	type tubeStats struct {
		name   string
		fields map[string]interface{}
	}

	tests := []struct {
		name             string
		tubesConfig      []string
		expectedTubes    []tubeStats
		notExpectedTubes []tubeStats
		expectedError    string
	}{
		{
			name: "All tubes stats",
			expectedTubes: []tubeStats{
				{name: "default", fields: defaultTubeFields},
				{name: "test", fields: testTubeFields},
			},
		},
		{
			name:        "Specified tubes stats",
			tubesConfig: []string{"test"},
			expectedTubes: []tubeStats{
				{name: "test", fields: testTubeFields},
			},
			notExpectedTubes: []tubeStats{
				{name: "default", fields: defaultTubeFields},
			},
		},
		{
			name:        "Unknown tube stats",
			tubesConfig: []string{"unknown"},
			notExpectedTubes: []tubeStats{
				{name: "default", fields: defaultTubeFields},
				{name: "test", fields: testTubeFields},
			},
			expectedError: "input does not match format",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, err := startTestServer(t)
			require.NoError(t, err, "Unable to create test server")
			defer server.Close()

			serverAddress := server.Addr().String()
			plugin := beanstalkd.Beanstalkd{
				Server: serverAddress,
				Tubes:  test.tubesConfig,
			}

			var acc testutil.Accumulator
			err = acc.GatherError(plugin.Gather)
			if test.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, test.expectedError, err.Error())
			}
			acc.AssertContainsTaggedFields(t, "beanstalkd_overview",
				overviewFields,
				getOverviewTags(serverAddress),
			)

			for _, expectedTube := range test.expectedTubes {
				acc.AssertContainsTaggedFields(t, "beanstalkd_tube",
					expectedTube.fields,
					getTubeTags(serverAddress, expectedTube.name),
				)
			}

			for _, notExpectedTube := range test.notExpectedTubes {
				acc.AssertDoesNotContainsTaggedFields(t, "beanstalkd_tube",
					notExpectedTube.fields,
					getTubeTags(serverAddress, notExpectedTube.name),
				)
			}
		})
	}
}

func startTestServer(t *testing.T) (net.Listener, error) {
	server, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	go func() {
		defer server.Close()

		connection, err := server.Accept()
		if err != nil {
			t.Log("Test server: failed to accept connection. Error: ", err)
			return
		}

		tp := textproto.NewConn(connection)
		defer tp.Close()

		sendSuccessResponse := func(body string) error {
			return tp.PrintfLine("OK %d\r\n%s", len(body), body)
		}

		for {
			cmd, err := tp.ReadLine()
			if errors.Is(err, io.EOF) {
				return
			} else if err != nil {
				t.Log("Test server: failed read command. Error: ", err)
				return
			}

			switch cmd {
			case "list-tubes":
				if err := sendSuccessResponse(listTubesResponse); err != nil {
					t.Logf("sending response %q failed: %v", listTubesResponse, err)
					return
				}
			case "stats":
				if err := sendSuccessResponse(statsResponse); err != nil {
					t.Logf("sending response %q failed: %v", statsResponse, err)
					return
				}
			case "stats-tube default":
				if err := sendSuccessResponse(statsTubeDefaultResponse); err != nil {
					t.Logf("sending response %q failed: %v", statsTubeDefaultResponse, err)
					return
				}
			case "stats-tube test":
				if err := sendSuccessResponse(statsTubeTestResponse); err != nil {
					t.Logf("sending response %q failed: %v", statsTubeTestResponse, err)
					return
				}
			case "stats-tube unknown":
				if err := tp.PrintfLine("NOT_FOUND"); err != nil {
					t.Logf("sending response %q failed: %v", "NOT_FOUND", err)
					return
				}
			default:
				t.Log("Test server: unknown command")
			}
		}
	}()

	return server, nil
}

const (
	listTubesResponse = `---
- default
- test
`
	statsResponse = `---
current-jobs-urgent: 5
current-jobs-ready: 5
current-jobs-reserved: 0
current-jobs-delayed: 1
current-jobs-buried: 0
cmd-put: 6
cmd-peek: 0
cmd-peek-ready: 1
cmd-peek-delayed: 0
cmd-peek-buried: 0
cmd-reserve: 0
cmd-reserve-with-timeout: 1
cmd-delete: 1
cmd-release: 0
cmd-use: 2
cmd-watch: 0
cmd-ignore: 0
cmd-bury: 1
cmd-kick: 1
cmd-touch: 0
cmd-stats: 1
cmd-stats-job: 0
cmd-stats-tube: 2
cmd-list-tubes: 1
cmd-list-tube-used: 0
cmd-list-tubes-watched: 0
cmd-pause-tube: 0
job-timeouts: 0
total-jobs: 6
max-job-size: 65535
current-tubes: 2
current-connections: 2
current-producers: 1
current-workers: 1
current-waiting: 0
total-connections: 2
pid: 6
version: 1.10
rusage-utime: 0.000000
rusage-stime: 0.000000
uptime: 20
binlog-oldest-index: 0
binlog-current-index: 0
binlog-records-migrated: 0
binlog-records-written: 0
binlog-max-size: 10485760
id: bba7546657efdd4c
hostname: 2873efd3e88c
`
	statsTubeDefaultResponse = `---
name: default
current-jobs-urgent: 0
current-jobs-ready: 0
current-jobs-reserved: 0
current-jobs-delayed: 0
current-jobs-buried: 0
total-jobs: 0
current-using: 2
current-watching: 2
current-waiting: 0
cmd-delete: 0
cmd-pause-tube: 0
pause: 0
pause-time-left: 0
`
	statsTubeTestResponse = `---
name: test
current-jobs-urgent: 5
current-jobs-ready: 5
current-jobs-reserved: 0
current-jobs-delayed: 1
current-jobs-buried: 0
total-jobs: 6
current-using: 0
current-watching: 0
current-waiting: 0
cmd-delete: 0
cmd-pause-tube: 0
pause: 0
pause-time-left: 0
`
)

var (
	// Default tube without stats
	defaultTubeFields = map[string]interface{}{
		"cmd_delete":            0,
		"cmd_pause_tube":        0,
		"current_jobs_buried":   0,
		"current_jobs_delayed":  0,
		"current_jobs_ready":    0,
		"current_jobs_reserved": 0,
		"current_jobs_urgent":   0,
		"current_using":         2,
		"current_waiting":       0,
		"current_watching":      2,
		"pause":                 0,
		"pause_time_left":       0,
		"total_jobs":            0,
	}
	// Test tube with stats
	testTubeFields = map[string]interface{}{
		"cmd_delete":            0,
		"cmd_pause_tube":        0,
		"current_jobs_buried":   0,
		"current_jobs_delayed":  1,
		"current_jobs_ready":    5,
		"current_jobs_reserved": 0,
		"current_jobs_urgent":   5,
		"current_using":         0,
		"current_waiting":       0,
		"current_watching":      0,
		"pause":                 0,
		"pause_time_left":       0,
		"total_jobs":            6,
	}
	// Server stats
	overviewFields = map[string]interface{}{
		"binlog_current_index":     0,
		"binlog_max_size":          10485760,
		"binlog_oldest_index":      0,
		"binlog_records_migrated":  0,
		"binlog_records_written":   0,
		"cmd_bury":                 1,
		"cmd_delete":               1,
		"cmd_ignore":               0,
		"cmd_kick":                 1,
		"cmd_list_tube_used":       0,
		"cmd_list_tubes":           1,
		"cmd_list_tubes_watched":   0,
		"cmd_pause_tube":           0,
		"cmd_peek":                 0,
		"cmd_peek_buried":          0,
		"cmd_peek_delayed":         0,
		"cmd_peek_ready":           1,
		"cmd_put":                  6,
		"cmd_release":              0,
		"cmd_reserve":              0,
		"cmd_reserve_with_timeout": 1,
		"cmd_stats":                1,
		"cmd_stats_job":            0,
		"cmd_stats_tube":           2,
		"cmd_touch":                0,
		"cmd_use":                  2,
		"cmd_watch":                0,
		"current_connections":      2,
		"current_jobs_buried":      0,
		"current_jobs_delayed":     1,
		"current_jobs_ready":       5,
		"current_jobs_reserved":    0,
		"current_jobs_urgent":      5,
		"current_producers":        1,
		"current_tubes":            2,
		"current_waiting":          0,
		"current_workers":          1,
		"job_timeouts":             0,
		"max_job_size":             65535,
		"pid":                      6,
		"rusage_stime":             0.0,
		"rusage_utime":             0.0,
		"total_connections":        2,
		"total_jobs":               6,
		"uptime":                   20,
	}
)

func getOverviewTags(server string) map[string]string {
	return map[string]string{
		"hostname": "2873efd3e88c",
		"id":       "bba7546657efdd4c",
		"server":   server,
		"version":  "1.10",
	}
}

func getTubeTags(server, tube string) map[string]string {
	return map[string]string{
		"name":   tube,
		"server": server,
	}
}
