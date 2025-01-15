package mavlink

import (
	"testing"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/stretchr/testify/require"
)

// Test that a serial port URL can be parsed (Linux)
func TestParseSerialUrlLinux(t *testing.T) {
	testConfig := Mavlink{
		URL: "serial:///dev/ttyACM0:115200",
	}

	err := testConfig.Init()
	require.NoError(t, err)
	endpoint, ok := testConfig.endpointConfig[0].(gomavlib.EndpointSerial)
	require.True(t, ok)
	require.Equal(t, "/dev/ttyACM0", endpoint.Device)
	require.Equal(t, 115200, endpoint.Baud)
}

// Test that a serial port URL can be parsed (Windows)
func TestParseSerialUrlWindows(t *testing.T) {
	testConfig := Mavlink{
		URL: "serial://COM1:115200",
	}

	err := testConfig.Init()
	require.NoError(t, err)
	endpoint, ok := testConfig.endpointConfig[0].(gomavlib.EndpointSerial)
	require.True(t, ok)
	require.Equal(t, "COM1", endpoint.Device)
	require.Equal(t, 115200, endpoint.Baud)
}

// Test that a UDP client URL can be parsed.
func TestParseUDPClientUrl(t *testing.T) {
	testConfig := Mavlink{
		URL: "udp://192.168.1.12:14550",
	}

	err := testConfig.Init()
	require.NoError(t, err)
	endpoint, ok := testConfig.endpointConfig[0].(gomavlib.EndpointUDPClient)
	require.True(t, ok)
	require.Equal(t, "192.168.1.12:14550", endpoint.Address)
}

// Test that a UDP server URL can be parsed.
func TestParseUDPServerUrl(t *testing.T) {
	testConfig := Mavlink{
		URL: "udp://:14540",
	}

	err := testConfig.Init()
	require.NoError(t, err)
	endpoint, ok := testConfig.endpointConfig[0].(gomavlib.EndpointUDPServer)
	require.True(t, ok)
	require.Equal(t, ":14540", endpoint.Address)
}

// Test that a TCP client URL can be parsed.
func TestParseTCPClientUrl(t *testing.T) {
	testConfig := Mavlink{
		URL: "tcp://192.168.1.12:14550",
	}

	err := testConfig.Init()
	require.NoError(t, err)
	endpoint, ok := testConfig.endpointConfig[0].(gomavlib.EndpointTCPClient)
	require.True(t, ok)
	require.Equal(t, "192.168.1.12:14550", endpoint.Address)
}

// Test that an invalid URL is caught.
func TestParseInvalidUrl(t *testing.T) {
	testConfig := Mavlink{
		URL: "ftp://not-a-valid-fcu-url",
	}

	err := testConfig.Init()
	require.ErrorContains(t, err, "could not parse url")
}
