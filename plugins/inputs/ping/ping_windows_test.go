//go:build windows

package ping

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

// Windows ping format ( should support multilanguage ?)
var winPLPingOutput = `
Badanie 8.8.8.8 z 32 bajtami danych:
Odpowiedz z 8.8.8.8: bajtow=32 czas=49ms TTL=43
Odpowiedz z 8.8.8.8: bajtow=32 czas=46ms TTL=43
Odpowiedz z 8.8.8.8: bajtow=32 czas=48ms TTL=43
Odpowiedz z 8.8.8.8: bajtow=32 czas=57ms TTL=43

Statystyka badania ping dla 8.8.8.8:
    Pakiety: Wyslane = 4, Odebrane = 4, Utracone = 0
             (0% straty),
Szacunkowy czas bladzenia pakietww w millisekundach:
    Minimum = 46 ms, Maksimum = 57 ms, Czas sredni = 50 ms
`

// Windows ping format ( should support multilanguage ?)
var winENPingOutput = `
Pinging 8.8.8.8 with 32 bytes of data:
Reply from 8.8.8.8: bytes=32 time=52ms TTL=43
Reply from 8.8.8.8: bytes=32 time=50ms TTL=43
Reply from 8.8.8.8: bytes=32 time=50ms TTL=43
Reply from 8.8.8.8: bytes=32 time=51ms TTL=43

Ping statistics for 8.8.8.8:
    Packets: Sent = 4, Received = 4, Lost = 0 (0% loss),
Approximate round trip times in milli-seconds:
    Minimum = 50ms, Maximum = 52ms, Average = 50ms
`

func TestHost(t *testing.T) {
	stats, err := processPingOutput(winPLPingOutput)
	require.NoError(t, err)
	require.Equal(t, 4, stats.packetsTransmitted, "4 packets were transmitted")
	require.Equal(t, 4, stats.replyReceived, "4 packets were reply")
	require.Equal(t, 4, stats.packetsReceived, "4 packets were received")
	require.Equal(t, 50, stats.avg, "Average 50")
	require.Equal(t, 46, stats.min, "Min 46")
	require.Equal(t, 57, stats.max, "max 57")

	stats, err = processPingOutput(winENPingOutput)
	require.NoError(t, err)
	require.Equal(t, 4, stats.packetsTransmitted, "4 packets were transmitted")
	require.Equal(t, 4, stats.replyReceived, "4 packets were reply")
	require.Equal(t, 4, stats.packetsReceived, "4 packets were received")
	require.Equal(t, 50, stats.avg, "Average 50")
	require.Equal(t, 50, stats.min, "Min 50")
	require.Equal(t, 52, stats.max, "Max 52")
}

func mockHostPinger(string, float64, ...string) (string, error) {
	return winENPingOutput, nil
}

// Test that Gather function works on a normal ping
func TestPingGather(t *testing.T) {
	var acc testutil.Accumulator
	p := Ping{
		Urls:     []string{"www.google.com", "www.reddit.com"},
		pingHost: mockHostPinger,
	}

	require.NoError(t, acc.GatherError(p.Gather))
	tags := map[string]string{"url": "www.google.com"}
	fields := map[string]interface{}{
		"packets_transmitted": 4,
		"packets_received":    4,
		"reply_received":      4,
		"percent_packet_loss": 0.0,
		"percent_reply_loss":  0.0,
		"average_response_ms": 50.0,
		"minimum_response_ms": 50.0,
		"maximum_response_ms": 52.0,
		"result_code":         0,
	}
	acc.AssertContainsTaggedFields(t, "ping", fields, tags)

	tags = map[string]string{"url": "www.reddit.com"}
	acc.AssertContainsTaggedFields(t, "ping", fields, tags)
}

var errorPingOutput = `
Badanie nask.pl [195.187.242.157] z 32 bajtami danych:
Upłynął limit czasu żądania.
Upłynął limit czasu żądania.
Upłynął limit czasu żądania.
Upłynął limit czasu żądania.

Statystyka badania ping dla 195.187.242.157:
    Pakiety: Wysłane = 4, Odebrane = 0, Utracone = 4
             (100% straty),
`

func mockErrorHostPinger(string, float64, ...string) (string, error) {
	return errorPingOutput, errors.New("no packets received")
}

