package socket_listener

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/common/socket"
	"github.com/influxdata/telegraf/plugins/inputs"
	_ "github.com/influxdata/telegraf/plugins/parsers/all"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/plugins/parsers/value"
	"github.com/influxdata/telegraf/testutil"
)

var pki = testutil.NewPKI("../../../testutil/pki")

func TestSocketListener(t *testing.T) {
	messages := [][]byte{
		[]byte("test,foo=bar v=1i 123456789\ntest,foo=baz v=2i 123456790\n"),
		[]byte("test,foo=zab v=3i 123456791\n"),
	}
	expected := []telegraf.Metric{
		metric.New(
			"test",
			map[string]string{"foo": "bar"},
			map[string]interface{}{"v": int64(1)},
			time.Unix(0, 123456789),
		),
		metric.New(
			"test",
			map[string]string{"foo": "baz"},
			map[string]interface{}{"v": int64(2)},
			time.Unix(0, 123456790),
		),
		metric.New(
			"test",
			map[string]string{"foo": "zab"},
			map[string]interface{}{"v": int64(3)},
			time.Unix(0, 123456791),
		),
	}

	tests := []struct {
		name       string
		schema     string
		buffersize config.Size
		encoding   string
	}{
		{
			name:       "TCP",
			schema:     "tcp",
			buffersize: config.Size(1024),
		},
		{
			name:   "TCP with TLS",
			schema: "tcp+tls",
		},
		{
			name:       "TCP with gzip encoding",
			schema:     "tcp",
			buffersize: config.Size(1024),
			encoding:   "gzip",
		},
		{
			name:       "UDP",
			schema:     "udp",
			buffersize: config.Size(1024),
		},
		{
			name:       "UDP with gzip encoding",
			schema:     "udp",
			buffersize: config.Size(1024),
			encoding:   "gzip",
		},
		{
			name:       "unix socket",
			schema:     "unix",
			buffersize: config.Size(1024),
		},
		{
			name:   "unix socket with TLS",
			schema: "unix+tls",
		},
		{
			name:     "unix socket with gzip encoding",
			schema:   "unix",
			encoding: "gzip",
		},
		{
			name:       "unixgram socket",
			schema:     "unixgram",
			buffersize: config.Size(1024),
		},
	}

	serverTLS := pki.TLSServerConfig()
	clientTLS := pki.TLSClientConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proto := strings.TrimSuffix(tt.schema, "+tls")

			// Prepare the address and socket if needed
			var serverAddr string
			var tlsCfg *tls.Config
			switch proto {
			case "tcp", "udp":
				serverAddr = "127.0.0.1:0"
			case "unix", "unixgram":
				if runtime.GOOS == "windows" {
					t.Skip("Skipping on Windows, as unixgram sockets are not supported")
				}

				// Create a socket
				sock, err := os.CreateTemp("", "sock-")
				require.NoError(t, err)
				defer sock.Close()
				defer os.Remove(sock.Name())
				serverAddr = sock.Name()
			}

			// Setup plugin according to test specification
			plugin := &SocketListener{
				ServiceAddress: proto + "://" + serverAddr,
				Config: socket.Config{
					ContentEncoding: tt.encoding,
					ReadBufferSize:  tt.buffersize,
				},
				Log: &testutil.Logger{},
			}
			if strings.HasSuffix(tt.schema, "tls") {
				plugin.ServerConfig = *serverTLS
				var err error
				tlsCfg, err = clientTLS.TLSConfig()
				require.NoError(t, err)
			}
			parser := &influx.Parser{}
			require.NoError(t, parser.Init())
			plugin.SetParser(parser)

			// Start the plugin
			var acc testutil.Accumulator
			require.NoError(t, plugin.Init())
			require.NoError(t, plugin.Start(&acc))
			defer plugin.Stop()

			addr := plugin.socket.Address()

			// Create a noop client
			// Server is async, so verify no errors at the end.
			client, err := createClient(plugin.ServiceAddress, addr, tlsCfg)
			require.NoError(t, err)
			require.NoError(t, client.Close())

			// Setup the client for submitting data
			client, err = createClient(plugin.ServiceAddress, addr, tlsCfg)
			require.NoError(t, err)

			// Send the data with the correct encoding
			encoder, err := internal.NewContentEncoder(tt.encoding)
			require.NoError(t, err)

			for i, msg := range messages {
				m, err := encoder.Encode(msg)
				require.NoErrorf(t, err, "encoding failed for msg %d", i)
				_, err = client.Write(m)
				require.NoErrorf(t, err, "sending msg %d failed", i)
			}

			// Test the resulting metrics and compare against expected results
			require.Eventuallyf(t, func() bool {
				acc.Lock()
				defer acc.Unlock()
				return acc.NMetrics() >= uint64(len(expected))
			}, time.Second, 100*time.Millisecond, "did not receive metrics (%d)", acc.NMetrics())
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, expected, actual, testutil.SortMetrics())
		})
	}
}

