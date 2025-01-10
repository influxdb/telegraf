//go:generate ../../../tools/readme_config_includer/generator
package memcached

import (
	"bufio"
	"bytes"
	"crypto/tls"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"golang.org/x/net/proxy"

	"github.com/influxdata/telegraf"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	defaultTimeout = 5 * time.Second

	// The list of metrics that should be sent
	sendMetrics = []string{
		"accepting_conns",
		"auth_cmds",
		"auth_errors",
		"bytes",
		"bytes_read",
		"bytes_written",
		"cas_badval",
		"cas_hits",
		"cas_misses",
		"cmd_flush",
		"cmd_get",
		"cmd_set",
		"cmd_touch",
		"conn_yields",
		"connection_structures",
		"curr_connections",
		"curr_items",
		"decr_hits",
		"decr_misses",
		"delete_hits",
		"delete_misses",
		"evicted_active",
		"evicted_unfetched",
		"evictions",
		"expired_unfetched",
		"extstore_compact_lost",
		"extstore_compact_rescues",
		"extstore_compact_resc_cold",
		"extstore_compact_resc_old",
		"extstore_compact_skipped",
		"extstore_page_allocs",
		"extstore_page_evictions",
		"extstore_page_reclaims",
		"extstore_pages_free",
		"extstore_pages_used",
		"extstore_objects_evicted",
		"extstore_objects_read",
		"extstore_objects_written",
		"extstore_objects_used",
		"extstore_bytes_evicted",
		"extstore_bytes_written",
		"extstore_bytes_read",
		"extstore_bytes_used",
		"extstore_bytes_fragmented",
		"extstore_limit_maxbytes",
		"extstore_io_queue",
		"get_expired",
		"get_flushed",
		"get_hits",
		"get_misses",
		"hash_bytes",
		"hash_is_expanding",
		"hash_power_level",
		"incr_hits",
		"incr_misses",
		"limit_maxbytes",
		"listen_disabled_num",
		"max_connections",
		"reclaimed",
		"rejected_connections",
		"store_no_memory",
		"store_too_large",
		"threads",
		"total_connections",
		"total_items",
		"touch_hits",
		"touch_misses",
		"uptime",
	}
)

type Memcached struct {
	Servers     []string `toml:"servers"`
	UnixSockets []string `toml:"unix_sockets"`
	EnableTLS   bool     `toml:"enable_tls"`
	common_tls.ClientConfig
}

func (*Memcached) SampleConfig() string {
	return sampleConfig
}

func (m *Memcached) Gather(acc telegraf.Accumulator) error {
	if len(m.Servers) == 0 && len(m.UnixSockets) == 0 {
		return m.gatherServer(":11211", false, acc)
	}

	for _, serverAddress := range m.Servers {
		acc.AddError(m.gatherServer(serverAddress, false, acc))
	}

	for _, unixAddress := range m.UnixSockets {
		acc.AddError(m.gatherServer(unixAddress, true, acc))
	}

	return nil
}

func (m *Memcached) gatherServer(address string, unix bool, acc telegraf.Accumulator) error {
	var conn net.Conn
	var err error
	var dialer proxy.Dialer

	dialer = &net.Dialer{Timeout: defaultTimeout}
	if m.EnableTLS {
		tlsCfg, err := m.ClientConfig.TLSConfig()
		if err != nil {
			return err
		}

		dialer = &tls.Dialer{
			NetDialer: dialer.(*net.Dialer),
			Config:    tlsCfg,
		}
	}

	if unix {
		conn, err = dialer.Dial("unix", address)
		if err != nil {
			return err
		}
		defer conn.Close()
	} else {
		_, _, err = net.SplitHostPort(address)
		if err != nil {
			address = address + ":11211"
		}

		conn, err = dialer.Dial("tcp", address)
		if err != nil {
			return err
		}
		defer conn.Close()
	}

	if conn == nil {
		return errors.New("failed to create net connection")
	}

	// Extend connection
	if err := conn.SetDeadline(time.Now().Add(defaultTimeout)); err != nil {
		return err
	}

	// Read and write buffer
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	// Send command
	if _, err := fmt.Fprint(rw, "stats\r\n"); err != nil {
		return err
	}
	if err := rw.Flush(); err != nil {
		return err
	}

	values, err := parseResponse(rw.Reader)
	if err != nil {
		return err
	}

	// Add server address as a tag
	tags := map[string]string{"server": address}

	// Process values
	fields := make(map[string]interface{})
	for _, key := range sendMetrics {
		if value, ok := values[key]; ok {
			// Mostly it is the number
			if iValue, errParse := strconv.ParseInt(value, 10, 64); errParse == nil {
				fields[key] = iValue
			} else {
				fields[key] = value
			}
		}
	}
	acc.AddFields("memcached", fields, tags)
	return nil
}

func parseResponse(r *bufio.Reader) (map[string]string, error) {
	values := make(map[string]string)

	for {
		// Read line
		line, _, errRead := r.ReadLine()
		if errRead != nil {
			return values, errRead
		}
		// Done
		if bytes.Equal(line, []byte("END")) {
			break
		}
		// Read values
		s := bytes.SplitN(line, []byte(" "), 3)
		if len(s) != 3 || !bytes.Equal(s[0], []byte("STAT")) {
			return values, fmt.Errorf("unexpected line in stats response: %q", line)
		}

		// Save values
		values[string(s[1])] = string(s[2])
	}
	return values, nil
}

func init() {
	inputs.Add("memcached", func() telegraf.Input {
		return &Memcached{}
	})
}
