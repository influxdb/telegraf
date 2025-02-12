package http_listener_v2

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/parsers/form_urlencoded"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
)

const (
	testMsg = "cpu_load_short,host=server01 value=12.0 1422568543702900257\n"

	testMsgNoNewline = "cpu_load_short,host=server01 value=12.0 1422568543702900257"

	testMsgs = `cpu_load_short,host=server02 value=12.0 1422568543702900257
cpu_load_short,host=server03 value=12.0 1422568543702900257
cpu_load_short,host=server04 value=12.0 1422568543702900257
cpu_load_short,host=server05 value=12.0 1422568543702900257
cpu_load_short,host=server06 value=12.0 1422568543702900257
`
	badMsg = "blahblahblah: 42\n"

	emptyMsg = ""

	basicUsername = "test-username-please-ignore"
	basicPassword = "super-secure-password!"
)

var (
	pki = testutil.NewPKI("../../../testutil/pki")
)

func newTestHTTPListenerV2() (*HTTPListenerV2, error) {
	parser := &influx.Parser{}
	if err := parser.Init(); err != nil {
		return nil, err
	}

	listener := &HTTPListenerV2{
		Log:            testutil.Logger{},
		ServiceAddress: "localhost:0",
		Path:           "/write",
		Methods:        []string{"POST"},
		Parser:         parser,
		timeFunc:       time.Now,
		MaxBodySize:    config.Size(70000),
		DataSource:     "body",
		close:          make(chan struct{}),
	}
	return listener, nil
}

func newTestHTTPAuthListener() (*HTTPListenerV2, error) {
	listener, err := newTestHTTPListenerV2()
	if err != nil {
		return nil, err
	}
	listener.BasicUsername = basicUsername
	listener.BasicPassword = basicPassword
	return listener, nil
}

func newTestHTTPSListenerV2() (*HTTPListenerV2, error) {
	parser := &influx.Parser{}
	if err := parser.Init(); err != nil {
		return nil, err
	}

	listener := &HTTPListenerV2{
		Log:            testutil.Logger{},
		ServiceAddress: "localhost:0",
		Path:           "/write",
		Methods:        []string{"POST"},
		Parser:         parser,
		ServerConfig:   *pki.TLSServerConfig(),
		timeFunc:       time.Now,
		close:          make(chan struct{}),
	}

	return listener, nil
}

func getHTTPSClient() *http.Client {
	tlsConfig, err := pki.TLSClientConfig().TLSConfig()
	if err != nil {
		panic(err)
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
}

func createURL(listener *HTTPListenerV2, scheme, path, rawquery string) string {
	var port int
	if strings.HasPrefix(listener.ServiceAddress, "tcp://") {
		port = listener.listener.Addr().(*net.TCPAddr).Port
	}
	u := url.URL{
		Scheme:   scheme,
		Host:     "localhost:" + strconv.Itoa(port),
		Path:     path,
		RawQuery: rawquery,
	}
	return u.String()
}

func TestInvalidListenerConfig(t *testing.T) {
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())

	listener := &HTTPListenerV2{
		Log:            testutil.Logger{},
		ServiceAddress: "address_without_port",
		Path:           "/write",
		Methods:        []string{"POST"},
		Parser:         parser,
		timeFunc:       time.Now,
		MaxBodySize:    config.Size(70000),
		DataSource:     "body",
		close:          make(chan struct{}),
	}

	require.NoError(t, listener.Init())
	acc := &testutil.Accumulator{}
	require.Error(t, listener.Start(acc))

	// Stop is called when any ServiceInput fails to start; it must succeed regardless of state
	listener.Stop()
}

func TestWriteHTTPSNoClientAuth(t *testing.T) {
	listener, err := newTestHTTPSListenerV2()
	require.NoError(t, err)
	listener.TLSAllowedCACerts = nil

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	cas := x509.NewCertPool()
	cas.AppendCertsFromPEM([]byte(pki.ReadServerCert()))
	noClientAuthClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: cas,
			},
		},
	}

	// post single message to listener
	resp, err := noClientAuthClient.Post(createURL(listener, "https", "/write", "db=mydb"), "", bytes.NewBufferString(testMsg))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)
}

