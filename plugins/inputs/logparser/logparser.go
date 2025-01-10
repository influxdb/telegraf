//go:generate ../../../tools/readme_config_includer/generator
//go:build !solaris

package logparser

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/influxdata/tail"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/globpath"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers/grok"
)

//go:embed sample.conf
var sampleConfig string

var (
	offsets      = make(map[string]int64)
	offsetsMutex = new(sync.Mutex)
)

const (
	defaultWatchMethod = "inotify"
)

type LogParser struct {
	Files         []string        `toml:"files"`
	FromBeginning bool            `toml:"from_beginning"`
	WatchMethod   string          `toml:"watch_method"`
	GrokConfig    grokConfig      `toml:"grok"`
	Log           telegraf.Logger `toml:"-"`

	tailers map[string]*tail.Tail
	offsets map[string]int64
	lines   chan logEntry
	done    chan struct{}
	wg      sync.WaitGroup

	acc telegraf.Accumulator

	sync.Mutex
	grokParser telegraf.Parser
}

type grokConfig struct {
	MeasurementName    string `toml:"measurement"`
	Patterns           []string
	NamedPatterns      []string
	CustomPatterns     string
	CustomPatternFiles []string
	Timezone           string
	UniqueTimestamp    string
}

type logEntry struct {
	path string
	line string
}

func (*LogParser) SampleConfig() string {
	return sampleConfig
}

func (l *LogParser) Init() error {
	l.Log.Warnf(`The logparser plugin is deprecated; please use the 'tail' input with the 'grok' data_format`)
	return nil
}

func (l *LogParser) Start(acc telegraf.Accumulator) error {
	l.Lock()
	defer l.Unlock()

	l.acc = acc
	l.lines = make(chan logEntry, 1000)
	l.done = make(chan struct{})
	l.tailers = make(map[string]*tail.Tail)

	mName := "logparser"
	if l.GrokConfig.MeasurementName != "" {
		mName = l.GrokConfig.MeasurementName
	}

	// Looks for fields which implement LogParser interface
	parser := grok.Parser{
		Measurement:        mName,
		Patterns:           l.GrokConfig.Patterns,
		NamedPatterns:      l.GrokConfig.NamedPatterns,
		CustomPatterns:     l.GrokConfig.CustomPatterns,
		CustomPatternFiles: l.GrokConfig.CustomPatternFiles,
		Timezone:           l.GrokConfig.Timezone,
		UniqueTimestamp:    l.GrokConfig.UniqueTimestamp,
	}
	err := parser.Init()
	if err != nil {
		return err
	}
	l.grokParser = &parser
	models.SetLoggerOnPlugin(l.grokParser, l.Log)

	l.wg.Add(1)
	go l.parser()

	l.tailNewFiles(l.FromBeginning)

	// clear offsets
	l.offsets = make(map[string]int64)
	// assumption that once Start is called, all parallel plugins have already been initialized
	offsetsMutex.Lock()
	offsets = make(map[string]int64)
	offsetsMutex.Unlock()

	return nil
}

func (l *LogParser) Gather(_ telegraf.Accumulator) error {
	l.Lock()
	defer l.Unlock()

	// always start from the beginning of files that appear while we're running
	l.tailNewFiles(true)

	return nil
}

func (l *LogParser) Stop() {
	l.Lock()
	defer l.Unlock()

	for _, t := range l.tailers {
		if !l.FromBeginning {
			// store offset for resume
			offset, err := t.Tell()
			if err == nil {
				l.offsets[t.Filename] = offset
				l.Log.Debugf("Recording offset %d for file: %v", offset, t.Filename)
			} else {
				l.acc.AddError(fmt.Errorf("error recording offset for file %s", t.Filename))
			}
		}
		err := t.Stop()

		// message for a stopped tailer
		l.Log.Debugf("Tail dropped for file: %v", t.Filename)

		if err != nil {
			l.Log.Errorf("Error stopping tail on file %s", t.Filename)
		}
	}
	close(l.done)
	l.wg.Wait()

	// persist offsets
	offsetsMutex.Lock()
	for k, v := range l.offsets {
		offsets[k] = v
	}
	offsetsMutex.Unlock()
}

// check the globs against files on disk, and start tailing any new files.
// Assumes l's lock is held!
func (l *LogParser) tailNewFiles(fromBeginning bool) {
	var poll bool
	if l.WatchMethod == "poll" {
		poll = true
	}

	// Create a "tailer" for each file
	for _, filepath := range l.Files {
		g, err := globpath.Compile(filepath)
		if err != nil {
			l.Log.Errorf("Glob %q failed to compile: %s", filepath, err)
			continue
		}
		files := g.Match()

		for _, file := range files {
			if _, ok := l.tailers[file]; ok {
				// we're already tailing this file
				continue
			}

			var seek *tail.SeekInfo
			if !fromBeginning {
				if offset, ok := l.offsets[file]; ok {
					l.Log.Debugf("Using offset %d for file: %v", offset, file)
					seek = &tail.SeekInfo{
						Whence: 0,
						Offset: offset,
					}
				} else {
					seek = &tail.SeekInfo{
						Whence: 2,
						Offset: 0,
					}
				}
			}

			tailer, err := tail.TailFile(file,
				tail.Config{
					ReOpen:    true,
					Follow:    true,
					Location:  seek,
					MustExist: true,
					Poll:      poll,
					Logger:    tail.DiscardingLogger,
				})
			if err != nil {
				l.acc.AddError(err)
				continue
			}

			l.Log.Debugf("Tail added for file: %v", file)

			// create a goroutine for each "tailer"
			l.wg.Add(1)
			go l.receiver(tailer)
			l.tailers[file] = tailer
		}
	}
}

// receiver is launched as a goroutine to continuously watch a tailed logfile
// for changes and send any log lines down the l.lines channel.
func (l *LogParser) receiver(tailer *tail.Tail) {
	defer l.wg.Done()

	var line *tail.Line
	for line = range tailer.Lines {
		if line.Err != nil {
			l.Log.Errorf("Error tailing file %s, Error: %s",
				tailer.Filename, line.Err)
			continue
		}

		// Fix up files with Windows line endings.
		text := strings.TrimRight(line.Text, "\r")

		entry := logEntry{
			path: tailer.Filename,
			line: text,
		}

		select {
		case <-l.done:
		case l.lines <- entry:
		}
	}
}

// parse is launched as a goroutine to watch the l.lines channel.
// when a line is available, parse parses it and adds the metric(s) to the
// accumulator.
func (l *LogParser) parser() {
	defer l.wg.Done()

	var m telegraf.Metric
	var err error
	var entry logEntry
	for {
		select {
		case <-l.done:
			return
		case entry = <-l.lines:
			if entry.line == "" || entry.line == "\n" {
				continue
			}
		}
		m, err = l.grokParser.ParseLine(entry.line)
		if err == nil {
			if m != nil {
				tags := m.Tags()
				tags["path"] = entry.path
				l.acc.AddFields(m.Name(), m.Fields(), tags, m.Time())
			}
		} else {
			l.Log.Errorf("Error parsing log line: %s", err.Error())
		}
	}
}

func newLogParser() *LogParser {
	offsetsMutex.Lock()
	offsetsCopy := make(map[string]int64, len(offsets))
	for k, v := range offsets {
		offsetsCopy[k] = v
	}
	offsetsMutex.Unlock()

	return &LogParser{
		WatchMethod: defaultWatchMethod,
		offsets:     offsetsCopy,
	}
}

func init() {
	inputs.Add("logparser", func() telegraf.Input {
		return newLogParser()
	})
}
