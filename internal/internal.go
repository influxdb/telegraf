package internal

import (
	"bufio"
	"compress/gzip"
	"context"
	crypto_rand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/influxdata/telegraf/internal/choice"
)

const alphanum string = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const NoMetricsCreatedMsg = "No metrics were created from a message. Verify your parser settings. This message is only printed once."

var once sync.Once

var (
	ErrTimeout        = errors.New("command timed out")
	ErrNotImplemented = errors.New("not implemented yet")
)

// Set via LDFLAGS -X
var (
	Version = "unknown"
	Branch  = ""
	Commit  = ""
)

type ReadWaitCloser struct {
	pipeReader *io.PipeReader
	wg         sync.WaitGroup
}

func FormatFullVersion() string {
	var parts = []string{"Telegraf"}

	if Version != "" {
		parts = append(parts, Version)
	} else {
		parts = append(parts, "unknown")
	}

	if Branch != "" || Commit != "" {
		if Branch == "" {
			Branch = "unknown"
		}
		if Commit == "" {
			Commit = "unknown"
		}
		git := fmt.Sprintf("(git: %s@%s)", Branch, Commit)
		parts = append(parts, git)
	}

	return strings.Join(parts, " ")
}

// ProductToken returns a tag for Telegraf that can be used in user agents.
func ProductToken() string {
	return fmt.Sprintf("Telegraf/%s Go/%s",
		Version, strings.TrimPrefix(runtime.Version(), "go"))
}

// ReadLines reads contents from a file and splits them by new lines.
func ReadLines(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return []string{""}, err
	}
	defer f.Close()

	var ret []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		ret = append(ret, scanner.Text())
	}

	return ret, nil
}

// RandomString returns a random string of alphanumeric characters
func RandomString(n int) (string, error) {
	var bytes = make([]byte, n)
	_, err := crypto_rand.Read(bytes)
	if err != nil {
		return "", err
	}
	for i, b := range bytes {
		bytes[i] = alphanum[b%byte(len(alphanum))]
	}
	return string(bytes), nil
}

// SnakeCase converts the given string to snake case following the Golang format:
// acronyms are converted to lower-case and preceded by an underscore.
func SnakeCase(in string) string {
	runes := []rune(in)
	length := len(runes)

	var out []rune
	for i := 0; i < length; i++ {
		if i > 0 && unicode.IsUpper(runes[i]) && ((i+1 < length && unicode.IsLower(runes[i+1])) || unicode.IsLower(runes[i-1])) {
			out = append(out, '_')
		}
		out = append(out, unicode.ToLower(runes[i]))
	}

	return string(out)
}

// RandomSleep will sleep for a random amount of time up to max.
// If the shutdown channel is closed, it will return before it has finished sleeping.
func RandomSleep(limit time.Duration, shutdown chan struct{}) {
	sleepDuration := RandomDuration(limit)
	if sleepDuration == 0 {
		return
	}

	t := time.NewTimer(time.Nanosecond * sleepDuration)
	select {
	case <-t.C:
		return
	case <-shutdown:
		t.Stop()
		return
	}
}

// RandomDuration returns a random duration between 0 and max.
func RandomDuration(limit time.Duration) time.Duration {
	if limit == 0 {
		return 0
	}

	return time.Duration(rand.Int63n(limit.Nanoseconds())) //nolint:gosec // G404: not security critical
}

// SleepContext sleeps until the context is closed or the duration is reached.
func SleepContext(ctx context.Context, duration time.Duration) error {
	if duration == 0 {
		return nil
	}

	t := time.NewTimer(duration)
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		t.Stop()
		return ctx.Err()
	}
}

// AlignDuration returns the duration until next aligned interval.
// If the current time is aligned a 0 duration is returned.
func AlignDuration(tm time.Time, interval time.Duration) time.Duration {
	return AlignTime(tm, interval).Sub(tm)
}

// AlignTime returns the time of the next aligned interval.
// If the current time is aligned the current time is returned.
func AlignTime(tm time.Time, interval time.Duration) time.Time {
	truncated := tm.Truncate(interval)
	if truncated.Equal(tm) {
		return tm
	}
	return truncated.Add(interval)
}

