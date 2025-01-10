//go:generate ../../../tools/readme_config_includer/generator
package mcrouter

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// enum for statType
type statType int

const (
	typeInt   statType = iota
	typeFloat statType = iota
)

var (
	defaultTimeout   = 5 * time.Second
	defaultServerURL = url.URL{
		Scheme: "tcp",
		Host:   "localhost:11211",
	}
	// The list of metrics that should be sent
	sendMetrics = map[string]statType{
		"uptime":                                     typeInt,
		"num_servers":                                typeInt,
		"num_servers_new":                            typeInt,
		"num_servers_up":                             typeInt,
		"num_servers_down":                           typeInt,
		"num_servers_closed":                         typeInt,
		"num_clients":                                typeInt,
		"num_suspect_servers":                        typeInt,
		"destination_batches_sum":                    typeInt,
		"destination_requests_sum":                   typeInt,
		"outstanding_route_get_reqs_queued":          typeInt,
		"outstanding_route_update_reqs_queued":       typeInt,
		"outstanding_route_get_avg_queue_size":       typeInt,
		"outstanding_route_update_avg_queue_size":    typeInt,
		"outstanding_route_get_avg_wait_time_sec":    typeInt,
		"outstanding_route_update_avg_wait_time_sec": typeInt,
		"retrans_closed_connections":                 typeInt,
		"destination_pending_reqs":                   typeInt,
		"destination_inflight_reqs":                  typeInt,
		"destination_batch_size":                     typeInt,
		"asynclog_requests":                          typeInt,
		"proxy_reqs_processing":                      typeInt,
		"proxy_reqs_waiting":                         typeInt,
		"client_queue_notify_period":                 typeInt,
		"rusage_system":                              typeFloat,
		"rusage_user":                                typeFloat,
		"ps_num_minor_faults":                        typeInt,
		"ps_num_major_faults":                        typeInt,
		"ps_user_time_sec":                           typeFloat,
		"ps_system_time_sec":                         typeFloat,
		"ps_vsize":                                   typeInt,
		"ps_rss":                                     typeInt,
		"fibers_allocated":                           typeInt,
		"fibers_pool_size":                           typeInt,
		"fibers_stack_high_watermark":                typeInt,
		"successful_client_connections":              typeInt,
		"duration_us":                                typeInt,
		"destination_max_pending_reqs":               typeInt,
		"destination_max_inflight_reqs":              typeInt,
		"retrans_per_kbyte_max":                      typeInt,
		"cmd_get_count":                              typeInt,
		"cmd_delete_out":                             typeInt,
		"cmd_lease_get":                              typeInt,
		"cmd_set":                                    typeInt,
		"cmd_get_out_all":                            typeInt,
		"cmd_get_out":                                typeInt,
		"cmd_lease_set_count":                        typeInt,
		"cmd_other_out_all":                          typeInt,
		"cmd_lease_get_out":                          typeInt,
		"cmd_set_count":                              typeInt,
		"cmd_lease_set_out":                          typeInt,
		"cmd_delete_count":                           typeInt,
		"cmd_other":                                  typeInt,
		"cmd_delete":                                 typeInt,
		"cmd_get":                                    typeInt,
		"cmd_lease_set":                              typeInt,
		"cmd_set_out":                                typeInt,
		"cmd_lease_get_count":                        typeInt,
		"cmd_other_out":                              typeInt,
		"cmd_lease_get_out_all":                      typeInt,
		"cmd_set_out_all":                            typeInt,
		"cmd_other_count":                            typeInt,
		"cmd_delete_out_all":                         typeInt,
		"cmd_lease_set_out_all":                      typeInt,
	}
)

type Mcrouter struct {
	Servers []string        `toml:"servers"`
	Timeout config.Duration `toml:"timeout"`
}

func (*Mcrouter) SampleConfig() string {
	return sampleConfig
}

func (m *Mcrouter) Gather(acc telegraf.Accumulator) error {
	ctx := context.Background()

	if m.Timeout < config.Duration(1*time.Second) {
		m.Timeout = config.Duration(defaultTimeout)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(m.Timeout))
	defer cancel()

	if len(m.Servers) == 0 {
		m.Servers = []string{defaultServerURL.String()}
	}

	for _, serverAddress := range m.Servers {
		acc.AddError(gatherServer(ctx, serverAddress, acc))
	}

	return nil
}

// parseAddress parses an address string into 'host:port' and 'protocol' parts
func parseAddress(address string) (parsedAddress, protocol string, err error) {
	var host string
	var port string

	parsedAddress = address

	u, parseError := url.Parse(parsedAddress)

	if parseError != nil {
		return "", "", errors.New("invalid server address")
	}

	if u.Scheme != "tcp" && u.Scheme != "unix" {
		return "", "", errors.New("invalid server protocol")
	}

	protocol = u.Scheme

	if protocol == "unix" {
		if u.Path == "" {
			return "", "", errors.New("invalid unix socket path")
		}

		parsedAddress = u.Path
	} else {
		if u.Host == "" {
			return "", "", errors.New("invalid host")
		}

		host = u.Hostname()
		port = u.Port()

		if host == "" {
			host = defaultServerURL.Hostname()
		}

		if port == "" {
			port = defaultServerURL.Port()
		}

		parsedAddress = host + ":" + port
	}

	return parsedAddress, protocol, nil
}

func gatherServer(ctx context.Context, address string, acc telegraf.Accumulator) error {
	var conn net.Conn
	var err error
	var protocol string
	var dialer net.Dialer

	address, protocol, err = parseAddress(address)
	if err != nil {
		return err
	}

	conn, err = dialer.DialContext(ctx, protocol, address)
	if err != nil {
		return err
	}

	defer conn.Close()

	// Extend connection
	deadline, ok := ctx.Deadline()

	if ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}

	// Read and write buffer
	reader := bufio.NewReader(conn)
	scanner := bufio.NewScanner(reader)

	// Send command
	if _, err := fmt.Fprint(conn, "stats\r\n"); err != nil {
		return err
	}

	values, err := parseResponse(scanner)

	if err != nil {
		return err
	}

	// Add server address as a tag
	tags := map[string]string{"server": address}

	// Process values
	fields := make(map[string]interface{})
	for key, sType := range sendMetrics {
		if value, ok := values[key]; ok {
			switch sType {
			case typeInt:
				if v, errParse := strconv.ParseInt(value, 10, 64); errParse == nil {
					fields[key] = v
				}
			case typeFloat:
				if v, errParse := strconv.ParseFloat(value, 64); errParse == nil {
					fields[key] = v
				}
			default:
			}
		}
	}
	acc.AddFields("mcrouter", fields, tags)
	return nil
}

func parseResponse(r *bufio.Scanner) (map[string]string, error) {
	values := make(map[string]string)

	for r.Scan() {
		// Read line
		line := r.Text()

		// Done
		if line == "END" {
			break
		}

		// Read values
		s := strings.SplitN(line, " ", 3)

		if len(s) != 3 || s[0] != "STAT" {
			return nil, fmt.Errorf("unexpected line in stats response: %s", line)
		}

		// Save values
		values[s[1]] = s[2]
	}

	return values, nil
}

func init() {
	inputs.Add("mcrouter", func() telegraf.Input {
		return &Mcrouter{}
	})
}
