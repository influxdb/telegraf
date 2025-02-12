package logger

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/stretchr/testify/require"
)

func TestTextLogger(t *testing.T) {
	tempDir := t.TempDir()
	anotherTempDir := t.TempDir()

	defer closeLogger(t)

	t.Run("TestTextStderr", func(t *testing.T) {
		createDefaultHandler(t)
		cfg := &Config{
			LogFormat: "text",
			Quiet:     true,
		}
		require.NoError(t, SetupLogging(cfg))
		logger, ok := instance.impl.(*textLogger)
		require.Truef(t, ok, "logging instance is not a text-logger but %T", instance.impl)
		require.Equal(t, logger.logger.Writer(), os.Stderr)
	})

	t.Run("TestTextFile", func(t *testing.T) {
		tmpFile := filepath.Join(tempDir, "TestTextFile.log")

		cfg := &Config{
			Logfile:             tmpFile,
			LogFormat:           "text",
			RotationMaxArchives: -1,
		}
		require.NoError(t, SetupLogging(cfg))

		log.Printf("I! TEST")
		log.Printf("D! TEST") // <- should be ignored

		buf, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		require.Greater(t, len(buf), 19)
		require.Equal(t, "Z I! TEST\n", string(buf[19:]))
	})

	t.Run("TestTextFileDebug", func(t *testing.T) {
		tmpFile := filepath.Join(tempDir, "TestTextFileDebug.log")

		cfg := &Config{
			Logfile:             tmpFile,
			LogFormat:           "text",
			RotationMaxArchives: -1,
			Debug:               true,
		}
		require.NoError(t, SetupLogging(cfg))

		log.Printf("D! TEST")

		buf, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		require.Greater(t, len(buf), 19)
		require.Equal(t, "Z D! TEST\n", string(buf[19:]))
	})

	t.Run("TestTextFileError", func(t *testing.T) {
		tmpFile := filepath.Join(tempDir, "TestTextFileError.log")

		cfg := &Config{
			Logfile:             tmpFile,
			LogFormat:           "text",
			RotationMaxArchives: -1,
			Quiet:               true,
		}
		require.NoError(t, SetupLogging(cfg))

		log.Printf("E! TEST")
		log.Printf("I! TEST") // <- should be ignored

		buf, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		require.Greater(t, len(buf), 19)
		require.Equal(t, "Z E! TEST\n", string(buf[19:]))
	})

	t.Run("TestTextAddDefaultLogLevel", func(t *testing.T) {
		tmpFile := filepath.Join(tempDir, "TestTextAddDefaultLogLevel.log")

		cfg := &Config{
			Logfile:             tmpFile,
			LogFormat:           "text",
			RotationMaxArchives: -1,
			Debug:               true,
		}
		require.NoError(t, SetupLogging(cfg))

		log.Printf("TEST")

		buf, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		require.Greater(t, len(buf), 19)
		require.Equal(t, "Z I! TEST\n", string(buf[19:]))
	})

	t.Run("TestTextWriteToTruncatedFile", func(t *testing.T) {
		tmpFile := filepath.Join(tempDir, "TestTextWriteToTruncatedFile.log")

		cfg := &Config{
			Logfile:             tmpFile,
			LogFormat:           "text",
			RotationMaxArchives: -1,
			Debug:               true,
		}
		require.NoError(t, SetupLogging(cfg))

		log.Printf("TEST")

		buf, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		require.Greater(t, len(buf), 19)
		require.Equal(t, "Z I! TEST\n", string(buf[19:]))

		require.NoError(t, os.Truncate(tmpFile, 0))

		log.Printf("SHOULD BE FIRST")

		buf, err = os.ReadFile(tmpFile)
		require.NoError(t, err)
		require.Equal(t, "Z I! SHOULD BE FIRST\n", string(buf[19:]))
	})

	t.Run("TestTextWriteToFileInRotation", func(t *testing.T) {
		cfg := &Config{
			Logfile:             filepath.Join(anotherTempDir, "TestTextWriteToFileInRotation.log"),
			LogFormat:           "text",
			RotationMaxArchives: -1,
			RotationMaxSize:     30,
		}
		require.NoError(t, SetupLogging(cfg))

		log.Printf("I! TEST 1") // Writes 31 bytes, will rotate
		log.Printf("I! TEST")   // Writes 29 byes, no rotation expected

		files, err := os.ReadDir(anotherTempDir)
		require.NoError(t, err)
		require.Len(t, files, 2)
	})

	t.Run("TestTextWriteDerivedLogger", func(t *testing.T) {
		createDefaultHandler(t)
		tmpFile := filepath.Join(tempDir, "TestTextWriteDerivedLogger.log")

		cfg := &Config{
			Logfile:             tmpFile,
			LogFormat:           "text",
			RotationMaxArchives: -1,
			Debug:               true,
		}
		require.NoError(t, SetupLogging(cfg))

		l := New("testing", "test", "")
		l.Info("TEST")

		buf, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		require.Greater(t, len(buf), 19)
		require.Equal(t, "Z I! [testing.test] TEST\n", string(buf[19:]))
	})

	t.Run("TestTextWriteDerivedLoggerWithAttributes", func(t *testing.T) {
		createDefaultHandler(t)
		tmpFile := filepath.Join(tempDir, "TestTextWriteDerivedLoggerWithAttributes.log")

		cfg := &Config{
			Logfile:             tmpFile,
			LogFormat:           "text",
			RotationMaxArchives: -1,
			Debug:               true,
		}
		require.NoError(t, SetupLogging(cfg))

		l := New("testing", "test", "myalias")

		// All attributes should be ignored
		l.AddAttribute("alias", "foo")
		l.AddAttribute("device_id", 123)

		l.Info("TEST")

		buf, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		require.Greater(t, len(buf), 19)
		require.Equal(t, "Z I! [testing.test::myalias] TEST\n", string(buf[19:]))
	})
}

func BenchmarkTelegrafTextLogWrite(b *testing.B) {
	l, err := createTextLogger(&Config{})
	require.NoError(b, err)

	// Discard all logging output
	dl := l.(*textLogger)
	dl.logger.SetOutput(io.Discard)

	ts := time.Now()
	for i := 0; i < b.N; i++ {
		dl.Print(telegraf.Debug, ts, "", nil, "test")
	}
}
