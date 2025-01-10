package logger

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
)

// Central handler for the logs used by the logger to actually output the logs.
// This is necessary to be able to dynamically switch the sink even though
// plugins already instantiated a logger _before_ the final sink is set up.
var (
	instance *handler  // handler for the actual output
	once     sync.Once // once token to initialize the handler only once
)

// sink interface that has to be implemented by a logging sink
type sink interface {
	Print(telegraf.LogLevel, time.Time, string, map[string]interface{}, ...interface{})
}

// logger is the actual implementation of the telegraf logger interface
type logger struct {
	level    *telegraf.LogLevel
	category string
	name     string
	alias    string

	prefix     string
	onError    []func()
	attributes map[string]interface{}
}

// New creates a new logging instance to be used in models
func New(category, name, alias string) *logger {
	l := &logger{
		category:   category,
		name:       name,
		alias:      alias,
		attributes: map[string]interface{}{"category": category, "plugin": name},
	}
	if alias != "" {
		l.attributes["alias"] = alias
	}

	// Format the prefix
	l.prefix = l.category

	if l.prefix != "" && l.name != "" {
		l.prefix += "."
	}
	l.prefix += l.name

	if l.prefix != "" && l.alias != "" {
		l.prefix += "::"
	}
	l.prefix += l.alias

	if l.prefix != "" {
		l.prefix = "[" + l.prefix + "] "
	}

	return l
}

// Level returns the current log-level of the logger
func (l *logger) Level() telegraf.LogLevel {
	if l.level != nil {
		return *l.level
	}
	return instance.level
}

// AddAttribute allows to add a key-value attribute to the logging output
func (l *logger) AddAttribute(key string, value interface{}) {
	// Do not allow to overwrite general keys
	switch key {
	case "category", "plugin", "alias":
	default:
		l.attributes[key] = value
	}
}

// Error logging including callbacks
func (l *logger) Errorf(format string, args ...interface{}) {
	l.Error(fmt.Sprintf(format, args...))
}

func (l *logger) Error(args ...interface{}) {
	l.Print(telegraf.Error, time.Now(), args...)
	for _, f := range l.onError {
		f()
	}
}

// Warning logging
func (l *logger) Warnf(format string, args ...interface{}) {
	l.Warn(fmt.Sprintf(format, args...))
}

func (l *logger) Warn(args ...interface{}) {
	l.Print(telegraf.Warn, time.Now(), args...)
}

// Info logging
func (l *logger) Infof(format string, args ...interface{}) {
	l.Info(fmt.Sprintf(format, args...))
}

func (l *logger) Info(args ...interface{}) {
	l.Print(telegraf.Info, time.Now(), args...)
}

// Debug logging, this is suppressed on console
func (l *logger) Debugf(format string, args ...interface{}) {
	l.Debug(fmt.Sprintf(format, args...))
}

func (l *logger) Debug(args ...interface{}) {
	l.Print(telegraf.Debug, time.Now(), args...)
}

// Trace logging, this is suppressed on console
func (l *logger) Tracef(format string, args ...interface{}) {
	l.Trace(fmt.Sprintf(format, args...))
}

func (l *logger) Trace(args ...interface{}) {
	l.Print(telegraf.Trace, time.Now(), args...)
}

func (l *logger) Print(level telegraf.LogLevel, ts time.Time, args ...interface{}) {
	// Check if we are in early logging state and store the message in this case
	if instance.impl == nil {
		instance.add(level, ts, l.prefix, l.attributes, args...)
	}

	// Skip all messages with insufficient log-levels
	if l.level != nil && !l.level.Includes(level) || l.level == nil && !instance.level.Includes(level) {
		return
	}
	if instance.impl != nil {
		instance.impl.Print(level, ts.In(instance.timezone), l.prefix, l.attributes, args...)
	} else {
		msg := append([]interface{}{ts.In(instance.timezone).Format(time.RFC3339), " ", level.Indicator(), " ", l.prefix}, args...)
		instance.earlysink.Print(msg...)
	}
}

// SetLevel overrides the current log-level of the logger
func (l *logger) SetLevel(level telegraf.LogLevel) {
	l.level = &level
}

