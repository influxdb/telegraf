//go:generate ../../../tools/readme_config_includer/generator
package suricata

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	// inBufSize is the input buffer size for JSON received via socket.
	// Set to 10MB, as depending on the number of threads the output might be
	// large.
	inBufSize = 10 * 1024 * 1024
)

type Suricata struct {
	Source    string `toml:"source"`
	Delimiter string `toml:"delimiter"`
	Alerts    bool   `toml:"alerts"`
	Version   string `toml:"version"`

	inputListener *net.UnixListener
	cancel        context.CancelFunc

	Log telegraf.Logger `toml:"-"`

	wg sync.WaitGroup
}

func (*Suricata) SampleConfig() string {
	return sampleConfig
}

func (s *Suricata) Init() error {
	if s.Source == "" {
		s.Source = "/var/run/suricata-stats.sock"
	}

	if s.Delimiter == "" {
		s.Delimiter = "_"
	}

	switch s.Version {
	case "":
		s.Version = "1"
	case "1", "2":
	default:
		return fmt.Errorf("invalid version %q, use either 1 or 2", s.Version)
	}

	return nil
}

// Start initiates background collection of JSON data from the socket provided to Suricata.
func (s *Suricata) Start(acc telegraf.Accumulator) error {
	var err error
	s.inputListener, err = net.ListenUnix("unix", &net.UnixAddr{
		Name: s.Source,
		Net:  "unix",
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.inputListener.SetUnlinkOnClose(true)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		go s.handleServerConnection(ctx, acc)
	}()
	return nil
}

// Gather measures and submits one full set of telemetry to Telegraf.
// Not used here, submission is completely input-driven.
func (*Suricata) Gather(telegraf.Accumulator) error {
	return nil
}

// Stop causes the plugin to cease collecting JSON data from the socket provided to Suricata.
func (s *Suricata) Stop() {
	s.inputListener.Close()
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Suricata) readInput(ctx context.Context, acc telegraf.Accumulator, conn net.Conn) error {
	reader := bufio.NewReaderSize(conn, inBufSize)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			line, rerr := reader.ReadBytes('\n')
			if rerr != nil {
				return rerr
			}
			if len(line) > 0 {
				err := s.parse(acc, line)
				if err != nil {
					acc.AddError(err)
				}
			}
		}
	}
}

func (s *Suricata) handleServerConnection(ctx context.Context, acc telegraf.Accumulator) {
	var err error
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var conn net.Conn
			conn, err = s.inputListener.Accept()
			if err != nil {
				if !strings.HasSuffix(err.Error(), ": use of closed network connection") {
					acc.AddError(err)
				}
				continue
			}
			err = s.readInput(ctx, acc, conn)
			// we want to handle EOF as an opportunity to wait for a new
			// connection -- this could, for example, happen when Suricata is
			// restarted while Telegraf is running.
			if !errors.Is(err, io.EOF) {
				acc.AddError(err)
				return
			}
		}
	}
}

func flexFlatten(outmap map[string]interface{}, field string, v interface{}, delimiter string) error {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, v := range t {
			var err error
			if field == "" {
				err = flexFlatten(outmap, k, v, delimiter)
			} else {
				err = flexFlatten(outmap, fmt.Sprintf("%s%s%s", field, delimiter, k), v, delimiter)
			}
			if err != nil {
				return err
			}
		}
	case []interface{}:
		for _, v := range t {
			err := flexFlatten(outmap, field, v, delimiter)
			if err != nil {
				return err
			}
		}
	case string:
		outmap[field] = v
	case float64:
		outmap[field] = t
	default:
		return fmt.Errorf("unsupported type %T encountered", t)
	}
	return nil
}

func (s *Suricata) parseAlert(acc telegraf.Accumulator, result map[string]interface{}) {
	if _, ok := result["alert"].(map[string]interface{}); !ok {
		s.Log.Debug("'alert' sub-object does not have required structure")
		return
	}

	totalmap := make(map[string]interface{})
	for k, v := range result["alert"].(map[string]interface{}) {
		// source and target fields are maps
		err := flexFlatten(totalmap, k, v, s.Delimiter)
		if err != nil {
			s.Log.Debugf("Flattening alert failed: %v", err)
			// we skip this subitem as something did not parse correctly
			continue
		}
	}

	// threads field do not exist in alert output, always global
	acc.AddFields("suricata_alert", totalmap, nil)
}

