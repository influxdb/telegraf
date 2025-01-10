package haproxy

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func serverSocket(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}

		go func(c net.Conn) {
			defer c.Close()

			buf := make([]byte, 1024)
			n, err := c.Read(buf)
			if err != nil {
				return
			}

			data := buf[:n]
			if string(data) == "show stat\n" {
				c.Write(csvOutputSample) //nolint:errcheck // we return anyway
			}
		}(conn)
	}
}

func TestHaproxyGeneratesMetricsWithAuthentication(t *testing.T) {
	// We create a fake server to return test data
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			if _, err := fmt.Fprint(w, "Unauthorized"); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
			return
		}

		if username == "user" && password == "password" {
			if _, err := fmt.Fprint(w, string(csvOutputSample)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
			if _, err := fmt.Fprint(w, "Unauthorized"); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		}
	}))
	defer ts.Close()

	// Now we tested again above server, with our authentication data
	r := &HAProxy{
		Servers: []string{strings.Replace(ts.URL, "http://", "http://user:password@", 1)},
	}

	var acc testutil.Accumulator

	err := r.Gather(&acc)
	require.NoError(t, err)

	tags := map[string]string{
		"server": ts.Listener.Addr().String(),
		"proxy":  "git",
		"sv":     "www",
		"type":   "server",
	}

	fields := haproxyGetFieldValues()
	acc.AssertContainsTaggedFields(t, "haproxy", fields, tags)

	// Here, we should get error because we don't pass authentication data
	r = &HAProxy{
		Servers: []string{ts.URL},
	}

	require.NoError(t, r.Gather(&acc))
	require.NotEmpty(t, acc.Errors)
}

