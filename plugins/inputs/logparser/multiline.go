package logparser

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// Indicates relation to the multiline event
type MultilineWhat int

type Multiline struct {
	config        *MultilineConfig
	enabled       bool
	patternRegexp *regexp.Regexp
}

type MultilineConfig struct {
	Pattern string
	What    MultilineWhat
	Negate  bool
}

const (
	// Previous => Append current line to previous line
	Previous MultilineWhat = iota
	// Next => Next line will be appended to current line
	Next
)

func (m *MultilineConfig) NewMultiline() (*Multiline, error) {
	enabled := false
	var r *regexp.Regexp
	var err error

	if m.Pattern != "" {
		enabled = true
		if r, err = regexp.Compile(m.Pattern); err != nil {
			return nil, err
		}
	}

	return &Multiline{
		config:        m,
		enabled:       enabled,
		patternRegexp: r}, nil
}

func (m *Multiline) IsEnabled() bool {
	return m.enabled
}

func (m *Multiline) ProcessLine(text string, buffer *bytes.Buffer) string {
	if m.matchString(text) {
		buffer.WriteString(text)
		return ""
	}
	if m.config.What == Previous {
		previousText := buffer.String()
		buffer.Reset()
		buffer.WriteString(text)
		text = previousText
	} else {
		// Next
		if buffer.Len() > 0 {
			buffer.WriteString(text)
			text = buffer.String()
			buffer.Reset()
		}
	}

	return text
}

func (m *Multiline) matchString(text string) bool {
	return m.patternRegexp.MatchString(text) != m.config.Negate
}

func (w MultilineWhat) String() string {
	switch w {
	case Previous:
		return "previous"
	case Next:
		return "next"
	}
	return ""
}

// UnmarshalTOML implements ability to unmarshal MultilineWhat from TOML files.
func (w *MultilineWhat) UnmarshalTOML(data []byte) (err error) {
	return w.UnmarshalText(data)
}

// UnmarshalText implements encoding.TextUnmarshaler
func (w *MultilineWhat) UnmarshalText(data []byte) (err error) {
	s := string(data)
	switch strings.ToUpper(s) {
	case `PREVIOUS`:
		fallthrough
	case `"PREVIOUS"`:
		fallthrough
	case `'PREVIOUS'`:
		*w = Previous
		return

	case `NEXT`:
		fallthrough
	case `"NEXT"`:
		fallthrough
	case `'NEXT'`:
		*w = Next
		return
	}
	*w = -1
	return fmt.Errorf("unknown multiline what")
}

// MarshalText implements encoding.TextMarshaler
func (w MultilineWhat) MarshalText() ([]byte, error) {
	s := w.String()
	if s != "" {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("unknown multiline what")
}
