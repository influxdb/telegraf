//go:build !windows

// TODO: Windows - should be enabled for Windows when super asterisk is fixed on Windows
// https://github.com/influxdata/telegraf/issues/6248

package phpfpm

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/http/fcgi"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
)

type statServer struct{}

// We create a fake server to return test data
func (statServer) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Length", strconv.Itoa(len(outputSample)))
	fmt.Fprint(w, outputSample)
}

func TestPhpFpmGeneratesMetrics_From_Http(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("test") != "ok" {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", "ok", r.URL.Query().Get("test"))
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(outputSample)))
		if _, err := fmt.Fprint(w, outputSample); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer ts.Close()

	url := ts.URL + "?test=ok"
	r := &Phpfpm{
		Urls: []string{url},
		Log:  &testutil.Logger{},
	}

	require.NoError(t, r.Init())

	var acc testutil.Accumulator

	require.NoError(t, acc.GatherError(r.Gather))

	tags := map[string]string{
		"pool": "www",
		"url":  url,
	}

	fields := map[string]interface{}{
		"start_since":          int64(1991),
		"accepted_conn":        int64(3),
		"listen_queue":         int64(1),
		"max_listen_queue":     int64(0),
		"listen_queue_len":     int64(0),
		"idle_processes":       int64(1),
		"active_processes":     int64(1),
		"total_processes":      int64(2),
		"max_active_processes": int64(1),
		"max_children_reached": int64(2),
		"slow_requests":        int64(1),
	}

	acc.AssertContainsTaggedFields(t, "phpfpm", fields, tags)
}

func TestPhpFpmGeneratesJSONMetrics_From_Http(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(outputSampleJSON)))
		if _, err := fmt.Fprint(w, string(outputSampleJSON)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer server.Close()

	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	expected, err := testutil.ParseMetricsFromFile("testdata/expected.out", parser)
	require.NoError(t, err)

	input := &Phpfpm{
		Urls:   []string{server.URL + "?full&json"},
		Format: "json",
		Log:    &testutil.Logger{},
	}
	require.NoError(t, input.Init())

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(input.Gather))
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime(), testutil.IgnoreTags("url"))
}

func TestPhpFpmGeneratesMetrics_From_Fcgi(t *testing.T) {
	// Let OS find an available port
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "Cannot initialize test server")
	defer tcp.Close()

	s := statServer{}
	go fcgi.Serve(tcp, s) //nolint:errcheck // ignore the returned error as we cannot do anything about it anyway

	// Now we tested again above server
	r := &Phpfpm{
		Urls: []string{"fcgi://" + tcp.Addr().String() + "/status"},
		Log:  &testutil.Logger{},
	}
	require.NoError(t, r.Init())

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(r.Gather))

	tags := map[string]string{
		"pool": "www",
		"url":  r.Urls[0],
	}

	fields := map[string]interface{}{
		"start_since":          int64(1991),
		"accepted_conn":        int64(3),
		"listen_queue":         int64(1),
		"max_listen_queue":     int64(0),
		"listen_queue_len":     int64(0),
		"idle_processes":       int64(1),
		"active_processes":     int64(1),
		"total_processes":      int64(2),
		"max_active_processes": int64(1),
		"max_children_reached": int64(2),
		"slow_requests":        int64(1),
	}

	acc.AssertContainsTaggedFields(t, "phpfpm", fields, tags)
}

func TestPhpFpmTimeout_From_Fcgi(t *testing.T) {
	// Let OS find an available port
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "Cannot initialize test server")
	defer tcp.Close()

	const timeout = 200 * time.Millisecond

	go func() {
		conn, err := tcp.Accept()
		if err != nil {
			return // ignore the returned error as we cannot do anything about it anyway
		}
		defer conn.Close()

		// Sleep longer than the timeout
		time.Sleep(2 * timeout)
	}()

	// Now we tested again above server
	r := &Phpfpm{
		Urls:    []string{"fcgi://" + tcp.Addr().String() + "/status"},
		Timeout: config.Duration(timeout),
		Log:     &testutil.Logger{},
	}
	require.NoError(t, r.Init())

	start := time.Now()

	var acc testutil.Accumulator
	require.Error(t, acc.GatherError(r.Gather))

	require.Empty(t, acc.GetTelegrafMetrics())
	require.GreaterOrEqual(t, time.Since(start), timeout)
}

// TestPhpFpmCrashWithTimeout_From_Fcgi show issue #15175: when timeout is enabled
// and nothing is listening on specified port, a nil pointer was dereferenced.
func TestPhpFpmCrashWithTimeout_From_Fcgi(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "Cannot initialize test server")

	tcpAddress := tcp.Addr().String()

	// Yes close the tcp port now. The listenner is only used to find a free
	// port and then make it free. This test hope that nothing will re-use the
	// port in meantime.
	tcp.Close()

	const timeout = 200 * time.Millisecond

	// Now we tested again above server
	r := &Phpfpm{
		Urls:    []string{"fcgi://" + tcpAddress + "/status"},
		Timeout: config.Duration(timeout),
		Log:     &testutil.Logger{},
	}
	require.NoError(t, r.Init())

	var acc testutil.Accumulator
	require.Error(t, acc.GatherError(r.Gather))

	require.Empty(t, acc.GetTelegrafMetrics())
}