func TestHaproxyGeneratesMetricsWithoutAuthentication(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprint(w, string(csvOutputSample)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer ts.Close()

	r := &HAProxy{
		Servers: []string{ts.URL},
	}

	var acc testutil.Accumulator

	require.NoError(t, r.Gather(&acc))

	tags := map[string]string{
		"server": ts.Listener.Addr().String(),
		"proxy":  "git",
		"sv":     "www",
		"type":   "server",
	}

	fields := haproxyGetFieldValues()
	acc.AssertContainsTaggedFields(t, "haproxy", fields, tags)
}

func TestHaproxyGeneratesMetricsUsingSocket(t *testing.T) {
	var randomNumber int64
	var sockets [5]net.Listener

	_globmask := filepath.Join(os.TempDir(), "test-haproxy*.sock")
	_badmask := filepath.Join(os.TempDir(), "test-fail-haproxy*.sock")

	for i := 0; i < 5; i++ {
		require.NoError(t, binary.Read(rand.Reader, binary.LittleEndian, &randomNumber))
		sockname := filepath.Join(os.TempDir(), fmt.Sprintf("test-haproxy%d.sock", randomNumber))

		sock, err := net.Listen("unix", sockname)
		if err != nil {
			t.Fatal("Cannot initialize socket ")
		}

		sockets[i] = sock
		defer sock.Close() //nolint:revive,gocritic // done on purpose, closing will be executed properly

		go serverSocket(sock)
	}

	r := &HAProxy{
		Servers: []string{_globmask},
	}

	var acc testutil.Accumulator

	err := r.Gather(&acc)
	require.NoError(t, err)

	fields := haproxyGetFieldValues()

	for _, sock := range sockets {
		tags := map[string]string{
			"server": getSocketAddr(sock.Addr().String()),
			"proxy":  "git",
			"sv":     "www",
			"type":   "server",
		}

		acc.AssertContainsTaggedFields(t, "haproxy", fields, tags)
	}

	// This mask should not match any socket
	r.Servers = []string{_badmask}

	require.NoError(t, r.Gather(&acc))
	require.NotEmpty(t, acc.Errors)
}

func TestHaproxyGeneratesMetricsUsingTcp(t *testing.T) {
	l, err := net.Listen("tcp", "localhost:8192")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go serverSocket(l)

	r := &HAProxy{
		Servers: []string{"tcp://" + l.Addr().String()},
	}

	var acc testutil.Accumulator
	require.NoError(t, r.Gather(&acc))

	fields := haproxyGetFieldValues()

	tags := map[string]string{
		"server": l.Addr().String(),
		"proxy":  "git",
		"sv":     "www",
		"type":   "server",
	}

	acc.AssertContainsTaggedFields(t, "haproxy", fields, tags)

	require.NoError(t, r.Gather(&acc))
}

// When not passing server config, we default to localhost
// We just want to make sure we did request stat from localhost
func TestHaproxyDefaultGetFromLocalhost(t *testing.T) {
	r := &HAProxy{}

	var acc testutil.Accumulator

	err := r.Gather(&acc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "127.0.0.1:1936/haproxy?stats/;csv")
}

func TestHaproxyKeepFieldNames(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprint(w, string(csvOutputSample)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer ts.Close()

	r := &HAProxy{
		Servers:        []string{ts.URL},
		KeepFieldNames: true,
	}

	var acc testutil.Accumulator

	require.NoError(t, r.Gather(&acc))

	tags := map[string]string{
		"server": ts.Listener.Addr().String(),
		"pxname": "git",
		"svname": "www",
		"type":   "server",
	}

	fields := haproxyGetFieldValues()
	fields["act"] = fields["active_servers"]
	delete(fields, "active_servers")
	fields["bck"] = fields["backup_servers"]
	delete(fields, "backup_servers")
	fields["cli_abrt"] = fields["cli_abort"]
	delete(fields, "cli_abort")
	fields["srv_abrt"] = fields["srv_abort"]
	delete(fields, "srv_abort")
	fields["hrsp_1xx"] = fields["http_response.1xx"]
	delete(fields, "http_response.1xx")
	fields["hrsp_2xx"] = fields["http_response.2xx"]
	delete(fields, "http_response.2xx")
	fields["hrsp_3xx"] = fields["http_response.3xx"]
	delete(fields, "http_response.3xx")
	fields["hrsp_4xx"] = fields["http_response.4xx"]
	delete(fields, "http_response.4xx")
	fields["hrsp_5xx"] = fields["http_response.5xx"]
	delete(fields, "http_response.5xx")
	fields["hrsp_other"] = fields["http_response.other"]
	delete(fields, "http_response.other")

	acc.AssertContainsTaggedFields(t, "haproxy", fields, tags)
}

func mustReadSampleOutput() []byte {
	filePath := "testdata/sample_output.csv"
	data, err := os.ReadFile(filePath)
	if err != nil {
		panic(fmt.Errorf("could not read from file %s: %w", filePath, err))
	}

	return data
}

func haproxyGetFieldValues() map[string]interface{} {
	fields := map[string]interface{}{
		"active_servers":      uint64(1),
		"backup_servers":      uint64(0),
		"bin":                 uint64(5228218),
		"bout":                uint64(303747244),
		"check_code":          uint64(200),
		"check_duration":      uint64(3),
		"check_fall":          uint64(3),
		"check_health":        uint64(4),
		"check_rise":          uint64(2),
		"check_status":        "L7OK",
		"chkdown":             uint64(84),
		"chkfail":             uint64(559),
		"cli_abort":           uint64(690),
		"ctime":               uint64(1),
		"downtime":            uint64(3352),
		"dresp":               uint64(0),
		"econ":                uint64(0),
		"eresp":               uint64(21),
		"http_response.1xx":   uint64(0),
		"http_response.2xx":   uint64(5668),
		"http_response.3xx":   uint64(8710),
		"http_response.4xx":   uint64(140),
		"http_response.5xx":   uint64(0),
		"http_response.other": uint64(0),
		"iid":                 uint64(4),
		"last_chk":            "OK",
		"lastchg":             uint64(1036557),
		"lastsess":            int64(1342),
		"lbtot":               uint64(9481),
		"mode":                "http",
		"pid":                 uint64(1),
		"qcur":                uint64(0),
		"qmax":                uint64(0),
		"qtime":               uint64(1268),
		"rate":                uint64(0),
		"rate_max":            uint64(2),
		"rtime":               uint64(2908),
		"sid":                 uint64(1),
		"scur":                uint64(0),
		"slim":                uint64(2),
		"smax":                uint64(2),
		"srv_abort":           uint64(0),
		"status":              "UP",
		"stot":                uint64(14539),
		"ttime":               uint64(4500),
		"weight":              uint64(1),
		"wredis":              uint64(0),
		"wretr":               uint64(0),
	}
	return fields
}

// Can obtain from official haproxy demo: 'http://demo.haproxy.org/;csv'
var csvOutputSample = mustReadSampleOutput()