func TestWriteHTTPSWithClientAuth(t *testing.T) {
	listener, err := newTestHTTPSListenerV2()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post single message to listener
	resp, err := getHTTPSClient().Post(createURL(listener, "https", "/write", "db=mydb"), "", bytes.NewBufferString(testMsg))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)
}

func TestWriteHTTPBasicAuth(t *testing.T) {
	listener, err := newTestHTTPAuthListener()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	client := &http.Client{}

	req, err := http.NewRequest("POST", createURL(listener, "http", "/write", "db=mydb"), bytes.NewBufferString(testMsg))
	require.NoError(t, err)
	req.SetBasicAuth(basicUsername, basicPassword)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, http.StatusNoContent, resp.StatusCode)
}

func TestWriteHTTP(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post single message to listener
	resp, err := http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(testMsg))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	acc.Wait(1)
	acc.AssertContainsTaggedFields(t, "cpu_load_short",
		map[string]interface{}{"value": float64(12)},
		map[string]string{"host": "server01"},
	)

	// post multiple message to listener
	resp, err = http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(testMsgs))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	acc.Wait(2)
	hostTags := []string{"server02", "server03",
		"server04", "server05", "server06"}
	for _, hostTag := range hostTags {
		acc.AssertContainsTaggedFields(t, "cpu_load_short",
			map[string]interface{}{"value": float64(12)},
			map[string]string{"host": hostTag},
		)
	}

	// Post a gigantic metric to the listener and verify that an error is returned:
	resp, err = http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBuffer(hugeMetric))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 413, resp.StatusCode)

	acc.Wait(3)
	acc.AssertContainsTaggedFields(t, "cpu_load_short",
		map[string]interface{}{"value": float64(12)},
		map[string]string{"host": "server01"},
	)
}

// http listener should add request path as configured path_tag
func TestWriteHTTPWithPathTag(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)
	listener.PathTag = true

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post single message to listener
	resp, err := http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(testMsgNoNewline))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	acc.Wait(1)
	acc.AssertContainsTaggedFields(t, "cpu_load_short",
		map[string]interface{}{"value": float64(12)},
		map[string]string{"host": "server01", "http_listener_v2_path": "/write"},
	)
}

func TestWriteHTTPWithReturnCode(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)
	listener.SuccessCode = 200

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post single message to listener
	resp, err := http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(testMsgNoNewline))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 200, resp.StatusCode)
}

// http listener should add request path as configured path_tag (trimming it before)
func TestWriteHTTPWithMultiplePaths(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)
	listener.Paths = []string{"/alternative_write"}
	listener.PathTag = true

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post single message to /write
	resp, err := http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(testMsgNoNewline))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	// post single message to /alternative_write
	resp, err = http.Post(createURL(listener, "http", "/alternative_write", "db=mydb"), "", bytes.NewBufferString(testMsgNoNewline))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	acc.Wait(1)
	acc.AssertContainsTaggedFields(t, "cpu_load_short",
		map[string]interface{}{"value": float64(12)},
		map[string]string{"host": "server01", "http_listener_v2_path": "/write"},
	)

	acc.AssertContainsTaggedFields(t, "cpu_load_short",
		map[string]interface{}{"value": float64(12)},
		map[string]string{"host": "server01", "http_listener_v2_path": "/alternative_write"},
	)
}

// http listener should add a newline at the end of the buffer if it's not there
func TestWriteHTTPNoNewline(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post single message to listener
	resp, err := http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(testMsgNoNewline))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	acc.Wait(1)
	acc.AssertContainsTaggedFields(t, "cpu_load_short",
		map[string]interface{}{"value": float64(12)},
		map[string]string{"host": "server01"},
	)
}

func TestWriteHTTPExactMaxBodySize(t *testing.T) {
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())

	listener := &HTTPListenerV2{
		Log:            testutil.Logger{},
		ServiceAddress: "localhost:0",
		Path:           "/write",
		Methods:        []string{"POST"},
		Parser:         parser,
		MaxBodySize:    config.Size(len(hugeMetric)),
		timeFunc:       time.Now,
		close:          make(chan struct{}),
	}

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	resp, err := http.Post(createURL(listener, "http", "/write", ""), "", bytes.NewBuffer(hugeMetric))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)
}

