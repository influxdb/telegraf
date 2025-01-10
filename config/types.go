package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/units"
)

// Regexp for day specifications in durations
var durationDayRe = regexp.MustCompile(`(\d+(?:\.\d+)?)d`)

// Duration is a time.Duration
type Duration time.Duration

// Size is an int64
type Size int64

// UnmarshalText parses the duration from the Text config file
func (d *Duration) UnmarshalText(b []byte) error {
	// convert to string
	durStr := string(b)

	// Value is a TOML number (e.g. 3, 10, 3.5)
	// First try parsing as integer seconds
	sI, err := strconv.ParseInt(durStr, 10, 64)
	if err == nil {
		dur := time.Second * time.Duration(sI)
		*d = Duration(dur)
		return nil
	}
	// Second try parsing as float seconds
	sF, err := strconv.ParseFloat(durStr, 64)
	if err == nil {
		dur := float64(time.Second) * sF
		*d = Duration(dur)
		return nil
	}

	// Finally, try value is a TOML string (e.g. "3s", 3s) or literal (e.g. '3s')
	if durStr == "" {
		*d = Duration(0)
		return nil
	}

	// Handle "day" intervals and replace them with the "hours" equivalent
	for _, m := range durationDayRe.FindAllStringSubmatch(durStr, -1) {
		days, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return fmt.Errorf("converting %q to hours failed: %w", durStr, err)
		}
		hours := strconv.FormatFloat(days*24, 'f', -1, 64) + "h"
		durStr = strings.Replace(durStr, m[0], hours, 1)
	}

	dur, err := time.ParseDuration(durStr)
	if err != nil {
		return err
	}

	*d = Duration(dur)
	return nil
}

func (s *Size) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		return nil
	}

	str := string(b)
	val, err := strconv.ParseInt(str, 10, 64)
	if err == nil {
		*s = Size(val)
		return nil
	}
	val, err = units.ParseStrictBytes(str)
	if err != nil {
		return err
	}
	*s = Size(val)
	return nil
}
