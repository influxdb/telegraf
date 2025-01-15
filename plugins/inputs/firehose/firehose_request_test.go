package firehose

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestInvalidRequests(t *testing.T) {
	tests := []struct {
		name         string
		headers      map[string]string
		body         string
		method       string
		expectedMsg  string
		expectedCode int
	}{
		{
			name:         "missing request id",
			headers:      map[string]string{"x-amz-firehose-request-id": ""},
			body:         `{"requestId":"test-id","timestamp":1578090901599,"records":[{"data":"dGVzdA=="}]}`,
			expectedMsg:  "x-amz-firehose-request-id header is not set",
			expectedCode: 400,
		},
		{
			name:         "request id mismatch",
			headers:      map[string]string{"x-amz-firehose-request-id": "test-id"},
			body:         `{"requestId":"some-other-id","timestamp":1578090901599,"records":[{"data":"dGVzdA=="}]}`,
			expectedMsg:  "mismatch between request ID",
			expectedCode: 400,
		},
		{
			name:         "invalid body",
			headers:      map[string]string{"x-amz-firehose-request-id": "test-id"},
			body:         "not a json",
			expectedMsg:  `decode body for request "test-id" failed`,
			expectedCode: 400,
		},
		{
			name:         "invalid data encoding",
			headers:      map[string]string{"x-amz-firehose-request-id": "test-id"},
			body:         `{"requestId":"test-id","timestamp":1578090901599,"records":[{"data":"not a base64 encoded text"}]}`,
			expectedMsg:  `ecode base64 data from request "test-id" failed: illegal base64 data`,
			expectedCode: 400,
		},
		{
			name:         "content too large",
			headers:      map[string]string{"x-amz-firehose-request-id": "test-id"},
			body:         strings.Repeat("x", 65*1024*1024),
			expectedMsg:  `content length is too large`,
			expectedCode: 413,
		},
		{
			name: "invalid content type",
			headers: map[string]string{
				"x-amz-firehose-request-id": "test-id",
				"content-type":              "application/text",
			},
			body:         `{"requestId":"test-id","timestamp":1578090901599,"records":[{"data":"dGVzdA=="}]}`,
			expectedMsg:  `content type "application/text" is not allowed`,
			expectedCode: 415,
		},
		{
			name:         "invalid method",
			headers:      map[string]string{"x-amz-firehose-request-id": "test-id"},
			body:         `{"requestId":"test-id","timestamp":1578090901599,"records":[{"data":"dGVzdA=="}]}`,
			method:       "GET",
			expectedMsg:  `method "GET" is not allowed`,
			expectedCode: 405,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup plugin and start it
			plugin := &Firehose{
				ServiceAddress: "127.0.0.1:0",
				Log:            &testutil.Logger{},
			}
			require.NoError(t, plugin.Init())

			var acc testutil.Accumulator
			require.NoError(t, plugin.Start(&acc))
			defer plugin.Stop()

			// Get the listening address
			addr := plugin.listener.Addr().String()

			// Create a request with the data defined in the test case
			method := "POST"
			if tt.method != "" {
				method = tt.method
			}
			req, err := http.NewRequest(method, "http://"+addr+"/telegraf", bytes.NewBufferString(tt.body))
			require.NoError(t, err)
			req.Header.Set("content-type", "application/json")
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			// Execute the request
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Check the result
			require.ErrorContains(t, acc.FirstError(), tt.expectedMsg)
			require.Equal(t, tt.expectedCode, resp.StatusCode)
		})
	}
}