// Test that Gather works on a ping with no transmitted packets, even though the
// command returns an error
func TestBadPingGather(t *testing.T) {
	var acc testutil.Accumulator
	p := Ping{
		Log:      testutil.Logger{},
		Urls:     []string{"www.amazon.com"},
		pingHost: mockErrorHostPinger,
	}

	err := acc.GatherError(p.Gather)
	require.NoError(t, err)

	tags := map[string]string{"url": "www.amazon.com"}
	fields := map[string]interface{}{
		"packets_transmitted": 4,
		"packets_received":    0,
		"reply_received":      0,
		"percent_packet_loss": 100.0,
		"percent_reply_loss":  100.0,
		"result_code":         0,
	}
	acc.AssertContainsTaggedFields(t, "ping", fields, tags)
}

func TestArguments(t *testing.T) {
	arguments := []string{"-c", "3"}
	p := Ping{
		Log:       testutil.Logger{},
		Count:     2,
		Timeout:   12.0,
		Arguments: arguments,
	}

	actual := p.args("www.google.com")
	require.Equal(t, actual, arguments)
}

var lossyPingOutput = `
Badanie thecodinglove.com [66.6.44.4] z 9800 bajtami danych:
Upłynął limit czasu żądania.
Odpowiedź z 66.6.44.4: bajtów=9800 czas=114ms TTL=48
Odpowiedź z 66.6.44.4: bajtów=9800 czas=114ms TTL=48
Odpowiedź z 66.6.44.4: bajtów=9800 czas=118ms TTL=48
Odpowiedź z 66.6.44.4: bajtów=9800 czas=114ms TTL=48
Odpowiedź z 66.6.44.4: bajtów=9800 czas=114ms TTL=48
Upłynął limit czasu żądania.
Odpowiedź z 66.6.44.4: bajtów=9800 czas=119ms TTL=48
Odpowiedź z 66.6.44.4: bajtów=9800 czas=116ms TTL=48

Statystyka badania ping dla 66.6.44.4:
    Pakiety: Wysłane = 9, Odebrane = 7, Utracone = 2
             (22% straty),
Szacunkowy czas błądzenia pakietów w millisekundach:
    Minimum = 114 ms, Maksimum = 119 ms, Czas średni = 115 ms
`

func mockLossyHostPinger(string, float64, ...string) (string, error) {
	return lossyPingOutput, nil
}

// Test that Gather works on a ping with lossy packets
func TestLossyPingGather(t *testing.T) {
	var acc testutil.Accumulator
	p := Ping{
		Log:      testutil.Logger{},
		Urls:     []string{"www.google.com"},
		pingHost: mockLossyHostPinger,
	}

	err := acc.GatherError(p.Gather)
	require.NoError(t, err)

	tags := map[string]string{"url": "www.google.com"}
	fields := map[string]interface{}{
		"packets_transmitted": 9,
		"packets_received":    7,
		"reply_received":      7,
		"percent_packet_loss": 22.22222222222222,
		"percent_reply_loss":  22.22222222222222,
		"average_response_ms": 115.0,
		"minimum_response_ms": 114.0,
		"maximum_response_ms": 119.0,
		"result_code":         0,
	}
	acc.AssertContainsTaggedFields(t, "ping", fields, tags)
}

// Fatal ping output (invalid argument)
var fatalPingOutput = `
Bad option -d.


Usage: ping [-t] [-a] [-n count] [-l size] [-f] [-i TTL] [-v TOS]
            [-r count] [-s count] [[-j host-list] | [-k host-list]]
            [-w timeout] [-R] [-S srcaddr] [-4] [-6] target_name

Options:
    -t             Ping the specified host until stopped.
                   To see statistics and continue - type Control-Break;
                   To stop - type Control-C.
    -a             Resolve addresses to hostnames.
    -n count       Number of echo requests to send.
    -l size        Send buffer size.
    -f             Set Don't Fragment flag in packet (IPv4-only).
    -i TTL         Time To Live.
    -v TOS         Type Of Service (IPv4-only. This setting has been deprecated
                   and has no effect on the type of service field in the IP Header).
    -r count       Record route for count hops (IPv4-only).
    -s count       Timestamp for count hops (IPv4-only).
    -j host-list   Loose source route along host-list (IPv4-only).
    -k host-list   Strict source route along host-list (IPv4-only).
    -w timeout     Timeout in milliseconds to wait for each reply.
    -R             Use routing header to test reverse route also (IPv6-only).
    -S srcaddr     Source address to use.
    -4             Force using IPv4.
    -6             Force using IPv6.

`

func mockFatalHostPinger(string, float64, ...string) (string, error) {
	return fatalPingOutput, errors.New("so very bad")
}

