package whatap

import (
	"fmt"
	"net"
	"os"
	"testing"

	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"

	whatap_hash "github.com/whatap/go-api/common/util/hash"

	"github.com/stretchr/testify/require"
)

func newWhatap() *Whatap {
	hostname, _ := os.Hostname()
	return &Whatap{
		Timeout: config.Duration(60 * time.Second),
		Log:     testutil.Logger{},
		oname:   hostname,
		oid:     whatap_hash.HashStr(hostname),
	}
}
func TestWhatapConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	w := newWhatap()
	addr := listener.Addr().String()
	fmt.Println(addr)

	w.Servers = append(w.Servers, fmt.Sprintf("%s://%s", "tcp", addr))
	require.NoError(t, err)

	err = w.Connect()
	require.NoError(t, err)

	_, err = listener.Accept()
	require.NoError(t, err)
}

func TestWhatapWriteErr(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	w := newWhatap()
	addr := listener.Addr().String()
	fmt.Println(addr)

	w.Servers = append(w.Servers, fmt.Sprintf("%s://%s", "tcp", addr))
	require.NoError(t, err)

	err = w.Connect()
	require.NoError(t, err)

	lconn, err := listener.Accept()
	require.NoError(t, err)
	err = lconn.(*net.TCPConn).SetWriteBuffer(256)
	require.NoError(t, err)

	metrics := []telegraf.Metric{testutil.TestMetric(1, "testerr")}

	err = lconn.Close()
	require.NoError(t, err)

	_ = w.Close()
	err = w.Write(metrics)
	require.NoError(t, err)
}