// ExitStatus takes the error from exec.Command
// and returns the exit status and true
// if error is not exit status, will return 0 and false
func ExitStatus(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus(), true
		}
	}
	return 0, false
}

func (r *ReadWaitCloser) Close() error {
	err := r.pipeReader.Close()
	r.wg.Wait() // wait for the gzip goroutine finish
	return err
}

// CompressWithGzip takes an io.Reader as input and pipes it through a
// gzip.Writer returning an io.Reader containing the gzipped data.
// Errors occurring during compression are returned to the instance reading
// from the returned reader via through the corresponding read call
// (e.g. io.Copy or io.ReadAll).
func CompressWithGzip(data io.Reader) io.ReadCloser {
	pipeReader, pipeWriter := io.Pipe()
	gzipWriter := gzip.NewWriter(pipeWriter)

	// Start copying from the uncompressed reader to the output reader
	// in the background until the input reader is closed (or errors out).
	go func() {
		// This copy will block until "data" reached EOF or an error occurs
		_, err := io.Copy(gzipWriter, data)

		// Close the compression writer and make sure we do not overwrite
		// the copy error if any.
		gzipErr := gzipWriter.Close()
		if err == nil {
			err = gzipErr
		}

		// Subsequent reads from the output reader (connected to "pipeWriter"
		// via pipe) will return the copy (or closing) error if any to the
		// instance reading from the reader returned by the CompressWithGzip
		// function. If "err" is nil, the below function will correctly report
		// io.EOF.
		pipeWriter.CloseWithError(err)
	}()

	// Return a reader which then can be read by the caller to collect the
	// compressed stream.
	return pipeReader
}

// ParseTimestamp parses a Time according to the standard Telegraf options.
// These are generally displayed in the toml similar to:
//
//	json_time_key= "timestamp"
//	json_time_format = "2006-01-02T15:04:05Z07:00"
//	json_timezone = "America/Los_Angeles"
//
// The format can be one of "unix", "unix_ms", "unix_us", "unix_ns", or a Go
// time layout suitable for time.Parse.
//
// When using the "unix" format, an optional fractional component is allowed.
// Specific unix time precisions cannot have a fractional component.
//
// Unix times may be an int64, float64, or string.  When using a Go format
// string the timestamp must be a string.
//
// The location is a location string suitable for time.LoadLocation.  Unix
// times do not use the location string, a unix time is always return in the
// UTC location.
func ParseTimestamp(format string, timestamp interface{}, location *time.Location, separator ...string) (time.Time, error) {
	switch format {
	case "unix", "unix_ms", "unix_us", "unix_ns":
		sep := []string{",", "."}
		if len(separator) > 0 {
			sep = separator
		}
		return parseUnix(format, timestamp, sep)
	default:
		v, ok := timestamp.(string)
		if !ok {
			return time.Unix(0, 0), errors.New("unsupported type")
		}
		return parseTime(format, v, location)
	}
}

// parseTime parses a timestamp in unix format with different resolutions
func parseUnix(format string, timestamp interface{}, separator []string) (time.Time, error) {
	// Extract the scaling factor to nanoseconds from "format"
	var factor int64
	switch format {
	case "unix":
		factor = int64(time.Second)
	case "unix_ms":
		factor = int64(time.Millisecond)
	case "unix_us":
		factor = int64(time.Microsecond)
	case "unix_ns":
		factor = int64(time.Nanosecond)
	}

	zero := time.Unix(0, 0)

	// Convert the representation to time
	switch v := timestamp.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		t, err := ToInt64(v)
		if err != nil {
			return zero, err
		}
		return time.Unix(0, t*factor).UTC(), nil
	case float32, float64:
		ts, err := ToFloat64(v)
		if err != nil {
			return zero, err
		}

		// Parse the float as a precise fraction to avoid precision loss
		f := big.Rat{}
		if f.SetFloat64(ts) == nil {
			return zero, errors.New("invalid number")
		}
		return timeFromFraction(&f, factor), nil
	case string:
		// Sanitize the string to have no thousand separators and dot
		// as decimal separator to ease later parsing
		v = sanitizeTimestamp(v, separator)

		// Parse the string as a precise fraction to avoid precision loss
		f := big.Rat{}
		if _, ok := f.SetString(v); !ok {
			return zero, errors.New("invalid number")
		}
		return timeFromFraction(&f, factor), nil
	}

	return zero, errors.New("unsupported type")
}

