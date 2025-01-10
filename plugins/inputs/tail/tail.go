//go:generate ../../../tools/readme_config_includer/generator
//go:build !solaris

package tail

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/dimchansky/utfbom"
	"github.com/influxdata/tail"
	"github.com/pborman/ansi"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/globpath"
	"github.com/influxdata/telegraf/plugins/common/encoding"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers"
)

//go:embed sample.conf
var sampleConfig string

var once sync.Once

var (
	offsets      = make(map[string]int64)
	offsetsMutex = new(sync.Mutex)
)

type empty struct{}
type semaphore chan empty

type Tail struct {
	Files               []string `toml:"files"`
	FromBeginning       bool     `toml:"from_beginning"`
	Pipe                bool     `toml:"pipe"`
	WatchMethod         string   `toml:"watch_method"`
	MaxUndeliveredLines int      `toml:"max_undelivered_lines"`
	CharacterEncoding   string   `toml:"character_encoding"`
	PathTag             string   `toml:"path_tag"`

	Filters      []string `toml:"filters"`
	filterColors bool

	Log        telegraf.Logger `toml:"-"`
	tailers    map[string]*tail.Tail
	offsets    map[string]int64
	parserFunc telegraf.ParserFunc
	wg         sync.WaitGroup

	acc telegraf.TrackingAccumulator

	MultilineConfig MultilineConfig `toml:"multiline"`
	multiline       *Multiline

	ctx     context.Context
	cancel  context.CancelFunc
	sem     semaphore
	decoder *encoding.Decoder
}

func NewTail() *Tail {
	offsetsMutex.Lock()
	offsetsCopy := make(map[string]int64, len(offsets))
	for k, v := range offsets {
		offsetsCopy[k] = v
	}
	offsetsMutex.Unlock()

	return &Tail{
		FromBeginning:       false,
		MaxUndeliveredLines: 1000,
		offsets:             offsetsCopy,
		PathTag:             "path",
	}
}

func (*Tail) SampleConfig() string {
	return sampleConfig
}

func (t *Tail) Init() error {
	if t.MaxUndeliveredLines == 0 {
		return errors.New("max_undelivered_lines must be positive")
	}
	t.sem = make(semaphore, t.MaxUndeliveredLines)

	for _, filter := range t.Filters {
		if filter == "ansi_color" {
			t.filterColors = true
		}
	}
	// init offsets
	t.offsets = make(map[string]int64)

	var err error
	t.decoder, err = encoding.NewDecoder(t.CharacterEncoding)
	return err
}

func (t *Tail) GetState() interface{} {
	return t.offsets
}

func (t *Tail) SetState(state interface{}) error {
	offsetsState, ok := state.(map[string]int64)
	if !ok {
		return errors.New("state has to be of type 'map[string]int64'")
	}
	for k, v := range offsetsState {
		t.offsets[k] = v
	}
	return nil
}

func (t *Tail) Gather(_ telegraf.Accumulator) error {
	return t.tailNewFiles(true)
}

func (t *Tail) Start(acc telegraf.Accumulator) error {
	t.acc = acc.WithTracking(t.MaxUndeliveredLines)

	t.ctx, t.cancel = context.WithCancel(context.Background())

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		for {
			select {
			case <-t.ctx.Done():
				return
			case <-t.acc.Delivered():
				<-t.sem
			}
		}
	}()

	var err error
	t.multiline, err = t.MultilineConfig.NewMultiline()

	if err != nil {
		return err
	}

	t.tailers = make(map[string]*tail.Tail)

	err = t.tailNewFiles(t.FromBeginning)

	// assumption that once Start is called, all parallel plugins have already been initialized
	offsetsMutex.Lock()
	offsets = make(map[string]int64)
	offsetsMutex.Unlock()

	return err
}

func (t *Tail) tailNewFiles(fromBeginning bool) error {
	var poll bool
	if t.WatchMethod == "poll" {
		poll = true
	}

	// Create a "tailer" for each file
	for _, filepath := range t.Files {
		g, err := globpath.Compile(filepath)
		if err != nil {
			t.Log.Errorf("Glob %q failed to compile: %s", filepath, err.Error())
		}
		for _, file := range g.Match() {
			if _, ok := t.tailers[file]; ok {
				// we're already tailing this file
				continue
			}

			var seek *tail.SeekInfo
			if !t.Pipe && !fromBeginning {
				if offset, ok := t.offsets[file]; ok {
					t.Log.Debugf("Using offset %d for %q", offset, file)
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
					Pipe:      t.Pipe,
					Logger:    tail.DiscardingLogger,
					OpenReaderFunc: func(rd io.Reader) io.Reader {
						r, _ := utfbom.Skip(t.decoder.Reader(rd))
						return r
					},
				})

			if err != nil {
				t.Log.Debugf("Failed to open file (%s): %v", file, err)
				continue
			}

			t.Log.Debugf("Tail added for %q", file)

			parser, err := t.parserFunc()
			if err != nil {
				t.Log.Errorf("Creating parser: %s", err.Error())
				continue
			}

			// create a goroutine for each "tailer"
			t.wg.Add(1)

			go func() {
				defer t.wg.Done()
				t.receiver(parser, tailer)

				t.Log.Debugf("Tail removed for %q", tailer.Filename)

				if err := tailer.Err(); err != nil {
					if strings.HasSuffix(err.Error(), "permission denied") {
						t.Log.Errorf("Deleting tailer for %q due to: %v", tailer.Filename, err)
						delete(t.tailers, tailer.Filename)
					} else {
						t.Log.Errorf("Tailing %q: %s", tailer.Filename, err.Error())
					}
				}
			}()

			t.tailers[tailer.Filename] = tailer
		}
	}
	return nil
}