// SetLevel changes the log-level to the given one
func (l *logger) SetLogLevel(name string) error {
	if name == "" {
		return nil
	}
	level := telegraf.LogLevelFromString(name)
	if level == telegraf.None {
		return fmt.Errorf("invalid log-level %q", name)
	}
	l.SetLevel(level)
	return nil
}

// Register a callback triggered when errors are about to be written to the log
func (l *logger) RegisterErrorCallback(f func()) {
	l.onError = append(l.onError, f)
}

type Config struct {
	// will set the log level to DEBUG
	Debug bool
	// will set the log level to ERROR
	Quiet bool
	// format and target of log messages
	LogTarget string
	LogFormat string
	Logfile   string
	// will rotate when current file at the specified time interval
	RotationInterval time.Duration
	// will rotate when current file size exceeds this parameter.
	RotationMaxSize int64
	// maximum rotated files to keep (older ones will be deleted)
	RotationMaxArchives int
	// pick a timezone to use when logging. or type 'local' for local time.
	LogWithTimezone string
	// Logger instance name
	InstanceName string
	// Structured logging message key
	StructuredLogMessageKey string

	// internal  log-level
	logLevel telegraf.LogLevel
}

// SetupLogging configures the logging output.
func SetupLogging(cfg *Config) error {
	// Issue deprecation warning for option
	switch cfg.LogTarget {
	case "":
		// Best-case no target set or file already migrated...
	case "stderr":
		msg := "Agent setting %q is deprecated, please leave %q empty and remove this setting!"
		deprecation := "The setting will be removed in v1.40.0."
		log.Printf("W! "+msg+" "+deprecation, "logtarget", "logfile")
		cfg.Logfile = ""
	case "file":
		msg := "Agent setting %q is deprecated, please just set %q and remove this setting!"
		deprecation := "The setting will be removed in v1.40.0."
		log.Printf("W! "+msg+" "+deprecation, "logtarget", "logfile")
	case "eventlog":
		msg := "Agent setting %q is deprecated, please set %q to %q and remove this setting!"
		deprecation := "The setting will be removed in v1.40.0."
		log.Printf("W! "+msg+" "+deprecation, "logtarget", "logformat", "eventlog")
		if cfg.LogFormat != "" && cfg.LogFormat != "eventlog" {
			return errors.New("contradicting setting between 'logtarget' and 'logformat'")
		}
		cfg.LogFormat = "eventlog"
	default:
		return fmt.Errorf("invalid deprecated 'logtarget' setting %q", cfg.LogTarget)
	}

	if cfg.LogFormat == "" {
		cfg.LogFormat = "text"
	}

	if cfg.Debug {
		cfg.logLevel = telegraf.Debug
	}
	if cfg.Quiet {
		cfg.logLevel = telegraf.Error
	}
	if !cfg.Debug && !cfg.Quiet {
		cfg.logLevel = telegraf.Info
	}

	if cfg.InstanceName == "" {
		cfg.InstanceName = "telegraf"
	}

	if cfg.LogFormat == "" {
		cfg.LogFormat = "text"
	}

	// Get configured timezone
	timezoneName := cfg.LogWithTimezone
	if strings.EqualFold(timezoneName, "local") {
		timezoneName = "Local"
	}
	tz, err := time.LoadLocation(timezoneName)
	if err != nil {
		return fmt.Errorf("setting logging timezone failed: %w", err)
	}

	// Get the logging factory and create the root instance
	creator, found := registry[cfg.LogFormat]
	if !found {
		return fmt.Errorf("unsupported log-format: %s", cfg.LogFormat)
	}

	l, err := creator(cfg)
	if err != nil {
		return err
	}

	// Close the previous logger if possible
	if err := CloseLogging(); err != nil {
		return err
	}

	// Update the logging instance
	skipEarlyLogs := cfg.LogFormat == "text" && cfg.Logfile == ""
	instance.switchSink(l, cfg.logLevel, tz, skipEarlyLogs)

	return nil
}

func RedirectLogging(w io.Writer) {
	instance = redirectHandler(w)
}

func CloseLogging() error {
	return instance.close()
}

func init() {
	once.Do(func() {
		// Create a special logging instance that additionally buffers all
		// messages logged before the final logger is up.
		instance = defaultHandler()

		// Redirect the standard logger output to our logger instance
		log.SetFlags(0)
		log.SetOutput(&stdlogRedirector{})
	})
}