func TestLargeReadBufferTCP(t *testing.T) {
	// Construct a buffer-size setting of 1000KiB
	var bufsize config.Size
	require.NoError(t, bufsize.UnmarshalText([]byte("1000KiB")))

	// Setup plugin with a sufficient read buffer
	plugin := &SocketListener{
		ServiceAddress: "tcp://127.0.0.1:0",
		Config: socket.Config{
			ReadBufferSize: bufsize,
		},
		SplitConfig: socket.SplitConfig{
			SplittingStrategy: "newline",
		},
		Log: &testutil.Logger{},
	}
	parser := &value.Parser{
		MetricName: "test",
		DataType:   "string",
	}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	// Create a large message with the readbuffer size
	message := bytes.Repeat([]byte{'a'}, int(bufsize)-2)
	expected := []telegraf.Metric{
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": string(message)},
			time.Unix(0, 0),
		),
	}

	// Start the plugin
	var acc testutil.Accumulator
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	addr := plugin.socket.Address()

	// Setup the client for submitting data
	client, err := createClient(plugin.ServiceAddress, addr, nil)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.Write(append(message, '\n'))
	require.NoError(t, err)
	client.Close()

	getError := func() error {
		acc.Lock()
		defer acc.Unlock()
		return acc.FirstError()
	}

	// Test the resulting metrics and compare against expected results
	require.Eventuallyf(t, func() bool {
		return acc.NMetrics() >= uint64(len(expected))
	}, time.Second, 100*time.Millisecond, "did not receive metrics (%d): %v", acc.NMetrics(), getError())
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
}

func TestLargeReadBufferUnixgram(t *testing.T) {
	// Construct a buffer-size setting of 100KiB
	// Assuming that the testing environment has net.core.wmem_max set to a value greater than 100KiB
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows, as unixgram sockets are not supported")
	}

	if runtime.GOOS == "darwin" {
		t.Skip("Skipping on macOS (darwin), as unixgram write buffer size cannot be changed (default 2048 bytes)")
	}

	var bufsize config.Size
	require.NoError(t, bufsize.UnmarshalText([]byte("100KiB")))

	// Create a socket
	sock, err := os.CreateTemp("", "sock-")
	require.NoError(t, err)
	defer sock.Close()
	defer os.Remove(sock.Name())
	var serverAddr = sock.Name()

	// Setup plugin with a sufficient read buffer
	plugin := &SocketListener{
		ServiceAddress: "unixgram" + "://" + serverAddr,
		Config: socket.Config{
			ReadBufferSize: bufsize,
		},
		Log: &testutil.Logger{},
	}
	parser := &value.Parser{
		MetricName: "test",
		DataType:   "string",
	}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	// Create a large message with the readbuffer size
	message := bytes.Repeat([]byte{'a'}, int(bufsize))
	expected := []telegraf.Metric{
		metric.New(
			"test",
			map[string]string{},
			map[string]interface{}{"value": string(message)},
			time.Unix(0, 0),
		),
	}

	// Start the plugin
	var acc testutil.Accumulator
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Start(&acc))
	defer plugin.Stop()

	addr := plugin.socket.Address()

	// Setup the client for submitting data
	client, err := createClient(plugin.ServiceAddress, addr, nil)
	require.NoError(t, err)
	defer client.Close()

	// Check the socket write buffer size
	unixConn, ok := client.(*net.UnixConn)
	require.True(t, ok, "client is not a *net.UnixConn")
	if err := unixConn.SetWriteBuffer(len(message)); err != nil {
		t.Skipf("Failed to set write buffer size: %v. Skipping test.", err)
	}

	// Write the message
	_, err = client.Write(message)
	require.NoError(t, err)
	client.Close()

	getError := func() error {
		acc.Lock()
		defer acc.Unlock()
		return acc.FirstError()
	}

	// Test the resulting metrics and compare against expected results
	require.Eventuallyf(t, func() bool {
		return acc.NMetrics() >= uint64(len(expected))
	}, time.Second, 100*time.Millisecond, "did not receive metrics (%d): %v", acc.NMetrics(), getError())
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
}

