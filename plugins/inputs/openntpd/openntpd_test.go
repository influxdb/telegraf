package openntpd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

func openntpdCTL(output string) func(string, config.Duration, bool) (*bytes.Buffer, error) {
	return func(string, config.Duration, bool) (*bytes.Buffer, error) {
		return bytes.NewBufferString(output), nil
	}
}

func TestParseSimpleOutput(t *testing.T) {
	acc := &testutil.Accumulator{}
	v := &Openntpd{
		run: openntpdCTL(simpleOutput),
	}
	err := v.Gather(acc)

	require.NoError(t, err)
	require.True(t, acc.HasMeasurement("openntpd"))
	require.Equal(t, uint64(1), acc.NMetrics())

	require.Equal(t, 7, acc.NFields())

	firstpeerfields := map[string]interface{}{
		"wt":     int64(1),
		"tl":     int64(10),
		"next":   int64(56),
		"poll":   int64(63),
		"offset": float64(9.271),
		"delay":  float64(44.662),
		"jitter": float64(2.678),
	}

	firstpeertags := map[string]string{
		"remote":  "212.129.9.36",
		"stratum": "3",
	}

	acc.AssertContainsTaggedFields(t, "openntpd", firstpeerfields, firstpeertags)
}

func TestParseSimpleOutputwithStatePrefix(t *testing.T) {
	acc := &testutil.Accumulator{}
	v := &Openntpd{
		run: openntpdCTL(simpleOutputwithStatePrefix),
	}
	err := v.Gather(acc)

	require.NoError(t, err)
	require.True(t, acc.HasMeasurement("openntpd"))
	require.Equal(t, uint64(1), acc.NMetrics())

	require.Equal(t, 7, acc.NFields())

	firstpeerfields := map[string]interface{}{
		"wt":     int64(1),
		"tl":     int64(10),
		"next":   int64(45),
		"poll":   int64(980),
		"offset": float64(-9.901),
		"delay":  float64(67.573),
		"jitter": float64(29.350),
	}

	firstpeertags := map[string]string{
		"remote":       "92.243.6.5",
		"stratum":      "2",
		"state_prefix": "*",
	}

	acc.AssertContainsTaggedFields(t, "openntpd", firstpeerfields, firstpeertags)
}

func TestParseSimpleOutputInvalidPeer(t *testing.T) {
	acc := &testutil.Accumulator{}
	v := &Openntpd{
		run: openntpdCTL(simpleOutputInvalidPeer),
	}
	err := v.Gather(acc)

	require.NoError(t, err)
	require.True(t, acc.HasMeasurement("openntpd"))
	require.Equal(t, uint64(1), acc.NMetrics())

	require.Equal(t, 4, acc.NFields())

	firstpeerfields := map[string]interface{}{
		"wt":   int64(1),
		"tl":   int64(2),
		"next": int64(203),
		"poll": int64(300),
	}

	firstpeertags := map[string]string{
		"remote":  "178.33.111.49",
		"stratum": "-",
	}

	acc.AssertContainsTaggedFields(t, "openntpd", firstpeerfields, firstpeertags)
}

func TestParseSimpleOutputServersDNSError(t *testing.T) {
	acc := &testutil.Accumulator{}
	v := &Openntpd{
		run: openntpdCTL(simpleOutputServersDNSError),
	}
	err := v.Gather(acc)

	require.NoError(t, err)
	require.True(t, acc.HasMeasurement("openntpd"))
	require.Equal(t, uint64(1), acc.NMetrics())

	require.Equal(t, 4, acc.NFields())

	firstpeerfields := map[string]interface{}{
		"next": int64(2),
		"poll": int64(15),
		"wt":   int64(1),
		"tl":   int64(2),
	}

	firstpeertags := map[string]string{
		"remote":  "pool.nl.ntp.org",
		"stratum": "-",
	}

	acc.AssertContainsTaggedFields(t, "openntpd", firstpeerfields, firstpeertags)

	secondpeerfields := map[string]interface{}{
		"next": int64(2),
		"poll": int64(15),
		"wt":   int64(1),
		"tl":   int64(2),
	}

	secondpeertags := map[string]string{
		"remote":  "pool.nl.ntp.org",
		"stratum": "-",
	}

	acc.AssertContainsTaggedFields(t, "openntpd", secondpeerfields, secondpeertags)
}

func TestParseSimpleOutputServerDNSError(t *testing.T) {
	acc := &testutil.Accumulator{}
	v := &Openntpd{
		run: openntpdCTL(simpleOutputServerDNSError),
	}
	err := v.Gather(acc)

	require.NoError(t, err)
	require.True(t, acc.HasMeasurement("openntpd"))
	require.Equal(t, uint64(1), acc.NMetrics())

	require.Equal(t, 4, acc.NFields())

	firstpeerfields := map[string]interface{}{
		"next": int64(12),
		"poll": int64(15),
		"wt":   int64(1),
		"tl":   int64(2),
	}

	firstpeertags := map[string]string{
		"remote":  "pool.fr.ntp.org",
		"stratum": "-",
	}

	acc.AssertContainsTaggedFields(t, "openntpd", firstpeerfields, firstpeertags)
}

