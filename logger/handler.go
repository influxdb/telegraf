package logger

import (
	"container/list"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
)

type entry struct {
	timestamp  time.Time
	level      telegraf.LogLevel
	prefix     string
	attributes map[string]interface{}
	args       []interface{}
}

type handler struct {
	level    telegraf.LogLevel
	timezone *time.Location

	impl      sink
	earlysink *log.Logger
	earlylogs *list.List
	sync.Mutex
}

func defaultHandler() *handler {
	return &handler{
		level:     telegraf.Info,
		timezone:  time.UTC,
		earlysink: log.New(os.Stderr, "", 0),
		earlylogs: list.New(),
	}
}

func redirectHandler(w io.Writer) *handler {
	return &handler{
		level:     99,
		timezone:  time.UTC,
		impl:      &redirectLogger{writer: w},
		earlysink: log.New(w, "", 0),
		earlylogs: list.New(),
	}
}

func (h *handler) switchSink(impl sink, level telegraf.LogLevel, tz *time.Location, skipEarlyLogs bool) {
	// Setup the new sink etc
	h.impl = impl
	h.level = level
	h.timezone = tz

	// Use the new logger to output the early log-messages
	h.Lock()
	if !skipEarlyLogs && h.earlylogs.Len() > 0 {
		current := h.earlylogs.Front()
		for current != nil {
			e := current.Value.(*entry)
			h.impl.Print(e.level, e.timestamp.In(h.timezone), e.prefix, e.attributes, e.args...)
			next := current.Next()
			h.earlylogs.Remove(current)
			current = next
		}
	}
	h.Unlock()
}

func (h *handler) add(level telegraf.LogLevel, ts time.Time, prefix string, attr map[string]interface{}, args ...interface{}) *entry {
	e := &entry{
		timestamp:  ts,
		level:      level,
		prefix:     prefix,
		attributes: attr,
		args:       args,
	}

	h.Lock()
	h.earlylogs.PushBack(e)
	h.Unlock()

	return e
}

func (h *handler) close() error {
	if h.impl == nil {
		return nil
	}

	h.Lock()
	current := h.earlylogs.Front()
	for current != nil {
		h.earlylogs.Remove(current)
		current = h.earlylogs.Front()
	}
	h.Unlock()

	if l, ok := h.impl.(io.Closer); ok {
		return l.Close()
	}

	return nil
}

// Logger to redirect the logs to an arbitrary writer
type redirectLogger struct {
	writer io.Writer
}

func (l *redirectLogger) Print(level telegraf.LogLevel, ts time.Time, prefix string, attr map[string]interface{}, args ...interface{}) {
	var attrMsg string
	if len(attr) > 0 {
		var parts []string
		for k, v := range attr {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
		attrMsg = "(" + strings.Join(parts, ",") + ")"
	}

	msg := []interface{}{ts.In(time.UTC).Format(time.RFC3339), level.Indicator()}
	if prefix+attrMsg != "" {
		msg = append(msg, prefix+attrMsg)
	}
	msg = append(msg, args...)

	fmt.Fprintln(l.writer, msg...)
}

func (l *redirectLogger) Close() error {
	if l.writer == os.Stderr {
		return nil
	}
	if closer, ok := l.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
