//go:generate ../../../tools/config_includer/generator
//go:generate ../../../tools/readme_config_includer/generator
package slurm

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"

	goslurm "github.com/pcolladosoto/goslurm/v0038"
)

//go:embed sample.conf
var sampleConfig string

type Slurm struct {
	URL              string          `toml:"url"`
	Username         string          `toml:"username"`
	Token            string          `toml:"token"`
	EnabledEndpoints []string        `toml:"enabled_endpoints"`
	ResponseTimeout  config.Duration `toml:"response_timeout"`
	Log              telegraf.Logger `toml:"-"`
	tls.ClientConfig

	client      *goslurm.APIClient
	baseURL     *url.URL
	endpointMap map[string]bool
}

func (*Slurm) SampleConfig() string {
	return sampleConfig
}

func (s *Slurm) Init() error {
	if len(s.EnabledEndpoints) == 0 {
		s.EnabledEndpoints = []string{"diag", "jobs", "nodes", "partitions", "reservations"}
	}

	s.endpointMap = make(map[string]bool, len(s.EnabledEndpoints))
	for _, endpoint := range s.EnabledEndpoints {
		switch e := strings.ToLower(endpoint); e {
		case "diag", "jobs", "nodes", "partitions", "reservations":
			s.endpointMap[e] = true
		default:
			return fmt.Errorf("unknown endpoint %q", endpoint)
		}
	}

	if s.URL == "" {
		return errors.New("empty URL provided")
	}

	u, err := url.Parse(s.URL)
	if err != nil {
		return err
	}

	if u.Hostname() == "" {
		return fmt.Errorf("empty hostname for url %q", s.URL)
	}

	s.baseURL = u

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid scheme %q", u.Scheme)
	}

	tlsCfg, err := s.ClientConfig.TLSConfig()
	if err != nil {
		return err
	}

	if u.Scheme == "http" && tlsCfg != nil {
		s.Log.Warn("non-empty TLS configuration for a URL with an http scheme. Ignoring it...")
		tlsCfg = nil
	}

	configuration := goslurm.NewConfiguration()
	configuration.Host = u.Host
	configuration.Scheme = u.Scheme
	configuration.UserAgent = internal.ProductToken()
	configuration.HTTPClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
		Timeout: time.Duration(s.ResponseTimeout),
	}

	s.client = goslurm.NewAPIClient(configuration)

	return nil
}

func parseTres(tres string) map[string]interface{} {
	tresKVs := strings.Split(tres, ",")
	parsedValues := make(map[string]interface{}, len(tresKVs))

	for _, tresVal := range tresKVs {
		parsedTresVal := strings.Split(tresVal, "=")
		if len(parsedTresVal) != 2 {
			continue
		}

		tag := parsedTresVal[0]
		val := parsedTresVal[1]
		var factor float64 = 1

		if tag == "mem" {
			var ok bool
			factor, ok = map[string]float64{
				"K": 1.0 / 1024.0,
				"M": 1,
				"G": 1024,
				"T": 1024 * 1024,
				"P": 1024 * 1024 * 1024,
			}[strings.ToUpper(val[len(val)-1:])]
			if !ok {
				continue
			}
			val = val[:len(val)-1]
		}

		parsedFloat, err := strconv.ParseFloat(val, 64)
		if err == nil {
			parsedValues[tag] = parsedFloat * factor
			continue
		}
		parsedValues[tag] = val
	}

	return parsedValues
}