func TestPhpFpmGeneratesMetrics_From_Socket(t *testing.T) {
	// Create a socket in /tmp because we always have write permission and if the
	// removing of socket fail when system restart /tmp is clear so
	// we don't have junk files around
	var randomNumber int64
	require.NoError(t, binary.Read(rand.Reader, binary.LittleEndian, &randomNumber))
	tcp, err := net.Listen("unix", fmt.Sprintf("/tmp/test-fpm%d.sock", randomNumber))
	require.NoError(t, err, "Cannot initialize server on port ")

	defer tcp.Close()
	s := statServer{}
	go fcgi.Serve(tcp, s) //nolint:errcheck // ignore the returned error as we cannot do anything about it anyway

	r := &Phpfpm{
		Urls: []string{tcp.Addr().String()},
		Log:  &testutil.Logger{},
	}
	require.NoError(t, r.Init())

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(r.Gather))

	tags := map[string]string{
		"pool": "www",
		"url":  r.Urls[0],
	}

	fields := map[string]interface{}{
		"start_since":          int64(1991),
		"accepted_conn":        int64(3),
		"listen_queue":         int64(1),
		"max_listen_queue":     int64(0),
		"listen_queue_len":     int64(0),
		"idle_processes":       int64(1),
		"active_processes":     int64(1),
		"total_processes":      int64(2),
		"max_active_processes": int64(1),
		"max_children_reached": int64(2),
		"slow_requests":        int64(1),
	}

	acc.AssertContainsTaggedFields(t, "phpfpm", fields, tags)
}

func TestPhpFpmGeneratesMetrics_From_Multiple_Sockets_With_Glob(t *testing.T) {
	// Create a socket in /tmp because we always have write permission and if the
	// removing of socket fail when system restart /tmp is clear so
	// we don't have junk files around
	var randomNumber int64
	require.NoError(t, binary.Read(rand.Reader, binary.LittleEndian, &randomNumber))
	socket1 := fmt.Sprintf("/tmp/test-fpm%d.sock", randomNumber)
	tcp1, err := net.Listen("unix", socket1)
	require.NoError(t, err, "Cannot initialize server on port ")
	defer tcp1.Close()

	require.NoError(t, binary.Read(rand.Reader, binary.LittleEndian, &randomNumber))
	socket2 := fmt.Sprintf("/tmp/test-fpm%d.sock", randomNumber)
	tcp2, err := net.Listen("unix", socket2)
	require.NoError(t, err, "Cannot initialize server on port ")
	defer tcp2.Close()

	s := statServer{}
	go fcgi.Serve(tcp1, s) //nolint:errcheck // ignore the returned error as we cannot do anything about it anyway
	go fcgi.Serve(tcp2, s) //nolint:errcheck // ignore the returned error as we cannot do anything about it anyway

	r := &Phpfpm{
		Urls: []string{"/tmp/test-fpm[\\-0-9]*.sock"},
		Log:  &testutil.Logger{},
	}
	require.NoError(t, r.Init())

	var acc1, acc2 testutil.Accumulator
	require.NoError(t, acc1.GatherError(r.Gather))
	require.NoError(t, acc2.GatherError(r.Gather))

	tags1 := map[string]string{
		"pool": "www",
		"url":  socket1,
	}

	tags2 := map[string]string{
		"pool": "www",
		"url":  socket2,
	}

	fields := map[string]interface{}{
		"start_since":          int64(1991),
		"accepted_conn":        int64(3),
		"listen_queue":         int64(1),
		"max_listen_queue":     int64(0),
		"listen_queue_len":     int64(0),
		"idle_processes":       int64(1),
		"active_processes":     int64(1),
		"total_processes":      int64(2),
		"max_active_processes": int64(1),
		"max_children_reached": int64(2),
		"slow_requests":        int64(1),
	}

	acc1.AssertContainsTaggedFields(t, "phpfpm", fields, tags1)
	acc2.AssertContainsTaggedFields(t, "phpfpm", fields, tags2)
}

func TestPhpFpmGeneratesMetrics_From_Socket_Custom_Status_Path(t *testing.T) {
	// Create a socket in /tmp because we always have write permission. If the
	// removing of socket fail we won't have junk files around. Cuz when system
	// restart, it clears out /tmp
	var randomNumber int64
	require.NoError(t, binary.Read(rand.Reader, binary.LittleEndian, &randomNumber))
	tcp, err := net.Listen("unix", fmt.Sprintf("/tmp/test-fpm%d.sock", randomNumber))
	require.NoError(t, err, "Cannot initialize server on port ")

	defer tcp.Close()
	s := statServer{}
	go fcgi.Serve(tcp, s) //nolint:errcheck // ignore the returned error as we cannot do anything about it anyway

	r := &Phpfpm{
		Urls: []string{tcp.Addr().String() + ":custom-status-path"},
		Log:  &testutil.Logger{},
	}
	require.NoError(t, r.Init())

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(r.Gather))

	tags := map[string]string{
		"pool": "www",
		"url":  r.Urls[0],
	}

	fields := map[string]interface{}{
		"start_since":          int64(1991),
		"accepted_conn":        int64(3),
		"listen_queue":         int64(1),
		"max_listen_queue":     int64(0),
		"listen_queue_len":     int64(0),
		"idle_processes":       int64(1),
		"active_processes":     int64(1),
		"total_processes":      int64(2),
		"max_active_processes": int64(1),
		"max_children_reached": int64(2),
		"slow_requests":        int64(1),
	}

	acc.AssertContainsTaggedFields(t, "phpfpm", fields, tags)
}

