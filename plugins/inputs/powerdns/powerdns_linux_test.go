//go:build !windows

package powerdns

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func serverSocket(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}

		go func(c net.Conn) {
			buf := make([]byte, 1024)
			n, _ := c.Read(buf) //nolint:errcheck // ignore the returned error as we need to close the socket anyway

			data := buf[:n]
			if string(data) == "show * \n" {
				c.Write([]byte(metrics)) //nolint:errcheck // ignore the returned error as we need to close the socket anyway
				c.Close()
			}
		}(conn)
	}
}

func TestPowerdnsGeneratesMetrics(t *testing.T) {
	// We create a fake server to return test data
	randomNumber := int64(5239846799706671610)
	sockname := filepath.Join(os.TempDir(), fmt.Sprintf("pdns%d.controlsocket", randomNumber))
	socket, err := net.Listen("unix", sockname)
	if err != nil {
		t.Fatal("Cannot initialize server on port ")
	}

	defer socket.Close()

	go serverSocket(socket)

	p := &Powerdns{
		UnixSockets: []string{sockname},
	}

	var acc testutil.Accumulator
	err = acc.GatherError(p.Gather)
	require.NoError(t, err)

	intMetrics := []string{"corrupt-packets", "deferred-cache-inserts",
		"deferred-cache-lookup", "dnsupdate-answers", "dnsupdate-changes",
		"dnsupdate-queries", "dnsupdate-refused", "packetcache-hit",
		"packetcache-miss", "packetcache-size", "query-cache-hit", "query-cache-miss",
		"rd-queries", "recursing-answers", "recursing-questions",
		"recursion-unanswered", "security-status", "servfail-packets", "signatures",
		"tcp-answers", "tcp-queries", "timedout-packets", "udp-answers",
		"udp-answers-bytes", "udp-do-queries", "udp-queries", "udp4-answers",
		"udp4-queries", "udp6-answers", "udp6-queries", "key-cache-size", "latency",
		"meta-cache-size", "qsize-q", "signature-cache-size", "sys-msec", "uptime", "user-msec"}

	for _, metric := range intMetrics {
		require.True(t, acc.HasInt64Field("powerdns", metric), metric)
	}
}