func (s *Slurm) gatherDiagMetrics(acc telegraf.Accumulator, diag *goslurm.V0038DiagStatistics) {
	records := make(map[string]interface{}, 13)
	tags := map[string]string{"source": s.baseURL.Hostname()}

	if int32Ptr, ok := diag.GetServerThreadCountOk(); ok {
		records["server_thread_count"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetJobsCanceledOk(); ok {
		records["jobs_canceled"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetJobsSubmittedOk(); ok {
		records["jobs_submitted"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetJobsStartedOk(); ok {
		records["jobs_started"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetJobsCompletedOk(); ok {
		records["jobs_completed"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetJobsFailedOk(); ok {
		records["jobs_failed"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetJobsPendingOk(); ok {
		records["jobs_pending"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetJobsRunningOk(); ok {
		records["jobs_running"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetScheduleCycleLastOk(); ok {
		records["schedule_cycle_last"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetScheduleCycleMeanOk(); ok {
		records["schedule_cycle_mean"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetBfQueueLenOk(); ok {
		records["bf_queue_len"] = *int32Ptr
	}
	if int32Ptr, ok := diag.GetBfQueueLenMeanOk(); ok {
		records["bf_queue_len_mean"] = *int32Ptr
	}
	if boolPtr, ok := diag.GetBfActiveOk(); ok {
		records["bf_active"] = *boolPtr
	}

	acc.AddFields("slurm_diag", records, tags)
}

func (s *Slurm) gatherJobsMetrics(acc telegraf.Accumulator, jobs []goslurm.V0038JobResponseProperties) {
	for i := range jobs {
		records := make(map[string]interface{}, 19)
		tags := make(map[string]string, 3)

		tags["source"] = s.baseURL.Hostname()
		if strPtr, ok := jobs[i].GetNameOk(); ok {
			tags["name"] = *strPtr
		}
		if int32Ptr, ok := jobs[i].GetJobIdOk(); ok {
			tags["job_id"] = strconv.Itoa(int(*int32Ptr))
		}

		if strPtr, ok := jobs[i].GetJobStateOk(); ok {
			records["state"] = *strPtr
		}
		if strPtr, ok := jobs[i].GetStateReasonOk(); ok {
			records["state_reason"] = *strPtr
		}
		if strPtr, ok := jobs[i].GetPartitionOk(); ok {
			records["partition"] = *strPtr
		}
		if strPtr, ok := jobs[i].GetNodesOk(); ok {
			records["nodes"] = *strPtr
		}
		if int32Ptr, ok := jobs[i].GetNodeCountOk(); ok {
			records["node_count"] = *int32Ptr
		}
		if int64Ptr, ok := jobs[i].GetPriorityOk(); ok {
			records["priority"] = *int64Ptr
		}
		if int32Ptr, ok := jobs[i].GetNiceOk(); ok {
			records["nice"] = *int32Ptr
		}
		if int32Ptr, ok := jobs[i].GetGroupIdOk(); ok {
			records["group_id"] = *int32Ptr
		}
		if strPtr, ok := jobs[i].GetCommandOk(); ok {
			records["command"] = *strPtr
		}
		if strPtr, ok := jobs[i].GetStandardOutputOk(); ok {
			records["standard_output"] = strings.ReplaceAll(*strPtr, "\\", "")
		}
		if strPtr, ok := jobs[i].GetStandardErrorOk(); ok {
			records["standard_error"] = strings.ReplaceAll(*strPtr, "\\", "")
		}
		if strPtr, ok := jobs[i].GetStandardInputOk(); ok {
			records["standard_input"] = strings.ReplaceAll(*strPtr, "\\", "")
		}
		if strPtr, ok := jobs[i].GetCurrentWorkingDirectoryOk(); ok {
			records["current_working_directory"] = strings.ReplaceAll(*strPtr, "\\", "")
		}
		if int64Ptr, ok := jobs[i].GetSubmitTimeOk(); ok {
			records["submit_time"] = *int64Ptr
		}
		if int64Ptr, ok := jobs[i].GetStartTimeOk(); ok {
			records["start_time"] = *int64Ptr
		}
		if int32Ptr, ok := jobs[i].GetCpusOk(); ok {
			records["cpus"] = *int32Ptr
		}
		if int32Ptr, ok := jobs[i].GetTasksOk(); ok {
			records["tasks"] = *int32Ptr
		}
		if int64Ptr, ok := jobs[i].GetTimeLimitOk(); ok {
			records["time_limit"] = *int64Ptr
		}
		if strPtr, ok := jobs[i].GetTresReqStrOk(); ok {
			for k, v := range parseTres(*strPtr) {
				records["tres_"+k] = v
			}
		}

		acc.AddFields("slurm_jobs", records, tags)
	}
}

func (s *Slurm) gatherNodesMetrics(acc telegraf.Accumulator, nodes []goslurm.V0038Node) {
	for _, node := range nodes {
		records := make(map[string]interface{}, 13)
		tags := make(map[string]string, 2)

		tags["source"] = s.baseURL.Hostname()
		if strPtr, ok := node.GetNameOk(); ok {
			tags["name"] = *strPtr
		}

		if strPtr, ok := node.GetStateOk(); ok {
			records["state"] = *strPtr
		}
		if int32Ptr, ok := node.GetCoresOk(); ok {
			records["cores"] = *int32Ptr
		}
		if int32Ptr, ok := node.GetCpusOk(); ok {
			records["cpus"] = *int32Ptr
		}
		if int64Ptr, ok := node.GetCpuLoadOk(); ok {
			records["cpu_load"] = *int64Ptr
		}
		if int64Ptr, ok := node.GetAllocCpusOk(); ok {
			records["alloc_cpu"] = *int64Ptr
		}
		if int32Ptr, ok := node.GetRealMemoryOk(); ok {
			records["real_memory"] = *int32Ptr
		}
		if int32Ptr, ok := node.GetFreeMemoryOk(); ok {
			records["free_memory"] = *int32Ptr
		}
		if int64Ptr, ok := node.GetAllocMemoryOk(); ok {
			records["alloc_memory"] = *int64Ptr
		}
		if strPtr, ok := node.GetTresOk(); ok {
			for k, v := range parseTres(*strPtr) {
				records["tres_"+k] = v
			}
		}
		if strPtr, ok := node.GetTresUsedOk(); ok {
			for k, v := range parseTres(*strPtr) {
				records["tres_used_"+k] = v
			}
		}
		if int32Ptr, ok := node.GetWeightOk(); ok {
			records["weight"] = *int32Ptr
		}
		if strPtr, ok := node.GetSlurmdVersionOk(); ok {
			records["slurmd_version"] = *strPtr
		}
		if strPtr, ok := node.GetArchitectureOk(); ok {
			records["architecture"] = *strPtr
		}

		acc.AddFields("slurm_nodes", records, tags)
	}
}

func (s *Slurm) gatherPartitionsMetrics(acc telegraf.Accumulator, partitions []goslurm.V0038Partition) {
	for _, partition := range partitions {
		records := make(map[string]interface{}, 5)
		tags := make(map[string]string, 2)

		tags["source"] = s.baseURL.Hostname()
		if strPtr, ok := partition.GetNameOk(); ok {
			tags["name"] = *strPtr
		}

		if strPtr, ok := partition.GetStateOk(); ok {
			records["state"] = *strPtr
		}
		if int32Ptr, ok := partition.GetTotalCpusOk(); ok {
			records["total_cpu"] = *int32Ptr
		}
		if int32Ptr, ok := partition.GetTotalNodesOk(); ok {
			records["total_nodes"] = *int32Ptr
		}
		if strPtr, ok := partition.GetNodesOk(); ok {
			records["nodes"] = *strPtr
		}
		if strPtr, ok := partition.GetTresOk(); ok {
			for k, v := range parseTres(*strPtr) {
				records["tres_"+k] = v
			}
		}

		acc.AddFields("slurm_partitions", records, tags)
	}
}

func (s *Slurm) gatherReservationsMetrics(acc telegraf.Accumulator, reservations []goslurm.V0038Reservation) {
	for _, reservation := range reservations {
		records := make(map[string]interface{}, 9)
		tags := make(map[string]string, 2)

		tags["source"] = s.baseURL.Hostname()
		if strPtr, ok := reservation.GetNameOk(); ok {
			tags["name"] = *strPtr
		}

		if int32Ptr, ok := reservation.GetCoreCountOk(); ok {
			records["core_count"] = *int32Ptr
		}
		if int32Ptr, ok := reservation.GetCoreSpecCntOk(); ok {
			records["core_spec_count"] = *int32Ptr
		}
		if strPtr, ok := reservation.GetGroupsOk(); ok {
			records["groups"] = *strPtr
		}
		if strPtr, ok := reservation.GetUsersOk(); ok {
			records["users"] = *strPtr
		}
		if int32Ptr, ok := reservation.GetStartTimeOk(); ok {
			records["start_time"] = *int32Ptr
		}
		if strPtr, ok := reservation.GetPartitionOk(); ok {
			records["partition"] = *strPtr
		}
		if strPtr, ok := reservation.GetAccountsOk(); ok {
			records["accounts"] = *strPtr
		}
		if int32Ptr, ok := reservation.GetNodeCountOk(); ok {
			records["node_count"] = *int32Ptr
		}
		if strPtr, ok := reservation.GetNodeListOk(); ok {
			records["node_list"] = *strPtr
		}

		acc.AddFields("slurm_reservations", records, tags)
	}
}

func (s *Slurm) Gather(acc telegraf.Accumulator) (err error) {
	auth := context.WithValue(
		context.Background(),
		goslurm.ContextAPIKeys,
		map[string]goslurm.APIKey{
			"user":  {Key: s.Username},
			"token": {Key: s.Token},
		},
	)

	if s.endpointMap["diag"] {
		diagResp, respRaw, err := s.client.SlurmAPI.SlurmV0038Diag(auth).Execute()
		if err != nil {
			return fmt.Errorf("error getting diag: %w", err)
		}
		if diag, ok := diagResp.GetStatisticsOk(); ok {
			s.gatherDiagMetrics(acc, diag)
		}
		respRaw.Body.Close()
	}

	if s.endpointMap["jobs"] {
		jobsResp, respRaw, err := s.client.SlurmAPI.SlurmV0038GetJobs(auth).Execute()
		if err != nil {
			return fmt.Errorf("error getting jobs: %w", err)
		}
		if jobs, ok := jobsResp.GetJobsOk(); ok {
			s.gatherJobsMetrics(acc, jobs)
		}
		respRaw.Body.Close()
	}

	if s.endpointMap["nodes"] {
		nodesResp, respRaw, err := s.client.SlurmAPI.SlurmV0038GetNodes(auth).Execute()
		if err != nil {
			return fmt.Errorf("error getting nodes: %w", err)
		}
		if nodes, ok := nodesResp.GetNodesOk(); ok {
			s.gatherNodesMetrics(acc, nodes)
		}
		respRaw.Body.Close()
	}

	if s.endpointMap["partitions"] {
		partitionsResp, respRaw, err := s.client.SlurmAPI.SlurmV0038GetPartitions(auth).Execute()
		if err != nil {
			return fmt.Errorf("error getting partitions: %w", err)
		}
		if partitions, ok := partitionsResp.GetPartitionsOk(); ok {
			s.gatherPartitionsMetrics(acc, partitions)
		}
		respRaw.Body.Close()
	}

	if s.endpointMap["reservations"] {
		reservationsResp, respRaw, err := s.client.SlurmAPI.SlurmV0038GetReservations(auth).Execute()
		if err != nil {
			return fmt.Errorf("error getting reservations: %w", err)
		}
		if reservations, ok := reservationsResp.GetReservationsOk(); ok {
			s.gatherReservationsMetrics(acc, reservations)
		}
		respRaw.Body.Close()
	}

	return nil
}

func init() {
	inputs.Add("slurm", func() telegraf.Input {
		return &Slurm{
			ResponseTimeout: config.Duration(5 * time.Second),
		}
	})
}