func TestCases(t *testing.T) {
	// Get all directories in testdata
	folders, err := os.ReadDir("testcases")
	require.NoError(t, err)

	// Register the plugin
	inputs.Add("socket_listener", func() telegraf.Input {
		return &SocketListener{}
	})

	for _, f := range folders {
		// Only handle folders
		if !f.IsDir() {
			continue
		}

		// Compare options
		options := []cmp.Option{
			testutil.IgnoreTime(),
			testutil.SortMetrics(),
		}

		t.Run(f.Name(), func(t *testing.T) {
			testcasePath := filepath.Join("testcases", f.Name())
			configFilename := filepath.Join(testcasePath, "telegraf.conf")
			inputFilename := filepath.Join(testcasePath, "sequence.json")
			expectedFilename := filepath.Join(testcasePath, "expected.out")
			expectedErrorFilename := filepath.Join(testcasePath, "expected.err")

			// Prepare the influx parser for expectations
			parser := &influx.Parser{}
			require.NoError(t, parser.Init())

			// Read the input sequence
			sequence, err := readInputData(inputFilename)
			require.NoError(t, err)
			require.NotEmpty(t, sequence)

			// Read the expected output if any
			var expected []telegraf.Metric
			if _, err := os.Stat(expectedFilename); err == nil {
				var err error
				expected, err = testutil.ParseMetricsFromFile(expectedFilename, parser)
				require.NoError(t, err)
			}

			// Read the expected output if any
			var expectedErrors []string
			if _, err := os.Stat(expectedErrorFilename); err == nil {
				var err error
				expectedErrors, err = testutil.ParseLinesFromFile(expectedErrorFilename)
				require.NoError(t, err)
				require.NotEmpty(t, expectedErrors)
			}

			// Configure the plugin
			cfg := config.NewConfig()
			require.NoError(t, cfg.LoadConfig(configFilename))
			require.Len(t, cfg.Inputs, 1)

			// Setup and start the plugin
			var acc testutil.Accumulator
			plugin := cfg.Inputs[0].Input.(*SocketListener)
			require.NoError(t, plugin.Init())
			require.NoError(t, plugin.Start(&acc))
			defer plugin.Stop()

			// Create a client without TLS
			addr := plugin.socket.Address()
			client, err := createClient(plugin.ServiceAddress, addr, nil)
			require.NoError(t, err)

			// Write the given sequence
			for i, step := range sequence {
				if step.Wait > 0 {
					time.Sleep(time.Duration(step.Wait))
					continue
				}
				require.NotEmpty(t, step.raw, "nothing to send")
				_, err := client.Write(step.raw)
				require.NoErrorf(t, err, "writing step %d failed: %v", i, err)
			}
			require.NoError(t, client.Close())

			getNErrors := func() int {
				acc.Lock()
				defer acc.Unlock()
				return len(acc.Errors)
			}
			require.Eventuallyf(t, func() bool {
				return getNErrors() >= len(expectedErrors)
			}, 3*time.Second, 100*time.Millisecond, "did not receive errors (%d/%d)", getNErrors(), len(expectedErrors))

			require.Len(t, acc.Errors, len(expectedErrors))
			sort.SliceStable(acc.Errors, func(i, j int) bool {
				return acc.Errors[i].Error() < acc.Errors[j].Error()
			})
			for i, err := range acc.Errors {
				require.ErrorContains(t, err, expectedErrors[i])
			}

			require.Eventuallyf(t, func() bool {
				acc.Lock()
				defer acc.Unlock()
				return acc.NMetrics() >= uint64(len(expected))
			}, 3*time.Second, 100*time.Millisecond, "did not receive metrics (%d/%d)", acc.NMetrics(), len(expected))

			// Check the metric nevertheless as we might get some metrics despite errors.
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, expected, actual, options...)
		})
	}
}

// element provides a way to configure the
// write sequence for the socket.
type element struct {
	Message string          `json:"message"`
	File    string          `json:"file"`
	Wait    config.Duration `json:"wait"`
	raw     []byte
}

func readInputData(filename string) ([]element, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var sequence []element
	if err := json.Unmarshal(content, &sequence); err != nil {
		return nil, err
	}

	for i, step := range sequence {
		if step.Message != "" && step.File != "" {
			return nil, errors.New("both message and file set in sequence")
		} else if step.Message != "" {
			step.raw = []byte(step.Message)
		} else if step.File != "" {
			path := filepath.Dir(filename)
			path = filepath.Join(path, step.File)
			step.raw, err = os.ReadFile(path)
			if err != nil {
				return nil, err
			}
		}
		sequence[i] = step
	}

	return sequence, nil
}

func createClient(endpoint string, addr net.Addr, tlsCfg *tls.Config) (net.Conn, error) {
	// Determine the protocol in a crude fashion
	parts := strings.SplitN(endpoint, "://", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid endpoint %q", endpoint)
	}
	protocol := parts[0]

	if tlsCfg == nil {
		return net.Dial(protocol, addr.String())
	}

	if protocol == "unix" {
		tlsCfg.InsecureSkipVerify = true
	}
	return tls.Dial(protocol, addr.String(), tlsCfg)
}
