package kinesis_consumer

import (
	"github.com/aws/smithy-go/logging"

	"github.com/influxdata/telegraf"
)

type telegrafLoggerWrapper struct {
	telegraf.Logger
}

func (t *telegrafLoggerWrapper) Log(args ...interface{}) {
	t.Trace(args...)
}

func (t *telegrafLoggerWrapper) Logf(classification logging.Classification, format string, v ...interface{}) {
	switch classification {
	case logging.Debug:
		format = "DEBUG " + format
	case logging.Warn:
		format = "WARN" + format
	default:
		format = "INFO " + format
	}
	t.Logger.Tracef(format, v...)
}
