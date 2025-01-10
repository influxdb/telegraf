//go:generate ../../../tools/readme_config_includer/generator
package leofs

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
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
	oid = ".1.3.6.1.4.1.35450"
	// For Manager Master
	defaultEndpoint = "127.0.0.1:4020"
)

type serverType int

const (
	serverTypeManagerMaster serverType = iota
	serverTypeManagerSlave
	serverTypeStorage
	serverTypeGateway
)

type LeoFS struct {
	Servers []string `toml:"servers"`
}

var keyMapping = map[serverType][]string{
	serverTypeManagerMaster: {
		"num_of_processes",
		"total_memory_usage",
		"system_memory_usage",
		"processes_memory_usage",
		"ets_memory_usage",
		"num_of_processes_5min",
		"total_memory_usage_5min",
		"system_memory_usage_5min",
		"processes_memory_usage_5min",
		"ets_memory_usage_5min",
		"used_allocated_memory",
		"allocated_memory",
		"used_allocated_memory_5min",
		"allocated_memory_5min",
	},
	serverTypeManagerSlave: {
		"num_of_processes",
		"total_memory_usage",
		"system_memory_usage",
		"processes_memory_usage",
		"ets_memory_usage",
		"num_of_processes_5min",
		"total_memory_usage_5min",
		"system_memory_usage_5min",
		"processes_memory_usage_5min",
		"ets_memory_usage_5min",
		"used_allocated_memory",
		"allocated_memory",
		"used_allocated_memory_5min",
		"allocated_memory_5min",
	},
	serverTypeStorage: {
		"num_of_processes",
		"total_memory_usage",
		"system_memory_usage",
		"processes_memory_usage",
		"ets_memory_usage",
		"num_of_processes_5min",
		"total_memory_usage_5min",
		"system_memory_usage_5min",
		"processes_memory_usage_5min",
		"ets_memory_usage_5min",
		"num_of_writes",
		"num_of_reads",
		"num_of_deletes",
		"num_of_writes_5min",
		"num_of_reads_5min",
		"num_of_deletes_5min",
		"num_of_active_objects",
		"total_objects",
		"total_size_of_active_objects",
		"total_size",
		"num_of_replication_messages",
		"num_of_sync-vnode_messages",
		"num_of_rebalance_messages",
		"used_allocated_memory",
		"allocated_memory",
		"used_allocated_memory_5min",
		"allocated_memory_5min",
		// following items are since LeoFS v1.4.0
		"mq_num_of_msg_recovery_node",
		"mq_num_of_msg_deletion_dir",
		"mq_num_of_msg_async_deletion_dir",
		"mq_num_of_msg_req_deletion_dir",
		"mq_mdcr_num_of_msg_req_comp_metadata",
		"mq_mdcr_num_of_msg_req_sync_obj",
		"comp_state",
		"comp_last_start_datetime",
		"comp_last_end_datetime",
		"comp_num_of_pending_targets",
		"comp_num_of_ongoing_targets",
		"comp_num_of_out_of_targets",
	},
	serverTypeGateway: {
		"num_of_processes",
		"total_memory_usage",
		"system_memory_usage",
		"processes_memory_usage",
		"ets_memory_usage",
		"num_of_processes_5min",
		"total_memory_usage_5min",
		"system_memory_usage_5min",
		"processes_memory_usage_5min",
		"ets_memory_usage_5min",
		"num_of_writes",
		"num_of_reads",
		"num_of_deletes",
		"num_of_writes_5min",
		"num_of_reads_5min",
		"num_of_deletes_5min",
		"count_of_cache-hit",
		"count_of_cache-miss",
		"total_of_files",
		"total_cached_size",
		"used_allocated_memory",
		"allocated_memory",
		"used_allocated_memory_5min",
		"allocated_memory_5min",
	},
}

var serverTypeMapping = map[string]serverType{
	"4020": serverTypeManagerMaster,
	"4021": serverTypeManagerSlave,
	"4010": serverTypeStorage,
	"4011": serverTypeStorage,
	"4012": serverTypeStorage,
	"4013": serverTypeStorage,
	"4000": serverTypeGateway,
	"4001": serverTypeGateway,
}

func (*LeoFS) SampleConfig() string {
	return sampleConfig
}

func (l *LeoFS) Gather(acc telegraf.Accumulator) error {
	if len(l.Servers) == 0 {
		return gatherServer(defaultEndpoint, serverTypeManagerMaster, acc)
	}
	var wg sync.WaitGroup
	for _, endpoint := range l.Servers {
		results := strings.Split(endpoint, ":")

		port := "4020"
		if len(results) > 2 {
			acc.AddError(fmt.Errorf("unable to parse address %q", endpoint))
			continue
		} else if len(results) == 2 {
			_, err := strconv.Atoi(results[1])
			if err != nil {
				acc.AddError(fmt.Errorf("unable to parse port from %q", endpoint))
				continue
			}
			port = results[1]
		}

		st, ok := serverTypeMapping[port]
		if !ok {
			st = serverTypeStorage
		}
		wg.Add(1)
		go func(endpoint string, st serverType) {
			defer wg.Done()
			acc.AddError(gatherServer(endpoint, st, acc))
		}(endpoint, st)
	}
	wg.Wait()
	return nil
}

func gatherServer(endpoint string, serverType serverType, acc telegraf.Accumulator) error {
	cmd := exec.Command("snmpwalk", "-v2c", "-cpublic", "-On", endpoint, oid)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer internal.WaitTimeout(cmd, time.Second*5) //nolint:errcheck // ignore the returned error as we cannot do anything about it anyway
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		return errors.New("unable to retrieve the node name")
	}
	nodeName, err := retrieveTokenAfterColon(scanner.Text())
	if err != nil {
		return err
	}
	nodeNameTrimmed := strings.Trim(nodeName, "\"")
	tags := map[string]string{
		"node": nodeNameTrimmed,
	}
	i := 0

	fields := make(map[string]interface{})
	for scanner.Scan() {
		key := keyMapping[serverType][i]
		val, err := retrieveTokenAfterColon(scanner.Text())
		if err != nil {
			return err
		}
		fVal, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("unable to parse the value %q: %w", val, err)
		}
		fields[key] = fVal
		i++
	}
	acc.AddFields("leofs", fields, tags)
	return nil
}

func retrieveTokenAfterColon(line string) (string, error) {
	tokens := strings.Split(line, ":")
	if len(tokens) != 2 {
		return "", fmt.Errorf("':' not found in the line:%s", line)
	}
	return strings.TrimSpace(tokens[1]), nil
}

func init() {
	inputs.Add("leofs", func() telegraf.Input {
		return &LeoFS{}
	})
}