// Test that a fatal ping command does not gather any statistics.
func TestFatalPingGather(t *testing.T) {
	var acc testutil.Accumulator
	p := Ping{
		Log:      testutil.Logger{},
		Urls:     []string{"www.amazon.com"},
		pingHost: mockFatalHostPinger,
	}

	err := acc.GatherError(p.Gather)
	require.Error(t, err)

	require.True(t, acc.HasFloatField("ping", "errors"),
		"Fatal ping should have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "packets_transmitted"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "packets_received"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasFloatField("ping", "percent_packet_loss"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasFloatField("ping", "percent_reply_loss"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "average_response_ms"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "maximum_response_ms"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "minimum_response_ms"),
		"Fatal ping should not have packet measurements")
}

var unreachablePingOutput = `
Pinging www.google.pl [8.8.8.8] with 32 bytes of data:
Request timed out.
Request timed out.
Reply from 194.204.175.50: Destination net unreachable.
Request timed out.

Ping statistics for 8.8.8.8:
    Packets: Sent = 4, Received = 1, Lost = 3 (75% loss),
`

func mockUnreachableHostPinger(string, float64, ...string) (string, error) {
	return unreachablePingOutput, errors.New("so very bad")
}

// Reply from 185.28.251.217: TTL expired in transit.

// in case 'Destination net unreachable' ping app return receive packet which is not what we need
// it's not contain valid metric so treat it as lost one
func TestUnreachablePingGather(t *testing.T) {
	var acc testutil.Accumulator
	p := Ping{
		Log:      testutil.Logger{},
		Urls:     []string{"www.google.com"},
		pingHost: mockUnreachableHostPinger,
	}

	err := acc.GatherError(p.Gather)
	require.NoError(t, err)

	tags := map[string]string{"url": "www.google.com"}
	fields := map[string]interface{}{
		"packets_transmitted": 4,
		"packets_received":    1,
		"reply_received":      0,
		"percent_packet_loss": 75.0,
		"percent_reply_loss":  100.0,
		"result_code":         0,
	}
	acc.AssertContainsTaggedFields(t, "ping", fields, tags)

	require.False(t, acc.HasFloatField("ping", "errors"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "average_response_ms"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "maximum_response_ms"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "minimum_response_ms"),
		"Fatal ping should not have packet measurements")
}

var ttlExpiredPingOutput = `
Pinging www.google.pl [8.8.8.8] with 32 bytes of data:
Request timed out.
Request timed out.
Reply from 185.28.251.217: TTL expired in transit.
Request timed out.

Ping statistics for 8.8.8.8:
    Packets: Sent = 4, Received = 1, Lost = 3 (75% loss),
`

func mockTTLExpiredPinger(string, float64, ...string) (string, error) {
	return ttlExpiredPingOutput, errors.New("so very bad")
}

// in case 'Destination net unreachable' ping app return receive packet which is not what we need
// it's not contain valid metric so treat it as lost one
func TestTTLExpiredPingGather(t *testing.T) {
	var acc testutil.Accumulator
	p := Ping{
		Log:      testutil.Logger{},
		Urls:     []string{"www.google.com"},
		pingHost: mockTTLExpiredPinger,
	}

	err := acc.GatherError(p.Gather)
	require.NoError(t, err)

	tags := map[string]string{"url": "www.google.com"}
	fields := map[string]interface{}{
		"packets_transmitted": 4,
		"packets_received":    1,
		"reply_received":      0,
		"percent_packet_loss": 75.0,
		"percent_reply_loss":  100.0,
		"result_code":         0,
	}
	acc.AssertContainsTaggedFields(t, "ping", fields, tags)

	require.False(t, acc.HasFloatField("ping", "errors"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "average_response_ms"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "maximum_response_ms"),
		"Fatal ping should not have packet measurements")
	require.False(t, acc.HasInt64Field("ping", "minimum_response_ms"),
		"Fatal ping should not have packet measurements")
}

func TestPingBinary(t *testing.T) {
	var acc testutil.Accumulator
	p := Ping{
		Log:    testutil.Logger{},
		Urls:   []string{"www.google.com"},
		Binary: "ping6",
		pingHost: func(binary string, _ float64, _ ...string) (string, error) {
			require.Equal(t, "ping6", binary)
			return "", nil
		},
	}
	err := acc.GatherError(p.Gather)
	require.Error(t, err)
	require.EqualValues(t, "www.google.com: fatal error processing ping output", err.Error())
}