func TestAuthentication(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		headers      map[string]string
		key          string
		expectedMsg  string
		expectedCode int
	}{
		{
			name: "no auth required",
			headers: map[string]string{
				"x-amz-firehose-request-id": "test-id",
			},
			body: `
			{
			  "requestId": "test-id",
			  "timestamp":1734625715000000000,
			  "records":[{"data":"dGVzdCB2YWx1ZT00MmkgMTczNDYyNTcxNTAwMDAwMDAwMAo="}]
			}`,
			expectedCode: 200,
		},
		{
			name: "no auth required but key sent",
			headers: map[string]string{
				"x-amz-firehose-request-id": "test-id",
				"x-amz-firehose-access-key": "test-key",
			},
			body: `
			{
			  "requestId": "test-id",
			  "timestamp":1734625715000000000,
			  "records":[{"data":"dGVzdCB2YWx1ZT00MmkgMTczNDYyNTcxNTAwMDAwMDAwMAo="}]
			}`,
			expectedCode: 200,
		},
		{
			name: "auth required success",
			headers: map[string]string{
				"x-amz-firehose-request-id": "test-id",
				"x-amz-firehose-access-key": "test-key",
			},
			body: `
			{
			  "requestId": "test-id",
			  "timestamp":1734625715000000000,
			  "records":[{"data":"dGVzdCB2YWx1ZT00MmkgMTczNDYyNTcxNTAwMDAwMDAwMAo="}]
			}`,
			key:          "test-key",
			expectedCode: 200,
		},
		{
			name: "auth required wrong key",
			headers: map[string]string{
				"x-amz-firehose-request-id": "test-id",
				"x-amz-firehose-access-key": "foo bar",
			},
			body: `
			{
			  "requestId": "test-id",
			  "timestamp":1734625715000000000,
			  "records":[{"data":"dGVzdCB2YWx1ZT00MmkgMTczNDYyNTcxNTAwMDAwMDAwMAo="}]
			}`,
			key:          "test-key",
			expectedMsg:  "unauthorized request",
			expectedCode: 401,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup plugin
			plugin := &Firehose{
				ServiceAddress: "127.0.0.1:0",
				AccessKey:      config.NewSecret([]byte(tt.key)),
				Log:            &testutil.Logger{},
			}

			// Setup a parser
			parser := &influx.Parser{}
			require.NoError(t, parser.Init())
			plugin.SetParser(parser)

			// Start the plugin
			require.NoError(t, plugin.Init())

			var acc testutil.Accumulator
			require.NoError(t, plugin.Start(&acc))
			defer plugin.Stop()

			// Get the listening address
			addr := plugin.listener.Addr().String()

			// Create a request with the data defined in the test case
			req, err := http.NewRequest("POST", "http://"+addr+"/telegraf", bytes.NewBufferString(tt.body))
			require.NoError(t, err)
			req.Header.Set("content-type", "application/json")
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			// Execute the request
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Check the result
			if tt.expectedMsg == "" {
				require.NoError(t, acc.FirstError())
			} else {
				require.ErrorContains(t, acc.FirstError(), tt.expectedMsg)
			}
			require.Equal(t, tt.expectedCode, resp.StatusCode)
		})
	}
}

func TestCases(t *testing.T) {
	// Get all directories in testdata
	folders, err := os.ReadDir("testcases")
	require.NoError(t, err)

	// Register the plugin
	inputs.Add("firehose", func() telegraf.Input {
		return &Firehose{
			ReadTimeout:  config.Duration(time.Second * 5),
			WriteTimeout: config.Duration(time.Second * 5),
		}
	})

	// Prepare the influx parser for expectations
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())

	for _, f := range folders {
		// Only handle folders
		if !f.IsDir() {
			continue
		}
		testcasePath := filepath.Join("testcases", f.Name())
		configFilename := filepath.Join(testcasePath, "telegraf.conf")
		expectedFilename := filepath.Join(testcasePath, "expected.out")
		expectedErrorFilename := filepath.Join(testcasePath, "expected.err")

		t.Run(f.Name(), func(t *testing.T) {
			// Read the input data
			headers, bodies, err := readInputData(testcasePath)
			require.NoError(t, err)

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

			// Configure and initialize the plugin
			cfg := config.NewConfig()
			require.NoError(t, cfg.LoadConfig(configFilename))
			require.Len(t, cfg.Inputs, 1)

			plugin := cfg.Inputs[0].Input.(*Firehose)
			plugin.ServiceAddress = "127.0.0.1:0"
			require.NoError(t, plugin.Init())

			// Start the plugin
			var acc testutil.Accumulator
			require.NoError(t, plugin.Start(&acc))
			defer plugin.Stop()

			// Get the listening address
			addr := plugin.listener.Addr().String()

			// Set all message bodies
			endpoint := "http://" + addr + plugin.Paths[0]
			for i, body := range bodies {
				req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
				require.NoErrorf(t, err, "creating request for body %d", i)
				req.Header.Set("content-type", "application/json")
				for k, v := range headers {
					req.Header.Set(k, v)
				}

				// Execute the request
				resp, err := http.DefaultClient.Do(req)
				require.NoErrorf(t, err, "executing request for body %d", i)
				resp.Body.Close()

				if len(expectedErrors) == 0 {
					require.Equalf(t, 200, resp.StatusCode, "result for body %d: %v", i, acc.Errors)
				} else {
					require.NotEqualf(t, 200, resp.StatusCode, "result for body %d: %v", i, acc.Errors)
				}
			}

			// Check the result
			var actualErrorMsgs []string
			if len(acc.Errors) > 0 {
				for _, err := range acc.Errors {
					actualErrorMsgs = append(actualErrorMsgs, err.Error())
				}
			}
			require.ElementsMatch(t, actualErrorMsgs, expectedErrors)

			// Check the metric nevertheless as we might get some metrics despite errors.
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, expected, actual, testutil.SortMetrics())
		})
	}
}

func readInputData(path string) (map[string]string, [][]byte, error) {
	// Reading the headers file
	var headers map[string]string
	headersBuf, err := os.ReadFile(filepath.Join(path, "headers.json"))
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(headersBuf, &headers); err != nil {
		return nil, nil, err
	}

	// Read all bodies
	bodyFiles, err := filepath.Glob(filepath.Join(path, "body*.json"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(bodyFiles)
	bodies := make([][]byte, 0, len(bodyFiles))
	for _, fn := range bodyFiles {
		buf, err := os.ReadFile(fn)
		if err != nil {
			return nil, nil, err
		}
		bodies = append(bodies, buf)
	}

	return headers, bodies, nil
}