func (s *Suricata) parseStats(acc telegraf.Accumulator, result map[string]interface{}) {
	if _, ok := result["stats"].(map[string]interface{}); !ok {
		s.Log.Debug("The 'stats' sub-object does not have required structure")
		return
	}

	fields := make(map[string]map[string]interface{})
	totalmap := make(map[string]interface{})
	for k, v := range result["stats"].(map[string]interface{}) {
		if k == "threads" {
			if v, ok := v.(map[string]interface{}); ok {
				for k, t := range v {
					outmap := make(map[string]interface{})
					if threadStruct, ok := t.(map[string]interface{}); ok {
						err := flexFlatten(outmap, "", threadStruct, s.Delimiter)
						if err != nil {
							s.Log.Debugf("Flattening alert failed: %v", err)
							// we skip this thread as something did not parse correctly
							continue
						}
						fields[k] = outmap
					}
				}
			} else {
				s.Log.Debug("The 'threads' sub-object does not have required structure")
			}
		} else {
			err := flexFlatten(totalmap, k, v, s.Delimiter)
			if err != nil {
				s.Log.Debugf("Flattening alert failed: %v", err)
				// we skip this subitem as something did not parse correctly
				continue
			}
		}
	}
	fields["total"] = totalmap

	for k := range fields {
		if k == "Global" {
			acc.AddFields("suricata", fields[k], nil)
		} else {
			acc.AddFields("suricata", fields[k], map[string]string{"thread": k})
		}
	}
}

func (s *Suricata) parseGeneric(acc telegraf.Accumulator, result map[string]interface{}) error {
	eventType := ""
	if _, ok := result["event_type"]; !ok {
		return fmt.Errorf("unable to determine event type of message: %s", result)
	}
	value, err := internal.ToString(result["event_type"])
	if err != nil {
		return fmt.Errorf("unable to convert event type %q to string: %w", result["event_type"], err)
	}
	eventType = value

	timestamp := time.Now()
	if val, ok := result["timestamp"]; ok {
		value, err := internal.ToString(val)
		if err != nil {
			return fmt.Errorf("unable to convert timestamp %q to string: %w", val, err)
		}
		timestamp, err = time.Parse("2006-01-02T15:04:05.999999-0700", value)
		if err != nil {
			return fmt.Errorf("unable to parse timestamp %q: %w", val, err)
		}
	}

	// Make sure the event key exists first
	if _, ok := result[eventType].(map[string]interface{}); !ok {
		return fmt.Errorf("unable to find key %q in %s", eventType, result)
	}

	fields := make(map[string]interface{})
	for k, v := range result[eventType].(map[string]interface{}) {
		err := flexFlatten(fields, k, v, s.Delimiter)
		if err != nil {
			s.Log.Debugf("Flattening %q failed: %v", eventType, err)
			continue
		}
	}

	tags := map[string]string{
		"event_type": eventType,
	}

	// best effort to gather these tags and fields, if errors are encountered
	// we ignore and move on
	for _, key := range []string{"proto", "out_iface", "in_iface"} {
		if val, ok := result[key]; ok {
			if convertedVal, err := internal.ToString(val); err == nil {
				tags[key] = convertedVal
			}
		}
	}
	for _, key := range []string{"src_ip", "dest_ip"} {
		if val, ok := result[key]; ok {
			if convertedVal, err := internal.ToString(val); err == nil {
				fields[key] = convertedVal
			}
		}
	}
	for _, key := range []string{"src_port", "dest_port"} {
		if val, ok := result[key]; ok {
			if convertedVal, err := internal.ToInt64(val); err == nil || errors.Is(err, internal.ErrOutOfRange) {
				fields[key] = convertedVal
			}
		}
	}

	acc.AddFields("suricata", fields, tags, timestamp)
	return nil
}

func (s *Suricata) parse(acc telegraf.Accumulator, sjson []byte) error {
	// initial parsing
	var result map[string]interface{}
	err := json.Unmarshal(sjson, &result)
	if err != nil {
		return err
	}

	if s.Version == "2" {
		return s.parseGeneric(acc, result)
	}

	// Version 1 parsing of stats and optionally alerts
	if _, ok := result["stats"]; ok {
		s.parseStats(acc, result)
	} else if _, ok := result["alert"]; ok {
		if s.Alerts {
			s.parseAlert(acc, result)
		}
	} else {
		s.Log.Debugf("Invalid input without 'stats' or 'alert' object: %v", result)
		return errors.New("input does not contain 'stats' or 'alert' object")
	}

	return nil
}

func init() {
	inputs.Add("suricata", func() telegraf.Input {
		return &Suricata{}
	})
}
