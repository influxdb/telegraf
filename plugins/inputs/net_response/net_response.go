//go:generate ../../../tools/readme_config_includer/generator
package net_response

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"regexp"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type resultType uint64

const (
	success          resultType = 0
	timeout          resultType = 1
	connectionFailed resultType = 2
	readFailed       resultType = 3
	stringMismatch   resultType = 4
)

type NetResponse struct {
	Address     string          `toml:"address"`
	Timeout     config.Duration `toml:"timeout"`
	ReadTimeout config.Duration `toml:"read_timeout"`
	Send        string          `toml:"send"`
	Expect      string          `toml:"expect"`
	Protocol    string          `toml:"protocol"`
}

func (*NetResponse) SampleConfig() string {
	return sampleConfig
}

func (n *NetResponse) Init() error {
	// Set default values
	if n.Timeout == 0 {
		n.Timeout = config.Duration(time.Second)
	}
	if n.ReadTimeout == 0 {
		n.ReadTimeout = config.Duration(time.Second)
	}
	// Check send and expected string
	if n.Protocol == "udp" && n.Send == "" {
		return errors.New("send string cannot be empty")
	}
	if n.Protocol == "udp" && n.Expect == "" {
		return errors.New("expected string cannot be empty")
	}
	// Prepare host and port
	host, port, err := net.SplitHostPort(n.Address)
	if err != nil {
		return err
	}
	if host == "" {
		n.Address = "localhost:" + port
	}
	if port == "" {
		return errors.New("bad port in config option address")
	}

	if err := choice.Check(n.Protocol, []string{"tcp", "udp"}); err != nil {
		return fmt.Errorf("config option protocol: %w", err)
	}

	return nil
}

func (n *NetResponse) Gather(acc telegraf.Accumulator) error {
	// Prepare host and port
	host, port, err := net.SplitHostPort(n.Address)
	if err != nil {
		return err
	}

	// Prepare data
	tags := map[string]string{"server": host, "port": port}
	var fields map[string]interface{}
	var returnTags map[string]string

	// Gather data
	switch n.Protocol {
	case "tcp":
		returnTags, fields, err = n.tcpGather()
		if err != nil {
			return err
		}
		tags["protocol"] = "tcp"
	case "udp":
		returnTags, fields, err = n.udpGather()
		if err != nil {
			return err
		}
		tags["protocol"] = "udp"
	}

	// Merge the tags
	for k, v := range returnTags {
		tags[k] = v
	}
	// Add metrics
	acc.AddFields("net_response", fields, tags)
	return nil
}

func (n *NetResponse) tcpGather() (map[string]string, map[string]interface{}, error) {
	// Prepare returns
	tags := make(map[string]string)
	fields := make(map[string]interface{})
	// Start Timer
	start := time.Now()
	// Connecting
	conn, err := net.DialTimeout("tcp", n.Address, time.Duration(n.Timeout))
	// Stop timer
	responseTime := time.Since(start).Seconds()
	// Handle error
	if err != nil {
		var e net.Error
		if errors.As(err, &e) && e.Timeout() {
			setResult(timeout, fields, tags, n.Expect)
		} else {
			setResult(connectionFailed, fields, tags, n.Expect)
		}
		return tags, fields, nil
	}
	defer conn.Close()
	// Send string if needed
	if n.Send != "" {
		msg := []byte(n.Send)
		if _, gerr := conn.Write(msg); gerr != nil {
			return nil, nil, gerr
		}
		// Stop timer
		responseTime = time.Since(start).Seconds()
	}
	// Read string if needed
	if n.Expect != "" {
		// Set read timeout
		if gerr := conn.SetReadDeadline(time.Now().Add(time.Duration(n.ReadTimeout))); gerr != nil {
			return nil, nil, gerr
		}
		// Prepare reader
		reader := bufio.NewReader(conn)
		tp := textproto.NewReader(reader)
		// Read
		data, err := tp.ReadLine()
		// Stop timer
		responseTime = time.Since(start).Seconds()
		// Handle error
		if err != nil {
			setResult(readFailed, fields, tags, n.Expect)
		} else {
			// Looking for string in answer
			regEx := regexp.MustCompile(`.*` + n.Expect + `.*`)
			find := regEx.FindString(data)
			if find != "" {
				setResult(success, fields, tags, n.Expect)
			} else {
				setResult(stringMismatch, fields, tags, n.Expect)
			}
		}
	} else {
		setResult(success, fields, tags, n.Expect)
	}
	fields["response_time"] = responseTime
	return tags, fields, nil
}

func (n *NetResponse) udpGather() (map[string]string, map[string]interface{}, error) {
	// Prepare returns
	tags := make(map[string]string)
	fields := make(map[string]interface{})
	// Start Timer
	start := time.Now()
	// Resolving
	udpAddr, err := net.ResolveUDPAddr("udp", n.Address)
	// Handle error
	if err != nil {
		setResult(connectionFailed, fields, tags, n.Expect)
		return tags, fields, nil
	}
	// Connecting
	conn, err := net.DialUDP("udp", nil, udpAddr)
	// Handle error
	if err != nil {
		setResult(connectionFailed, fields, tags, n.Expect)
		return tags, fields, nil
	}
	defer conn.Close()
	// Send string
	msg := []byte(n.Send)
	if _, gerr := conn.Write(msg); gerr != nil {
		return nil, nil, gerr
	}
	// Read string
	// Set read timeout
	if gerr := conn.SetReadDeadline(time.Now().Add(time.Duration(n.ReadTimeout))); gerr != nil {
		return nil, nil, gerr
	}
	// Read
	buf := make([]byte, 1024)
	_, _, err = conn.ReadFromUDP(buf)
	// Stop timer
	responseTime := time.Since(start).Seconds()
	// Handle error
	if err != nil {
		setResult(readFailed, fields, tags, n.Expect)
		return tags, fields, nil
	}

	// Looking for string in answer
	regEx := regexp.MustCompile(`.*` + n.Expect + `.*`)
	find := regEx.FindString(string(buf))
	if find != "" {
		setResult(success, fields, tags, n.Expect)
	} else {
		setResult(stringMismatch, fields, tags, n.Expect)
	}

	fields["response_time"] = responseTime

	return tags, fields, nil
}

func setResult(result resultType, fields map[string]interface{}, tags map[string]string, expect string) {
	var tag string
	switch result {
	case success:
		tag = "success"
	case timeout:
		tag = "timeout"
	case connectionFailed:
		tag = "connection_failed"
	case readFailed:
		tag = "read_failed"
	case stringMismatch:
		tag = "string_mismatch"
	}

	tags["result"] = tag
	fields["result_code"] = uint64(result)

	// deprecated in 1.7; use result tag
	fields["result_type"] = tag

	// deprecated in 1.4; use result tag
	if expect != "" {
		fields["string_found"] = result == success
	}
}

func init() {
	inputs.Add("net_response", func() telegraf.Input {
		return &NetResponse{}
	})
}
