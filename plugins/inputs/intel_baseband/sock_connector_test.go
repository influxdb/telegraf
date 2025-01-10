//go:build linux && amd64

package intel_baseband

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/plugins/inputs/intel_baseband/mocks"
)

func TestWriteCommandToSocket(t *testing.T) {
	t.Run("correct execution of the function", func(t *testing.T) {
		conn := &mocks.Conn{}
		conn.On("Write", mock.Anything).Return(2, nil)
		conn.On("SetWriteDeadline", mock.Anything).Return(nil)
		connector := socketConnector{connection: conn}

		err := connector.writeCommandToSocket(0x00)
		require.NoError(t, err)

		defer conn.AssertExpectations(t)
	})

	t.Run("without setting up a connection it should return an error", func(t *testing.T) {
		connector := socketConnector{}

		err := connector.writeCommandToSocket(0x00)
		require.Error(t, err)
		require.ErrorContains(t, err, "connection had not been established before")
	})

	t.Run("handling timeout setting error", func(t *testing.T) {
		conn := &mocks.Conn{}
		conn.On("SetWriteDeadline", mock.Anything).Return(errors.New("deadline set error"))
		connector := socketConnector{connection: conn}

		err := connector.writeCommandToSocket(0x00)

		require.Error(t, err)
		require.ErrorContains(t, err, "failed to set timeout for request")
		require.ErrorContains(t, err, "deadline set error")
		defer conn.AssertExpectations(t)
	})

	t.Run("handling net.Write error", func(t *testing.T) {
		var unsupportedCommand byte = 0x99
		conn := &mocks.Conn{}
		conn.On("Write", []byte{unsupportedCommand, 0x00}).Return(0, errors.New("unsupported command"))
		conn.On("SetWriteDeadline", mock.Anything).Return(nil)
		connector := socketConnector{connection: conn}

		err := connector.writeCommandToSocket(unsupportedCommand)

		require.Error(t, err)
		require.ErrorContains(t, err, "failed to send request to socket")
		require.ErrorContains(t, err, "unsupported command")
		defer conn.AssertExpectations(t)
	})
}

func TestDumpTelemetryToLog(t *testing.T) {
	t.Run("with correct temporary socket should return only an error related to the inability to refresh telemetry", func(t *testing.T) {
		tempSocket := newTempSocket(t)
		defer tempSocket.Close()
		tempLogFile := newTempLogFile(t)
		defer tempLogFile.close()
		connector := newSocketConnector(tempSocket.pathToSocket, 5*time.Second)

		err := connector.dumpTelemetryToLog()
		require.NoError(t, err)
	})
}
