package metric

import (
	"io"
	"io/ioutil"
	"regexp"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func BenchmarkMetricReader(b *testing.B) {
	metrics := make([]telegraf.Metric, 10)
	for i := 0; i < 10; i++ {
		metrics[i], _ = New("foo", map[string]string{},
			map[string]interface{}{"value": int64(1)}, time.Now())
	}
	for n := 0; n < b.N; n++ {
		r := NewReader(metrics)
		io.Copy(ioutil.Discard, r)
	}
}

func TestMetricReader(t *testing.T) {
	ts := time.Unix(1481032190, 0)
	metrics := make([]telegraf.Metric, 10)
	for i := 0; i < 10; i++ {
		metrics[i], _ = New("foo", map[string]string{},
			map[string]interface{}{"value": int64(1)}, ts)
	}

	r := NewReader(metrics)

	buf := make([]byte, 35)
	for i := 0; i < 10; i++ {
		n, err := r.Read(buf)
		if err != nil {
			assert.True(t, err == io.EOF, err.Error())
		}
		assert.Equal(t, 33, n)
		assert.Equal(t, "foo value=1i 1481032190000000000\n", string(buf[0:n]))
	}

	// reader should now be done, and always return 0, io.EOF
	for i := 0; i < 10; i++ {
		n, err := r.Read(buf)
		assert.True(t, err == io.EOF, err.Error())
		assert.Equal(t, 0, n)
	}
}

func TestMetricReader_OverflowMetric(t *testing.T) {
	ts := time.Unix(1481032190, 0)
	m, _ := New("foo", map[string]string{},
		map[string]interface{}{"value": int64(10)}, ts)
	metrics := []telegraf.Metric{m}

	r := NewReader(metrics)
	buf := make([]byte, 5)

	tests := []struct {
		exp string
		err error
		n   int
	}{
		{
			"foo v",
			nil,
			5,
		},
		{
			"alue=",
			nil,
			5,
		},
		{
			"10i 1",
			nil,
			5,
		},
		{
			"48103",
			nil,
			5,
		},
		{
			"21900",
			nil,
			5,
		},
		{
			"00000",
			nil,
			5,
		},
		{
			"000\n",
			io.EOF,
			4,
		},
		{
			"",
			io.EOF,
			0,
		},
	}

	for _, test := range tests {
		n, err := r.Read(buf)
		assert.Equal(t, test.n, n)
		assert.Equal(t, test.exp, string(buf[0:n]))
		assert.Equal(t, test.err, err)
	}
}

// Regression test for when a metric is the same size as the buffer.
//
// Previously EOF would not be set until the next call to Read.
func TestMetricReader_MetricSizeEqualsBufferSize(t *testing.T) {
	ts := time.Unix(1481032190, 0)
	m1, _ := New("foo", map[string]string{},
		map[string]interface{}{"a": int64(1)}, ts)
	metrics := []telegraf.Metric{m1}

	r := NewReader(metrics)
	buf := make([]byte, m1.Len())

	for {
		n, err := r.Read(buf)
		// Should never read 0 bytes unless at EOF, unless input buffer is 0 length
		if n == 0 {
			require.Equal(t, io.EOF, err)
			break
		}
		// Lines should be terminated with a LF
		if err == io.EOF {
			require.Equal(t, uint8('\n'), buf[n-1])
			break
		}
		require.NoError(t, err)
	}
}

// Regression test for when a metric requires to be split and one of the
// split metrics is exactly the size of the buffer.
//
// Previously an empty string would be returned on the next Read without error,
// and then next Read call would panic.
func TestMetricReader_SplitWithExactLengthSplit(t *testing.T) {
	ts := time.Unix(1481032190, 0)
	m1, _ := New("foo", map[string]string{},
		map[string]interface{}{"a": int64(1), "bb": int64(2)}, ts)
	metrics := []telegraf.Metric{m1}

	r := NewReader(metrics)
	buf := make([]byte, 30)

	//  foo a=1i,bb=2i 1481032190000000000\n // len 35
	//
	// Requires this specific split order:
	//  foo a=1i 1481032190000000000\n  // len 29
	//  foo bb=2i 1481032190000000000\n // len 30

	for {
		n, err := r.Read(buf)
		// Should never read 0 bytes unless at EOF, unless input buffer is 0 length
		if n == 0 {
			require.Equal(t, io.EOF, err)
			break
		}
		// Lines should be terminated with a LF
		if err == io.EOF {
			require.Equal(t, uint8('\n'), buf[n-1])
			break
		}
		require.NoError(t, err)
	}
}

func TestMetricReader_OverflowMultipleMetrics(t *testing.T) {
	ts := time.Unix(1481032190, 0)
	m, _ := New("foo", map[string]string{},
		map[string]interface{}{"value": int64(10)}, ts)
	metrics := []telegraf.Metric{m, m.Copy()}

	r := NewReader(metrics)
	buf := make([]byte, 10)

	tests := []struct {
		exp string
		err error
		n   int
	}{
		{
			"foo value=",
			nil,
			10,
		},
		{
			"10i 148103",
			nil,
			10,
		},
		{
			"2190000000",
			nil,
			10,
		},
		{
			"000\n",
			nil,
			4,
		},
		{
			"foo value=",
			nil,
			10,
		},
		{
			"10i 148103",
			nil,
			10,
		},
		{
			"2190000000",
			nil,
			10,
		},
		{
			"000\n",
			io.EOF,
			4,
		},
		{
			"",
			io.EOF,
			0,
		},
	}

	for _, test := range tests {
		n, err := r.Read(buf)
		assert.Equal(t, test.n, n)
		assert.Equal(t, test.exp, string(buf[0:n]))
		assert.Equal(t, test.err, err)
	}
}

// test splitting a metric
func TestMetricReader_SplitMetric(t *testing.T) {
	ts := time.Unix(1481032190, 0)
	m1, _ := New("foo", map[string]string{},
		map[string]interface{}{
			"value1": int64(10),
			"value2": int64(10),
			"value3": int64(10),
			"value4": int64(10),
			"value5": int64(10),
			"value6": int64(10),
		},
		ts,
	)
	metrics := []telegraf.Metric{m1}

	r := NewReader(metrics)
	buf := make([]byte, 60)

	tests := []struct {
		expRegex string
		err      error
		n        int
	}{
		{
			`foo value\d=10i,value\d=10i,value\d=10i 1481032190000000000\n`,
			nil,
			57,
		},
		{
			`foo value\d=10i,value\d=10i,value\d=10i 1481032190000000000\n`,
			io.EOF,
			57,
		},
		{
			"",
			io.EOF,
			0,
		},
	}

	for _, test := range tests {
		n, err := r.Read(buf)
		assert.Equal(t, test.n, n)
		re := regexp.MustCompile(test.expRegex)
		assert.True(t, re.MatchString(string(buf[0:n])), string(buf[0:n]))
		assert.Equal(t, test.err, err)
	}
}

// test an array with one split metric and one unsplit
func TestMetricReader_SplitMetric2(t *testing.T) {
	ts := time.Unix(1481032190, 0)
	m1, _ := New("foo", map[string]string{},
		map[string]interface{}{
			"value1": int64(10),
			"value2": int64(10),
			"value3": int64(10),
			"value4": int64(10),
			"value5": int64(10),
			"value6": int64(10),
		},
		ts,
	)
	m2, _ := New("foo", map[string]string{},
		map[string]interface{}{
			"value1": int64(10),
		},
		ts,
	)
	metrics := []telegraf.Metric{m1, m2}

	r := NewReader(metrics)
	buf := make([]byte, 60)

	tests := []struct {
		expRegex string
		err      error
		n        int
	}{
		{
			`foo value\d=10i,value\d=10i,value\d=10i 1481032190000000000\n`,
			nil,
			57,
		},
		{
			`foo value\d=10i,value\d=10i,value\d=10i 1481032190000000000\n`,
			nil,
			57,
		},
		{
			`foo value1=10i 1481032190000000000\n`,
			io.EOF,
			35,
		},
		{
			"",
			io.EOF,
			0,
		},
	}

	for _, test := range tests {
		n, err := r.Read(buf)
		assert.Equal(t, test.n, n)
		re := regexp.MustCompile(test.expRegex)
		assert.True(t, re.MatchString(string(buf[0:n])), string(buf[0:n]))
		assert.Equal(t, test.err, err)
	}
}

// test split that results in metrics that are still too long, which results in
// the reader falling back to regular overflow.
func TestMetricReader_SplitMetricTooLong(t *testing.T) {
	ts := time.Unix(1481032190, 0)
	m1, _ := New("foo", map[string]string{},
		map[string]interface{}{
			"value1": int64(10),
			"value2": int64(10),
		},
		ts,
	)
	metrics := []telegraf.Metric{m1}

	r := NewReader(metrics)
	buf := make([]byte, 30)

	tests := []struct {
		expRegex string
		err      error
		n        int
	}{
		{
			`foo value\d=10i,value\d=10i 1481`,
			nil,
			30,
		},
		{
			`032190000000000\n`,
			io.EOF,
			16,
		},
		{
			"",
			io.EOF,
			0,
		},
	}

	for _, test := range tests {
		n, err := r.Read(buf)
		assert.Equal(t, test.n, n)
		re := regexp.MustCompile(test.expRegex)
		assert.True(t, re.MatchString(string(buf[0:n])), string(buf[0:n]))
		assert.Equal(t, test.err, err)
	}
}

// test split with a changing buffer size in the middle of subsequent calls
// to Read
func TestMetricReader_SplitMetricChangingBuffer(t *testing.T) {
	ts := time.Unix(1481032190, 0)
	m1, _ := New("foo", map[string]string{},
		map[string]interface{}{
			"value1": int64(10),
			"value2": int64(10),
			"value3": int64(10),
		},
		ts,
	)
	m2, _ := New("foo", map[string]string{},
		map[string]interface{}{
			"value1": int64(10),
		},
		ts,
	)
	metrics := []telegraf.Metric{m1, m2}

	r := NewReader(metrics)

	tests := []struct {
		expRegex string
		err      error
		n        int
		buf      []byte
	}{
		{
			`foo value\d=10i 1481032190000000000\n`,
			nil,
			35,
			make([]byte, 36),
		},
		{
			`foo value\d=10i 148103219000000`,
			nil,
			30,
			make([]byte, 30),
		},
		{
			`0000\n`,
			nil,
			5,
			make([]byte, 30),
		},
		{
			`foo value\d=10i 1481032190000000000\n`,
			nil,
			35,
			make([]byte, 36),
		},
		{
			`foo value1=10i 1481032190000000000\n`,
			io.EOF,
			35,
			make([]byte, 36),
		},
		{
			"",
			io.EOF,
			0,
			make([]byte, 36),
		},
	}

	for _, test := range tests {
		n, err := r.Read(test.buf)
		assert.Equal(t, test.n, n, test.expRegex)
		re := regexp.MustCompile(test.expRegex)
		assert.True(t, re.MatchString(string(test.buf[0:n])), string(test.buf[0:n]))
		assert.Equal(t, test.err, err, test.expRegex)
	}
}

// test split with a changing buffer size in the middle of subsequent calls
// to Read
func TestMetricReader_SplitMetricChangingBuffer2(t *testing.T) {
	ts := time.Unix(1481032190, 0)
	m1, _ := New("foo", map[string]string{},
		map[string]interface{}{
			"value1": int64(10),
			"value2": int64(10),
		},
		ts,
	)
	m2, _ := New("foo", map[string]string{},
		map[string]interface{}{
			"value1": int64(10),
		},
		ts,
	)
	metrics := []telegraf.Metric{m1, m2}

	r := NewReader(metrics)

	tests := []struct {
		expRegex string
		err      error
		n        int
		buf      []byte
	}{
		{
			`foo value\d=10i 1481032190000000000\n`,
			nil,
			35,
			make([]byte, 36),
		},
		{
			`foo value\d=10i 148103219000000`,
			nil,
			30,
			make([]byte, 30),
		},
		{
			`0000\n`,
			nil,
			5,
			make([]byte, 30),
		},
		{
			`foo value1=10i 1481032190000000000\n`,
			io.EOF,
			35,
			make([]byte, 36),
		},
		{
			"",
			io.EOF,
			0,
			make([]byte, 36),
		},
	}

	for _, test := range tests {
		n, err := r.Read(test.buf)
		assert.Equal(t, test.n, n, test.expRegex)
		re := regexp.MustCompile(test.expRegex)
		assert.True(t, re.MatchString(string(test.buf[0:n])), string(test.buf[0:n]))
		assert.Equal(t, test.err, err, test.expRegex)
	}
}