func TestParseFullOutput(t *testing.T) {
	acc := &testutil.Accumulator{}
	v := &Openntpd{
		run: openntpdCTL(fullOutput),
	}
	err := v.Gather(acc)

	require.NoError(t, err)
	require.True(t, acc.HasMeasurement("openntpd"))
	require.Equal(t, uint64(20), acc.NMetrics())

	require.Equal(t, 113, acc.NFields())

	firstpeerfields := map[string]interface{}{
		"wt":     int64(1),
		"tl":     int64(10),
		"next":   int64(56),
		"poll":   int64(63),
		"offset": float64(9.271),
		"delay":  float64(44.662),
		"jitter": float64(2.678),
	}

	firstpeertags := map[string]string{
		"remote":  "212.129.9.36",
		"stratum": "3",
	}

	acc.AssertContainsTaggedFields(t, "openntpd", firstpeerfields, firstpeertags)

	secondpeerfields := map[string]interface{}{
		"wt":     int64(1),
		"tl":     int64(10),
		"next":   int64(21),
		"poll":   int64(64),
		"offset": float64(-0.103),
		"delay":  float64(53.199),
		"jitter": float64(9.046),
	}

	secondpeertags := map[string]string{
		"remote":  "163.172.25.19",
		"stratum": "2",
	}

	acc.AssertContainsTaggedFields(t, "openntpd", secondpeerfields, secondpeertags)

	thirdpeerfields := map[string]interface{}{
		"wt":     int64(1),
		"tl":     int64(10),
		"next":   int64(45),
		"poll":   int64(980),
		"offset": float64(-9.901),
		"delay":  float64(67.573),
		"jitter": float64(29.350),
	}

	thirdpeertags := map[string]string{
		"remote":       "92.243.6.5",
		"stratum":      "2",
		"state_prefix": "*",
	}

	acc.AssertContainsTaggedFields(t, "openntpd", thirdpeerfields, thirdpeertags)

	fourthpeerfields := map[string]interface{}{
		"wt":   int64(1),
		"tl":   int64(2),
		"next": int64(203),
		"poll": int64(300),
	}

	fourthpeertags := map[string]string{
		"remote":  "178.33.111.49",
		"stratum": "-",
	}

	acc.AssertContainsTaggedFields(t, "openntpd", fourthpeerfields, fourthpeertags)
}

var simpleOutput = `peer
wt tl st  next  poll          offset       delay      jitter
212.129.9.36 from pool 0.debian.pool.ntp.org
1 10  3   56s   63s         9.271ms    44.662ms     2.678ms`

var simpleOutputwithStatePrefix = `peer
wt tl st  next  poll          offset       delay      jitter
92.243.6.5 from pool 0.debian.pool.ntp.org
*  1 10  2   45s  980s        -9.901ms    67.573ms    29.350ms`

var simpleOutputInvalidPeer = `peer
wt tl st  next  poll          offset       delay      jitter
178.33.111.49 from pool 0.debian.pool.ntp.org
1  2  -  203s  300s             ---- peer not valid ----`

var simpleOutputServersDNSError = `peer
wt tl st  next  poll          offset       delay      jitter
not resolved from pool pool.nl.ntp.org
1  2  -    2s   15s             ---- peer not valid ----
`
var simpleOutputServerDNSError = `peer
wt tl st  next  poll          offset       delay      jitter
not resolved pool.fr.ntp.org
1  2  -   12s   15s             ---- peer not valid ----
`

var fullOutput = `peer
wt tl st  next  poll          offset       delay      jitter
212.129.9.36 from pool 0.debian.pool.ntp.org
1 10  3   56s   63s         9.271ms    44.662ms     2.678ms
163.172.25.19 from pool 0.debian.pool.ntp.org
1 10  2   21s   64s        -0.103ms    53.199ms     9.046ms
92.243.6.5 from pool 0.debian.pool.ntp.org
*  1 10  2   45s  980s        -9.901ms    67.573ms    29.350ms
178.33.111.49 from pool 0.debian.pool.ntp.org
1  2  -  203s  300s             ---- peer not valid ----
62.210.122.129 from pool 1.debian.pool.ntp.org
1 10  3    4s   60s         5.372ms    53.690ms    14.700ms
163.172.225.159 from pool 1.debian.pool.ntp.org
1 10  3   38s   61s        12.276ms    40.631ms     1.282ms
5.196.192.58 from pool 1.debian.pool.ntp.org
1  2  -    0s  300s             ---- peer not valid ----
129.250.35.250 from pool 1.debian.pool.ntp.org
1 10  2   28s   63s        11.236ms    43.874ms     1.381ms
2001:41d0:a:5a7::1 from pool 2.debian.pool.ntp.org
1  2  -    5s   15s             ---- peer not valid ----
2001:41d0:8:188d::16 from pool 2.debian.pool.ntp.org
1  2  -    3s   15s             ---- peer not valid ----
2001:4b98:dc0:41:216:3eff:fe69:46e3 from pool 2.debian.pool.ntp.org
1  2  -   14s   15s             ---- peer not valid ----
2a01:e0d:1:3:58bf:fa61:0:1 from pool 2.debian.pool.ntp.org
1  2  -    9s   15s             ---- peer not valid ----
163.172.179.38 from pool 2.debian.pool.ntp.org
1 10  2   51s   65s       -19.229ms    85.404ms    48.734ms
5.135.3.88 from pool 2.debian.pool.ntp.org
1  2  -  173s  300s             ---- peer not valid ----
195.154.41.195 from pool 2.debian.pool.ntp.org
1 10  2   84s 1004s        -3.956ms    54.549ms    13.658ms
62.210.81.130 from pool 2.debian.pool.ntp.org
1 10  2  158s 1043s       -42.593ms   124.353ms    94.230ms
149.202.97.123 from pool 3.debian.pool.ntp.org
1  2  -  205s  300s             ---- peer not valid ----
51.15.175.224 from pool 3.debian.pool.ntp.org
1 10  2    9s   64s         8.861ms    46.640ms     0.668ms
37.187.5.167 from pool 3.debian.pool.ntp.org
1  2  -  105s  300s             ---- peer not valid ----
194.57.169.1 from pool 3.debian.pool.ntp.org
1 10  2   32s   63s         6.589ms    52.051ms     2.057ms`
