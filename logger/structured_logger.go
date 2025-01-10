package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/rotate"
)

type structuredLogger struct {
	handler slog.Handler
	output  io.Writer

	errlog *log.Logger
}

func (l *structuredLogger) Close() error {
	if closer, ok := l.output.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (l *structuredLogger) Print(level telegraf.LogLevel, ts time.Time, _ string, attr map[string]interface{}, args ...interface{}) {
	record := slog.Record{
		Time:    ts,
		Message: fmt.Sprint(args...),
		Level:   slog.Level(level),
	}
	for k, v := range attr {
		record.Add(k, v)
	}
	if err := l.handler.Handle(context.Background(), record); err != nil {
		l.errlog.Printf("E! Writing log message failed: %v", err)
	}
}

var defaultReplaceAttr = func(_ []string, attr slog.Attr) slog.Attr {
	// Translate the Telegraf log-levels to strings
	if attr.Key == slog.LevelKey {
		if level, ok := attr.Value.Any().(slog.Level); ok {
			attr.Value = slog.StringValue(telegraf.LogLevel(level).String())
		}
	}
	return attr
}

var defaultStructuredHandlerOptions = &slog.HandlerOptions{
	Level:       slog.Level(-99),
	ReplaceAttr: defaultReplaceAttr,
}

func init() {
	add("structured", func(cfg *Config) (sink, error) {
		var writer io.Writer = os.Stderr
		if cfg.Logfile != "" {
			w, err := rotate.NewFileWriter(
				cfg.Logfile,
				cfg.RotationInterval,
				cfg.RotationMaxSize,
				cfg.RotationMaxArchives,
			)
			if err != nil {
				return nil, err
			}
			writer = w
		}

		structuredHandlerOptions := defaultStructuredHandlerOptions

		if cfg.StructuredLogMessageKey != "" {
			structuredHandlerOptions.ReplaceAttr = func(groups []string, attr slog.Attr) slog.Attr {
				if attr.Key == slog.MessageKey {
					attr.Key = cfg.StructuredLogMessageKey
				}

				return defaultReplaceAttr(groups, attr)
			}
		}

		return &structuredLogger{
			handler: slog.NewJSONHandler(writer, structuredHandlerOptions),
			output:  writer,
			errlog:  log.New(os.Stderr, "", 0),
		}, nil
	})
}
