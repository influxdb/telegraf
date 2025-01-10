package logzio

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

const (
	testToken = "123456789"
	testURL   = "https://logzio.com"
)

func TestConnectWithoutToken(t *testing.T) {
	l := &Logzio{
		URL: testURL,
		Log: testutil.Logger{},
	}
	err := l.Connect()
	require.ErrorContains(t, err, "token is required")
}

func TestConnectWithDefaultToken(t *testing.T) {
	l := &Logzio{
		URL:   testURL,
		Token: config.NewSecret([]byte("your logz.io token")),
		Log:   testutil.Logger{},
	}
	err := l.Connect()
	require.ErrorContains(t, err, "please replace 'token'")
}

func TestParseMetric(t *testing.T) {
	l := &Logzio{}
	for _, tm := range testutil.MockMetrics() {
		lm := l.parseMetric(tm)
		require.Equal(t, tm.Fields(), lm.Metric[tm.Name()])
		require.Equal(t, logzioType, lm.Type)
		require.Equal(t, tm.Tags(), lm.Dimensions)
		require.Equal(t, tm.Time(), lm.Time)
	}
}

func TestBadStatusCode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	l := &Logzio{
		Token: config.NewSecret([]byte(testToken)),
		URL:   ts.URL,
		Log:   testutil.Logger{},
	}
	require.NoError(t, l.Connect())
	require.Error(t, l.Write(testutil.MockMetrics()))
}

func TestWrite(t *testing.T) {
	tm := testutil.TestMetric(float64(3.14), "test1")
	var body bytes.Buffer
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}

		var maxDecompressionSize int64 = 500 * 1024 * 1024
		n, err := io.CopyN(&body, gz, maxDecompressionSize)
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
		if n > maxDecompressionSize {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Size of decoded data exceeds (%v) allowed size (%v)", n, maxDecompressionSize)
			return
		}

		var lm Metric
		if err = json.Unmarshal(body.Bytes(), &lm); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
		if !reflect.DeepEqual(lm.Metric[tm.Name()], tm.Fields()) {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", tm.Fields(), lm.Metric[tm.Name()])
			return
		}
		if lm.Type != logzioType {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", logzioType, lm.Type)
			return
		}
		if !reflect.DeepEqual(lm.Dimensions, tm.Tags()) {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", tm.Tags(), lm.Dimensions)
			return
		}
		if lm.Time != tm.Time() {
			w.WriteHeader(http.StatusInternalServerError)
			t.Errorf("Not equal, expected: %q, actual: %q", tm.Time(), lm.Time)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	l := &Logzio{
		Token: config.NewSecret([]byte(testToken)),
		URL:   ts.URL,
		Log:   testutil.Logger{},
	}
	require.NoError(t, l.Connect())
	require.NoError(t, l.Write([]telegraf.Metric{tm}))
}