// When not passing server config, we default to localhost
// We just want to make sure we did request stat from localhost
func TestPhpFpmDefaultGetFromLocalhost(t *testing.T) {
	r := &Phpfpm{
		Urls: []string{"http://bad.localhost:62001/status"},
		Log:  &testutil.Logger{},
	}
	require.NoError(t, r.Init())

	var acc testutil.Accumulator
	require.ErrorContains(t, acc.GatherError(r.Gather), "/status")
}

func TestPhpFpmGeneratesMetrics_Throw_Error_When_Fpm_Status_Is_Not_Responding(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long test in short mode")
	}

	r := &Phpfpm{
		Urls: []string{"http://aninvalidone"},
		Log:  &testutil.Logger{},
	}
	require.NoError(t, r.Init())

	var acc testutil.Accumulator
	err := acc.GatherError(r.Gather)
	require.ErrorContains(t, err, `unable to connect to phpfpm status page "http://aninvalidone"`)
	require.ErrorContains(t, err, `lookup aninvalidone`)
}

func TestPhpFpmGeneratesMetrics_Throw_Error_When_Socket_Path_Is_Invalid(t *testing.T) {
	r := &Phpfpm{
		Urls: []string{"/tmp/invalid.sock"},
		Log:  &testutil.Logger{},
	}
	require.NoError(t, r.Init())

	var acc testutil.Accumulator
	require.ErrorContains(t, acc.GatherError(r.Gather), `socket doesn't exist "/tmp/invalid.sock"`)
}

const outputSample = `
pool:                 www
process manager:      dynamic
start time:           11/Oct/2015:23:38:51 +0000
start since:          1991
accepted conn:        3
listen queue:         1
max listen queue:     0
listen queue len:     0
idle processes:       1
active processes:     1
total processes:      2
max active processes: 1
max children reached: 2
slow requests:        1
`

//go:embed testdata/phpfpm.json
var outputSampleJSON []byte

func TestPhpFpmParseJSON_Log_Error_Without_Panic_When_When_JSON_Is_Invalid(t *testing.T) {
	// Capture the logging output for checking
	logger := &testutil.CaptureLogger{Name: "inputs.phpfpm"}
	plugin := &Phpfpm{Log: logger}
	require.NoError(t, plugin.Init())

	// parse valid JSON without panic and without log output
	validJSON := outputSampleJSON
	require.NotPanics(t, func() { plugin.parseJSON(bytes.NewReader(validJSON), &testutil.NopAccumulator{}, "") })
	require.Empty(t, logger.NMessages())

	// parse invalid JSON without panic but with log output
	invalidJSON := []byte("X")
	require.NotPanics(t, func() { plugin.parseJSON(bytes.NewReader(invalidJSON), &testutil.NopAccumulator{}, "") })
	require.Contains(t, logger.Errors(), "E! [inputs.phpfpm] Unable to decode JSON response: invalid character 'X' looking for beginning of value")
}

func TestGatherDespiteUnavailable(t *testing.T) {
	// Let OS find an available port
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "Cannot initialize test server")
	defer tcp.Close()

	s := statServer{}
	go fcgi.Serve(tcp, s) //nolint:errcheck // ignore the returned error as we cannot do anything about it anyway

	// Now we tested again above server
	r := &Phpfpm{
		Urls: []string{"fcgi://" + tcp.Addr().String() + "/status", "/lala"},
		Log:  &testutil.Logger{},
	}
	require.NoError(t, r.Init())

	expected := []telegraf.Metric{
		metric.New(
			"phpfpm",
			map[string]string{
				"pool": "www",
				"url":  r.Urls[0],
			},
			map[string]interface{}{
				"start_since":          int64(1991),
				"accepted_conn":        int64(3),
				"listen_queue":         int64(1),
				"max_listen_queue":     int64(0),
				"listen_queue_len":     int64(0),
				"idle_processes":       int64(1),
				"active_processes":     int64(1),
				"total_processes":      int64(2),
				"max_active_processes": int64(1),
				"max_children_reached": int64(2),
				"slow_requests":        int64(1),
			},
			time.Unix(0, 0),
		),
	}

	var acc testutil.Accumulator
	require.ErrorContains(t, acc.GatherError(r.Gather), "socket doesn't exist")
	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}