// ParseLine parses a line of text.
func parseLine(parser telegraf.Parser, line string) ([]telegraf.Metric, error) {
	m, err := parser.Parse([]byte(line))
	if err != nil {
		if errors.Is(err, parsers.ErrEOF) {
			return nil, nil
		}
		return nil, err
	}
	return m, err
}

// Receiver is launched as a goroutine to continuously watch a tailed logfile
// for changes, parse any incoming msgs, and add to the accumulator.
func (t *Tail) receiver(parser telegraf.Parser, tailer *tail.Tail) {
	// holds the individual lines of multi-line log entries.
	var buffer bytes.Buffer

	var timer *time.Timer
	var timeout <-chan time.Time

	// The multiline mode requires a timer in order to flush the multiline buffer
	// if no new lines are incoming.
	if t.multiline.IsEnabled() {
		timer = time.NewTimer(time.Duration(*t.MultilineConfig.Timeout))
		timeout = timer.C
	}

	channelOpen := true
	tailerOpen := true
	var line *tail.Line

	for {
		line = nil

		if timer != nil {
			timer.Reset(time.Duration(*t.MultilineConfig.Timeout))
		}

		select {
		case <-t.ctx.Done():
			channelOpen = false
		case line, tailerOpen = <-tailer.Lines:
			if !tailerOpen {
				channelOpen = false
			}
		case <-timeout:
		}

		var text string

		if line != nil {
			// Fix up files with Windows line endings.
			text = strings.TrimRight(line.Text, "\r")

			if t.multiline.IsEnabled() {
				if text = t.multiline.ProcessLine(text, &buffer); text == "" {
					continue
				}
			}
		}
		if line == nil || !channelOpen || !tailerOpen {
			if text += Flush(&buffer); text == "" {
				if !channelOpen {
					return
				}

				continue
			}
		}

		if line != nil && line.Err != nil {
			t.Log.Errorf("Tailing %q: %s", tailer.Filename, line.Err.Error())
			continue
		}

		if t.filterColors {
			out, err := ansi.Strip([]byte(text))
			if err != nil {
				t.Log.Errorf("Cannot strip ansi colors from %s: %s", text, err)
			}
			text = string(out)
		}

		metrics, err := parseLine(parser, text)
		if err != nil {
			t.Log.Errorf("Malformed log line in %q: [%q]: %s",
				tailer.Filename, text, err.Error())
			continue
		}
		if len(metrics) == 0 {
			once.Do(func() {
				t.Log.Debug(internal.NoMetricsCreatedMsg)
			})
		}
		if t.PathTag != "" {
			for _, metric := range metrics {
				metric.AddTag(t.PathTag, tailer.Filename)
			}
		}

		// try writing out metric first without blocking
		select {
		case t.sem <- empty{}:
			t.acc.AddTrackingMetricGroup(metrics)
			if t.ctx.Err() != nil {
				return // exit!
			}
			continue // next loop
		default:
			// no room. switch to blocking write.
		}

		// Block until plugin is stopping or room is available to add metrics.
		select {
		case <-t.ctx.Done():
			return
		// Tail is trying to close so drain the sem to allow the receiver
		// to exit. This condition is hit when the tailer may have hit the
		// maximum undelivered lines and is trying to close.
		case <-tailer.Dying():
			<-t.sem
		case t.sem <- empty{}:
			t.acc.AddTrackingMetricGroup(metrics)
		}
	}
}

func (t *Tail) Stop() {
	for _, tailer := range t.tailers {
		if !t.Pipe && !t.FromBeginning {
			// store offset for resume
			offset, err := tailer.Tell()
			if err == nil {
				t.Log.Debugf("Recording offset %d for %q", offset, tailer.Filename)
				t.offsets[tailer.Filename] = offset
			} else {
				t.Log.Errorf("Recording offset for %q: %s", tailer.Filename, err.Error())
			}
		}
		err := tailer.Stop()
		if err != nil {
			t.Log.Errorf("Stopping tail on %q: %s", tailer.Filename, err.Error())
		}
	}

	t.cancel()
	t.wg.Wait()

	// persist offsets
	offsetsMutex.Lock()
	for k, v := range t.offsets {
		offsets[k] = v
	}
	offsetsMutex.Unlock()
}

func (t *Tail) SetParserFunc(fn telegraf.ParserFunc) {
	t.parserFunc = fn
}

func init() {
	inputs.Add("tail", func() telegraf.Input {
		return NewTail()
	})
}
