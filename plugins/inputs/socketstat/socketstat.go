//go:generate ../../../tools/readme_config_includer/generator
//go:build !windows

// iproute2 doesn't exist on Windows

package socketstat

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const measurement = "socketstat"

type Socketstat struct {
	SocketProto []string        `toml:"protocols"`
	Timeout     config.Duration `toml:"timeout"`
	Log         telegraf.Logger `toml:"-"`

	isNewConnection *regexp.Regexp
	validValues     *regexp.Regexp
	cmdName         string
	lister          socketLister
}

type socketLister func(cmdName string, proto string, timeout config.Duration) (*bytes.Buffer, error)

func (*Socketstat) SampleConfig() string {
	return sampleConfig
}

func (ss *Socketstat) Init() error {
	if len(ss.SocketProto) == 0 {
		ss.SocketProto = []string{"tcp", "udp"}
	}

	// Initialize regexps to validate input data
	validFields := "(bytes_acked|bytes_received|segs_out|segs_in|data_segs_in|data_segs_out)"
	ss.validValues = regexp.MustCompile("^" + validFields + ":[0-9]+$")
	ss.isNewConnection = regexp.MustCompile(`^\s+.*$`)

	ss.lister = socketList

	// Check that ss is installed, get its path.
	// Do it last, because in test environments where `ss` might not be available,
	//   we still want the other Init() actions to be performed.
	ssPath, err := exec.LookPath("ss")
	if err != nil {
		return err
	}
	ss.cmdName = ssPath

	return nil
}

func (ss *Socketstat) Gather(acc telegraf.Accumulator) error {
	// best effort : we continue through the protocols even if an error is encountered,
	// but we keep track of the last error.
	for _, proto := range ss.SocketProto {
		out, err := ss.lister(ss.cmdName, proto, ss.Timeout)
		if err != nil {
			acc.AddError(err)
			continue
		}
		ss.parseAndGather(acc, out, proto)
	}
	return nil
}

func socketList(cmdName, proto string, timeout config.Duration) (*bytes.Buffer, error) {
	// Run ss for the given protocol, return the output as bytes.Buffer
	args := []string{"-in", "--" + proto}
	cmd := exec.Command(cmdName, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := internal.RunTimeout(cmd, time.Duration(timeout))
	if err != nil {
		return &out, fmt.Errorf("error running ss -in --%s: %w", proto, err)
	}
	return &out, nil
}

func (ss *Socketstat) parseAndGather(acc telegraf.Accumulator, data *bytes.Buffer, proto string) {
	scanner := bufio.NewScanner(data)
	tags := make(map[string]string)
	fields := make(map[string]interface{})

	// ss output can have blank lines, and/or socket basic info lines and more advanced
	// statistics lines, in turns.
	// In all non-empty lines, we can have metrics, so we need to group those relevant to
	// the same connection.
	// To achieve this, we're using the flushData variable which indicates if we should add
	// a new measurement or postpone it to a later line.

	// The first line is only headers
	scanner.Scan()

	flushData := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		words := strings.Fields(line)

		if ss.isNewConnection.MatchString(line) {
			// A line with starting whitespace means metrics about the current connection.
			// We should never get 2 consecutive such lines. If we do, log a warning and in
			// a best effort, extend the metrics from the 1st line with the metrics of the 2nd
			// one, possibly overwriting.
			for _, word := range words {
				if !ss.validValues.MatchString(word) {
					continue
				}
				// kv will have 2 fields because it matched the regexp
				kv := strings.Split(word, ":")
				v, err := strconv.ParseUint(kv[1], 10, 64)
				if err != nil {
					ss.Log.Infof("Couldn't parse metric %q: %v", word, err)
					continue
				}
				fields[kv[0]] = v
			}
			if !flushData {
				ss.Log.Warnf("Found orphaned metrics: %s", words)
				ss.Log.Warn("Added them to the last known connection.")
			}
			acc.AddFields(measurement, fields, tags)
			flushData = false
			continue
		}
		// A line with no starting whitespace means we're going to parse a new connection.
		// Flush what we gathered about the previous one, if any.
		if flushData {
			acc.AddFields(measurement, fields, tags)
		}

		// Delegate the real parsing to getTagsAndState, which manages various
		// formats depending on the protocol.
		tags, fields = getTagsAndState(proto, words, ss.Log)

		// This line contained metrics, so record that.
		flushData = true
	}
	if flushData {
		acc.AddFields(measurement, fields, tags)
	}
}

func getTagsAndState(proto string, words []string, log telegraf.Logger) (map[string]string, map[string]interface{}) {
	tags := map[string]string{
		"proto": proto,
	}
	fields := make(map[string]interface{})
	switch proto {
	case "udp", "raw":
		words = append([]string{"dummy"}, words...)
	case "tcp", "dccp", "sctp":
		fields["state"] = words[0]
	}
	switch proto {
	case "tcp", "udp", "raw", "dccp", "sctp":
		// Local and remote addresses are fields 3 and 4
		// Separate addresses and ports with the last ':'
		localIndex := strings.LastIndex(words[3], ":")
		remoteIndex := strings.LastIndex(words[4], ":")
		tags["local_addr"] = words[3][:localIndex]
		tags["local_port"] = words[3][localIndex+1:]
		tags["remote_addr"] = words[4][:remoteIndex]
		tags["remote_port"] = words[4][remoteIndex+1:]
	case "unix", "packet":
		fields["netid"] = words[0]
		tags["local_addr"] = words[4]
		tags["local_port"] = words[5]
		tags["remote_addr"] = words[6]
		tags["remote_port"] = words[7]
	}
	v, err := strconv.ParseUint(words[1], 10, 64)
	if err != nil {
		log.Warnf("Couldn't read recv_q in %q: %v", words, err)
	} else {
		fields["recv_q"] = v
	}
	v, err = strconv.ParseUint(words[2], 10, 64)
	if err != nil {
		log.Warnf("Couldn't read send_q in %q: %v", words, err)
	} else {
		fields["send_q"] = v
	}
	return tags, fields
}

func init() {
	inputs.Add("socketstat", func() telegraf.Input {
		return &Socketstat{Timeout: config.Duration(time.Second)}
	})
}