func TestWriteHTTPVerySmallMaxBody(t *testing.T) {
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())

	listener := &HTTPListenerV2{
		Log:            testutil.Logger{},
		ServiceAddress: "localhost:0",
		Path:           "/write",
		Methods:        []string{"POST"},
		Parser:         parser,
		MaxBodySize:    config.Size(4096),
		timeFunc:       time.Now,
		close:          make(chan struct{}),
	}

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	resp, err := http.Post(createURL(listener, "http", "/write", ""), "", bytes.NewBuffer(hugeMetric))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 413, resp.StatusCode)
}

// test that writing gzipped data works
func TestWriteHTTPGzippedData(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	data, err := os.ReadFile("./testdata/testmsgs.gz")
	require.NoError(t, err)

	req, err := http.NewRequest("POST", createURL(listener, "http", "/write", ""), bytes.NewBuffer(data))
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "gzip")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	hostTags := []string{"server02", "server03",
		"server04", "server05", "server06"}
	acc.Wait(len(hostTags))
	for _, hostTag := range hostTags {
		acc.AssertContainsTaggedFields(t, "cpu_load_short",
			map[string]interface{}{"value": float64(12)},
			map[string]string{"host": hostTag},
		)
	}
}

// test that writing snappy data works
func TestWriteHTTPSnappyData(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	testData := "cpu_load_short,host=server01 value=12.0 1422568543702900257\n"
	encodedData := snappy.Encode(nil, []byte(testData))

	req, err := http.NewRequest("POST", createURL(listener, "http", "/write", ""), bytes.NewBuffer(encodedData))
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "snappy")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Log("Test client request failed. Error: ", err)
	}
	require.NoErrorf(t, resp.Body.Close(), "Test client close failed. Error: %v", err)
	require.NoError(t, err)
	require.EqualValues(t, 204, resp.StatusCode)

	hostTags := []string{"server01"}
	acc.Wait(1)

	for _, hostTag := range hostTags {
		acc.AssertContainsTaggedFields(t, "cpu_load_short",
			map[string]interface{}{"value": float64(12)},
			map[string]string{"host": hostTag},
		)
	}
}

// writes 25,000 metrics to the listener with 10 different writers
func TestWriteHTTPHighTraffic(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("Skipping due to hang on darwin and windows")
	}
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post many messages to listener
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(innerwg *sync.WaitGroup) {
			defer innerwg.Done()
			for i := 0; i < 500; i++ {
				resp, err := http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(testMsgs))
				if err != nil {
					return
				}
				if err := resp.Body.Close(); err != nil {
					return
				}
				if resp.StatusCode != 204 {
					return
				}
			}
		}(&wg)
	}

	wg.Wait()
	require.NoError(t, listener.Gather(acc))

	acc.Wait(25000)
	require.Equal(t, int64(25000), int64(acc.NMetrics()))
}

func TestReceive404ForInvalidEndpoint(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post single message to listener
	resp, err := http.Post(createURL(listener, "http", "/foobar", ""), "", bytes.NewBufferString(testMsg))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 404, resp.StatusCode)
}

func TestWriteHTTPInvalid(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post single message to listener
	resp, err := http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(badMsg))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 400, resp.StatusCode)
}

func TestWriteHTTPEmpty(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post single message to listener
	resp, err := http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(emptyMsg))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)
}

func TestWriteHTTPTransformHeaderValuesToTagsSingleWrite(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)
	listener.HTTPHeaderTags = map[string]string{
		"Present_http_header_1": "presentMeasurementKey1",
		"present_http_header_2": "presentMeasurementKey2",
		"NOT_PRESENT_HEADER":    "notPresentMeasurementKey",
	}

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	req, err := http.NewRequest("POST", createURL(listener, "http", "/write", "db=mydb"), bytes.NewBufferString(testMsg))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "")
	req.Header.Set("Present_http_header_1", "PRESENT_HTTP_VALUE_1")
	req.Header.Set("Present_http_header_2", "PRESENT_HTTP_VALUE_2")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	acc.Wait(1)
	acc.AssertContainsTaggedFields(t, "cpu_load_short",
		map[string]interface{}{"value": float64(12)},
		map[string]string{"host": "server01", "presentMeasurementKey1": "PRESENT_HTTP_VALUE_1", "presentMeasurementKey2": "PRESENT_HTTP_VALUE_2"},
	)

	// post single message to listener
	resp, err = http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(testMsg))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	acc.Wait(1)
	acc.AssertContainsTaggedFields(t, "cpu_load_short",
		map[string]interface{}{"value": float64(12)},
		map[string]string{"host": "server01", "presentMeasurementKey1": "PRESENT_HTTP_VALUE_1", "presentMeasurementKey2": "PRESENT_HTTP_VALUE_2"},
	)
}

