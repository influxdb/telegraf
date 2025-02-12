//go:build linux && amd64

package intel_baseband

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestInit(t *testing.T) {
	t.Run("with not specified path values Init should return an error", func(t *testing.T) {
		baseband := prepareBasebandEnvironment()
		require.NotNil(t, baseband)

		err := baseband.Init()

		// check default variables
		// check empty values
		require.Empty(t, baseband.SocketPath)
		require.Empty(t, baseband.FileLogPath)

		// UnreachableSocketBehavior variable should be = unreachableSocketBehaviorError
		require.Equal(t, unreachableSocketBehaviorError, baseband.UnreachableSocketBehavior)
		require.Error(t, err)
		require.ErrorContains(t, err, "path not specified")
	})

	t.Run("with only SocketPath provided the plugin should return the error", func(t *testing.T) {
		baseband := prepareBasebandEnvironment()
		require.NotNil(t, baseband)

		tempSocket := newTempSocket(t)
		defer tempSocket.Close()

		baseband.SocketPath = tempSocket.pathToSocket
		err := baseband.Init()
		require.Error(t, err)
		require.ErrorContains(t, err, "log_file_path")
		require.ErrorContains(t, err, "path not specified")
	})

	t.Run("with SocketAccessTimeout less then 0 provided the plugin should return the error", func(t *testing.T) {
		baseband := prepareBasebandEnvironment()
		require.NotNil(t, baseband)

		baseband.SocketAccessTimeout = -1
		err := baseband.Init()
		require.Error(t, err)
		require.ErrorContains(t, err, "socket_access_timeout should be positive number or equal to 0")
	})

	t.Run("with SocketPath and LogPath provided the plugin shouldn't return any errors", func(t *testing.T) {
		baseband := prepareBasebandEnvironment()
		require.NotNil(t, baseband)

		tempSocket := newTempSocket(t)
		defer tempSocket.Close()

		logTempFile := newTempLogFile(t)
		defer logTempFile.close()

		baseband.SocketPath = tempSocket.pathToSocket
		baseband.FileLogPath = logTempFile.pathToFile
		err := baseband.Init()

		require.NoError(t, err)
	})

	t.Run("with unknown option for UnreachableSocketBehavior plugin should return the error", func(t *testing.T) {
		baseband := prepareBasebandEnvironment()
		require.NotNil(t, baseband)

		baseband.UnreachableSocketBehavior = "UnknownRandomString"
		err := baseband.Init()
		require.Error(t, err)
		require.ErrorContains(t, err, "unreachable_socket_behavior")
	})

	t.Run("with error option for UnreachableSocketBehavior plugin should return error", func(t *testing.T) {
		baseband := prepareBasebandEnvironment()
		require.NotNil(t, baseband)

		baseband.UnreachableSocketBehavior = unreachableSocketBehaviorError
		baseband.SocketPath = "/some/random/path/test.sock"
		baseband.FileLogPath = "/some/random/path/test.log"

		err := baseband.Init()
		require.Error(t, err)
		require.ErrorContains(t, err, "socket_path")
		require.ErrorContains(t, err, "provided path does not exist")
	})

	t.Run("with ignore option for UnreachableSocketBehavior plugin shouldn't return any errors", func(t *testing.T) {
		baseband := prepareBasebandEnvironment()
		require.NotNil(t, baseband)

		baseband.UnreachableSocketBehavior = unreachableSocketBehaviorIgnore
		baseband.SocketPath = "/some/random/path/test.sock"
		baseband.FileLogPath = "/some/random/path/test.log"

		err := baseband.Init()
		require.NoError(t, err)
	})
}

// Test Socket
type tempSocket struct {
	pathToSocket string
	socket       net.Listener

	dirPath string
}

func (ts *tempSocket) Close() {
	var err error
	if err = ts.socket.Close(); err != nil {
		panic(err)
	}

	if err = os.RemoveAll(ts.dirPath); err != nil {
		panic(err)
	}
}

func newTempSocket(t *testing.T) *tempSocket {
	// The Maximum length of the socket path is 104/108 characters, path created with t.TempDir() is too long for some cases
	// (it combines test name with subtest name and some random numbers in the path)
	//nolint:usetesting // "os.MkdirTemp() could be replaced by t.TempDir() in newTempSocket"
	dirPath, err := os.MkdirTemp("", "test-socket")
	require.NoError(t, err)

	pathToSocket := filepath.Join(dirPath, "test"+socketExtension)
	socket, err := net.Listen("unix", pathToSocket)
	require.NoError(t, err)

	return &tempSocket{
		dirPath:      dirPath,
		pathToSocket: pathToSocket,
		socket:       socket,
	}
}

type tempLogFile struct {
	pathToFile string
	file       *os.File
}

func (tlf *tempLogFile) close() {
	var err error
	if err = tlf.file.Close(); err != nil {
		panic(err)
	}

	if err = os.Remove(tlf.pathToFile); err != nil {
		panic(err)
	}
}

func newTempLogFile(t *testing.T) *tempLogFile {
	file, err := os.CreateTemp(t.TempDir(), "*.log")
	require.NoError(t, err)

	return &tempLogFile{
		pathToFile: file.Name(),
		file:       file,
	}
}

func prepareBasebandEnvironment() *Baseband {
	b := newBaseband()
	b.Log = testutil.Logger{Name: "BasebandPluginTest"}
	return b
}