func timeFromFraction(f *big.Rat, factor int64) time.Time {
	// Extract the numerator and denominator and scale to nanoseconds
	num := f.Num()
	denom := f.Denom()
	num.Mul(num, big.NewInt(factor))

	// Get the integer (non-fractional part) of the timestamp and convert
	// it into time
	t := big.Int{}
	t.Div(num, denom)

	return time.Unix(0, t.Int64()).UTC()
}

// sanitizeTimestamp removes thousand separators and uses dot as
// decimal separator. Returns also a boolean indicating success.
func sanitizeTimestamp(timestamp string, decimalSeparator []string) string {
	// Remove thousand-separators that are not used for decimal separation
	sanitized := timestamp
	for _, s := range []string{" ", ",", "."} {
		if !choice.Contains(s, decimalSeparator) {
			sanitized = strings.ReplaceAll(sanitized, s, "")
		}
	}

	// Replace decimal separators by dot to have a standard, parsable format
	for _, s := range decimalSeparator {
		// Make sure we replace only the first occurrence of any separator.
		if strings.Contains(sanitized, s) {
			return strings.Replace(sanitized, s, ".", 1)
		}
	}
	return sanitized
}

// parseTime parses a string timestamp according to the format string.
func parseTime(format, timestamp string, location *time.Location) (time.Time, error) {
	loc := location
	if loc == nil {
		loc = time.UTC
	}

	switch strings.ToLower(format) {
	case "ansic":
		format = time.ANSIC
	case "unixdate":
		format = time.UnixDate
	case "rubydate":
		format = time.RubyDate
	case "rfc822":
		format = time.RFC822
	case "rfc822z":
		format = time.RFC822Z
	case "rfc850":
		format = time.RFC850
	case "rfc1123":
		format = time.RFC1123
	case "rfc1123z":
		format = time.RFC1123Z
	case "rfc3339":
		format = time.RFC3339
	case "rfc3339nano":
		format = time.RFC3339Nano
	case "stamp":
		format = time.Stamp
	case "stampmilli":
		format = time.StampMilli
	case "stampmicro":
		format = time.StampMicro
	case "stampnano":
		format = time.StampNano
	}

	if !strings.Contains(format, "MST") {
		return time.ParseInLocation(format, timestamp, loc)
	}

	// Golang does not parse times with ambiguous timezone abbreviations,
	// but only parses the time-fields and the timezone NAME with a zero
	// offset (see https://groups.google.com/g/golang-nuts/c/hDMdnm_jUFQ/m/yeL9IHOsAQAJ).
	// To handle those timezones correctly we can use the timezone-name and
	// force parsing the time in that timezone. This way we get the correct
	// time for the "most probably" of the ambiguous timezone-abbreviations.
	ts, err := time.Parse(format, timestamp)
	if err != nil {
		return time.Time{}, err
	}
	zone, offset := ts.Zone()
	if zone == "UTC" || offset != 0 {
		return ts.In(loc), nil
	}
	once.Do(func() {
		const msg = `Your config is using abbreviated timezones and parsing was changed in v1.27.0!
		Please see the change log, remove any workarounds in place, and carefully
		check your data timestamps! If case you experience any problems, please
		file an issue on https://github.com/influxdata/telegraf/issues!`
		log.Print("W! " + msg)
	})

	abbrevLoc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot resolve timezone abbreviation %q: %w", zone, err)
	}
	ts, err = time.ParseInLocation(format, timestamp, abbrevLoc)
	if err != nil {
		return time.Time{}, err
	}
	return ts.In(loc), nil
}
