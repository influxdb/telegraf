package net_response

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"

	"github.com/stretchr/testify/require"
)

func TestBadProtocol(t *testing.T) {
	// Init plugin
	c := NetResponse{
		Protocol: "unknownprotocol",
		Address:  ":9999",
	}
	// Error
	err := c.Init()
	require.Error(t, err)
	require.Equal(t, "config option protocol: unknown choice unknownprotocol", err.Error())
}

func TestNoPort(t *testing.T) {
	c := NetResponse{
		Protocol: "tcp",
		Address:  ":",
	}
	err := c.Init()
	require.Error(t, err)
	require.Equal(t, "bad port in config option address", err.Error())
}

func TestAddressOnly(t *testing.T) {
	c := NetResponse{
		Protocol: "tcp",
		Address:  "127.0.0.1",
	}
	err := c.Init()
	require.Error(t, err)
	require.Equal(t, "address 127.0.0.1: missing port in address", err.Error())
}

func TestSendExpectStrings(t *testing.T) {
	tc := NetResponse{
		Protocol: "udp",
		Address:  "127.0.0.1:7",
		Send:     "",
		Expect:   "toast",
	}
	uc := NetResponse{
		Protocol: "udp",
		Address:  "127.0.0.1:7",
		Send:     "toast",
		Expect:   "",
	}
	err := tc.Init()
	require.Error(t, err)
	require.Equal(t, "send string cannot be empty", err.Error())
	err = uc.Init()
	require.Error(t, err)
	require.Equal(t, "expected string cannot be empty", err.Error())
}

func TestTCPError(t *testing.T) {
	var acc testutil.Accumulator
	// Init plugin
	c := NetResponse{
		Protocol: "tcp",
		Address:  ":9999",
		Timeout:  config.Duration(time.Second * 30),
	}
	require.NoError(t, c.Init())
	// Gather
	require.NoError(t, c.Gather(&acc))
	acc.AssertContainsTaggedFields(t,
		"net_response",
		map[string]interface{}{
			"result_code": uint64(2),
			"result_type": "connection_failed",
		},
		map[string]string{
			"server":   "localhost",
			"port":     "9999",
			"protocol": "tcp",
			"result":   "connection_failed",
		},
	)
}

func TestTCPOK1(t *testing.T) {
	var wg sync.WaitGroup
	var acc testutil.Accumulator
	// Init plugin
	c := NetResponse{
		Address:     "127.0.0.1:2004",
		Send:        "test",
		Expect:      "test",
		ReadTimeout: config.Duration(time.Second * 3),
		Timeout:     config.Duration(time.Second),
		Protocol:    "tcp",
	}
	require.NoError(t, c.Init())
	// Start TCP server
	wg.Add(1)
	go tcpServer(t, &wg)
	wg.Wait() // Wait for the server to spin up
	wg.Add(1)
	// Connect
	require.NoError(t, c.Gather(&acc))
	acc.Wait(1)

	// Override response time
	for _, p := range acc.Metrics {
		p.Fields["response_time"] = 1.0
	}
	acc.AssertContainsTaggedFields(t,
		"net_response",
		map[string]interface{}{
			"result_code":   uint64(0),
			"result_type":   "success",
			"string_found":  true,
			"response_time": 1.0,
		},
		map[string]string{
			"result":   "success",
			"server":   "127.0.0.1",
			"port":     "2004",
			"protocol": "tcp",
		},
	)
	// Waiting TCPserver
	wg.Wait()
}

