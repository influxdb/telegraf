//go:generate ../../../tools/readme_config_includer/generator
package file

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/rotate"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

type File struct {
	Files                []string        `toml:"files"`
	RotationInterval     config.Duration `toml:"rotation_interval"`
	RotationMaxSize      config.Size     `toml:"rotation_max_size"`
	RotationMaxArchives  int             `toml:"rotation_max_archives"`
	UseBatchFormat       bool            `toml:"use_batch_format"`
	CompressionAlgorithm string          `toml:"compression_algorithm"`
	CompressionLevel     int             `toml:"compression_level"`
	Log                  telegraf.Logger `toml:"-"`

	encoder    internal.ContentEncoder
	writer     io.Writer
	closers    []io.Closer
	serializer telegraf.Serializer
}

func (*File) SampleConfig() string {
	return sampleConfig
}

func (f *File) SetSerializer(serializer telegraf.Serializer) {
	f.serializer = serializer
}

func (f *File) Init() error {
	var err error
	if len(f.Files) == 0 {
		f.Files = []string{"stdout"}
	}

	var options []internal.EncodingOption
	if f.CompressionAlgorithm == "" {
		f.CompressionAlgorithm = "identity"
	}

	if f.CompressionLevel >= 0 {
		options = append(options, internal.WithCompressionLevel(f.CompressionLevel))
	}
	f.encoder, err = internal.NewContentEncoder(f.CompressionAlgorithm, options...)

	return err
}

func (f *File) Connect() error {
	var writers []io.Writer

	for _, file := range f.Files {
		if file == "stdout" {
			writers = append(writers, os.Stdout)
		} else {
			of, err := rotate.NewFileWriter(
				file, time.Duration(f.RotationInterval), int64(f.RotationMaxSize), f.RotationMaxArchives)
			if err != nil {
				return err
			}

			writers = append(writers, of)
			f.closers = append(f.closers, of)
		}
	}
	f.writer = io.MultiWriter(writers...)
	return nil
}

func (f *File) Close() error {
	var err error
	for _, c := range f.closers {
		errClose := c.Close()
		if errClose != nil {
			err = errClose
		}
	}
	return err
}

func (f *File) Write(metrics []telegraf.Metric) error {
	var writeErr error

	if f.UseBatchFormat {
		octets, err := f.serializer.SerializeBatch(metrics)
		if err != nil {
			f.Log.Errorf("Could not serialize metric: %v", err)
		}

		octets, err = f.encoder.Encode(octets)
		if err != nil {
			f.Log.Errorf("Could not compress metrics: %v", err)
		}

		_, err = f.writer.Write(octets)
		if err != nil {
			f.Log.Errorf("Error writing to file: %v", err)
		}
	} else {
		for _, metric := range metrics {
			b, err := f.serializer.Serialize(metric)
			if err != nil {
				f.Log.Debugf("Could not serialize metric: %v", err)
			}

			b, err = f.encoder.Encode(b)
			if err != nil {
				f.Log.Errorf("Could not compress metrics: %v", err)
			}

			_, err = f.writer.Write(b)
			if err != nil {
				writeErr = fmt.Errorf("failed to write message: %w", err)
			}
		}
	}

	return writeErr
}

func init() {
	outputs.Add("file", func() telegraf.Output {
		return &File{
			CompressionLevel: -1,
		}
	})
}