func TestWriteHTTPTransformHeaderValuesToTagsBulkWrite(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)
	listener.HTTPHeaderTags = map[string]string{
		"Present_http_header_1": "presentMeasurementKey1",
		"Present_http_header_2": "presentMeasurementKey2",
		"NOT_PRESENT_HEADER":    "notPresentMeasurementKey",
	}

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	req, err := http.NewRequest("POST", createURL(listener, "http", "/write", "db=mydb"), bytes.NewBufferString(testMsgs))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "")
	req.Header.Set("Present_http_header_1", "PRESENT_HTTP_VALUE_1")
	req.Header.Set("Present_http_header_2", "PRESENT_HTTP_VALUE_2")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	acc.Wait(2)
	hostTags := []string{"server02", "server03", "server04", "server05", "server06"}
	for _, hostTag := range hostTags {
		acc.AssertContainsTaggedFields(t, "cpu_load_short",
			map[string]interface{}{"value": float64(12)},
			map[string]string{"host": hostTag, "presentMeasurementKey1": "PRESENT_HTTP_VALUE_1", "presentMeasurementKey2": "PRESENT_HTTP_VALUE_2"},
		)
	}
}

func TestWriteHTTPQueryParams(t *testing.T) {
	parser := form_urlencoded.Parser{
		MetricName: "query_measurement",
		TagKeys:    []string{"tagKey"},
	}

	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)
	listener.DataSource = "query"
	listener.Parser = &parser

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	resp, err := http.Post(createURL(listener, "http", "/write", "tagKey=tagValue&fieldKey=42"), "", bytes.NewBufferString(emptyMsg))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	acc.Wait(1)
	acc.AssertContainsTaggedFields(t, "query_measurement",
		map[string]interface{}{"fieldKey": float64(42)},
		map[string]string{"tagKey": "tagValue"},
	)
}

func TestWriteHTTPFormData(t *testing.T) {
	parser := form_urlencoded.Parser{
		MetricName: "query_measurement",
		TagKeys:    []string{"tagKey"},
	}

	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)
	listener.Parser = &parser

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	resp, err := http.PostForm(createURL(listener, "http", "/write", ""), url.Values{
		"tagKey":   {"tagValue"},
		"fieldKey": {"42"},
	})
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)

	acc.Wait(1)
	acc.AssertContainsTaggedFields(t, "query_measurement",
		map[string]interface{}{"fieldKey": float64(42)},
		map[string]string{"tagKey": "tagValue"},
	)
}

func TestServerHeaders(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)

	listener.HTTPHeaders = map[string]string{
		"key": "value",
	}

	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()

	// post single message to listener
	resp, err := http.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(testMsg))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)
	require.Equal(t, "value", resp.Header.Get("key"))
}

func TestUnixSocket(t *testing.T) {
	listener, err := newTestHTTPListenerV2()
	require.NoError(t, err)
	file, err := os.CreateTemp(t.TempDir(), "*.socket")
	require.NoError(t, err)
	require.NoError(t, file.Close())

	socketName := file.Name()
	if runtime.GOOS == "windows" {
		listener.ServiceAddress = "unix:///" + socketName
	} else {
		listener.ServiceAddress = "unix://" + socketName
	}
	listener.SocketMode = "777"
	acc := &testutil.Accumulator{}
	require.NoError(t, listener.Init())
	require.NoError(t, listener.Start(acc))
	defer listener.Stop()
	httpc := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketName)
			},
		},
	}
	resp, err := httpc.Post(createURL(listener, "http", "/write", "db=mydb"), "", bytes.NewBufferString(testMsg))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 204, resp.StatusCode)
}

func mustReadHugeMetric() []byte {
	filePath := "testdata/huge_metric"
	data, err := os.ReadFile(filePath)
	if err != nil {
		panic(fmt.Errorf("could not read from file %s: %w", filePath, err))
	}

	return data
}

// The term 'master_repl' used here is archaic language from redis
var hugeMetric = mustReadHugeMetric()