func TestTCPOK2(t *testing.T) {
	var wg sync.WaitGroup
	var acc testutil.Accumulator
	// Init plugin
	c := NetResponse{
		Address:     "127.0.0.1:2004",
		Send:        "test",
		Expect:      "test2",
		ReadTimeout: config.Duration(time.Second * 3),
		Timeout:     config.Duration(time.Second),
		Protocol:    "tcp",
	}
	require.NoError(t, c.Init())
	// Start TCP server
	wg.Add(1)
	go tcpServer(t, &wg)
	wg.Wait()
	wg.Add(1)

	// Connect
	require.NoError(t, c.Gather(&acc))
	acc.Wait(1)

	// Override response time
	for _, p := range acc.Metrics {
		p.Fields["response_time"] = 1.0
	}
	acc.AssertContainsTaggedFields(t,
		"net_response",
		map[string]interface{}{
			"result_code":   uint64(4),
			"result_type":   "string_mismatch",
			"string_found":  false,
			"response_time": 1.0,
		},
		map[string]string{
			"result":   "string_mismatch",
			"server":   "127.0.0.1",
			"port":     "2004",
			"protocol": "tcp",
		},
	)
	// Waiting TCPserver
	wg.Wait()
}

func TestUDPError(t *testing.T) {
	var acc testutil.Accumulator
	// Init plugin
	c := NetResponse{
		Address:  ":9999",
		Send:     "test",
		Expect:   "test",
		Protocol: "udp",
	}
	require.NoError(t, c.Init())
	// Gather
	require.NoError(t, c.Gather(&acc))
	acc.Wait(1)

	// Override response time
	for _, p := range acc.Metrics {
		p.Fields["response_time"] = 1.0
	}
	// Error
	acc.AssertContainsTaggedFields(t,
		"net_response",
		map[string]interface{}{
			"result_code":   uint64(3),
			"result_type":   "read_failed",
			"response_time": 1.0,
			"string_found":  false,
		},
		map[string]string{
			"result":   "read_failed",
			"server":   "localhost",
			"port":     "9999",
			"protocol": "udp",
		},
	)
}

func TestUDPOK1(t *testing.T) {
	var wg sync.WaitGroup
	var acc testutil.Accumulator
	// Init plugin
	c := NetResponse{
		Address:     "127.0.0.1:2004",
		Send:        "test",
		Expect:      "test",
		ReadTimeout: config.Duration(time.Second * 3),
		Timeout:     config.Duration(time.Second),
		Protocol:    "udp",
	}
	require.NoError(t, c.Init())
	// Start UDP server
	wg.Add(1)
	go udpServer(t, &wg)
	wg.Wait()
	wg.Add(1)

	// Connect
	require.NoError(t, c.Gather(&acc))
	acc.Wait(1)

	// Override response time
	for _, p := range acc.Metrics {
		p.Fields["response_time"] = 1.0
	}
	acc.AssertContainsTaggedFields(t,
		"net_response",
		map[string]interface{}{
			"result_code":   uint64(0),
			"result_type":   "success",
			"string_found":  true,
			"response_time": 1.0,
		},
		map[string]string{
			"result":   "success",
			"server":   "127.0.0.1",
			"port":     "2004",
			"protocol": "udp",
		},
	)
	// Waiting TCPserver
	wg.Wait()
}

func udpServer(t *testing.T, wg *sync.WaitGroup) {
	defer wg.Done()
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:2004")
	if err != nil {
		t.Error(err)
		return
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Error(err)
		return
	}

	wg.Done()
	buf := make([]byte, 1024)
	_, remoteaddr, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Error(err)
		return
	}

	if _, err = conn.WriteToUDP(buf, remoteaddr); err != nil {
		t.Error(err)
		return
	}

	if err = conn.Close(); err != nil {
		t.Error(err)
		return
	}
}

func tcpServer(t *testing.T, wg *sync.WaitGroup) {
	defer wg.Done()
	tcpAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:2004")
	if err != nil {
		t.Error(err)
		return
	}

	tcpServer, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		t.Error(err)
		return
	}

	wg.Done()
	conn, err := tcpServer.AcceptTCP()
	if err != nil {
		t.Error(err)
		return
	}

	buf := make([]byte, 1024)
	if _, err = conn.Read(buf); err != nil {
		t.Error(err)
		return
	}

	if _, err = conn.Write(buf); err != nil {
		t.Error(err)
		return
	}

	if err = conn.CloseWrite(); err != nil {
		t.Error(err)
		return
	}

	if err = tcpServer.Close(); err != nil {
		t.Error(err)
		return
	}
}
