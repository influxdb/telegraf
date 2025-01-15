//go:generate ../../../tools/readme_config_includer/generator
package redis_sentinel

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/go-redis/redis/v7"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	measurementMasters   = "redis_sentinel_masters"
	measurementSentinel  = "redis_sentinel"
	measurementSentinels = "redis_sentinel_sentinels"
	measurementReplicas  = "redis_sentinel_replicas"
)

type RedisSentinel struct {
	Servers []string `toml:"servers"`
	tls.ClientConfig

	clients []*redisSentinelClient
}

type redisSentinelClient struct {
	sentinel *redis.SentinelClient
	tags     map[string]string
}

func (*RedisSentinel) SampleConfig() string {
	return sampleConfig
}

func (r *RedisSentinel) Init() error {
	if len(r.Servers) == 0 {
		r.Servers = []string{"tcp://localhost:26379"}
	}

	tlsConfig, err := r.ClientConfig.TLSConfig()
	if err != nil {
		return err
	}

	r.clients = make([]*redisSentinelClient, 0, len(r.Servers))
	for _, serv := range r.Servers {
		u, err := url.Parse(serv)
		if err != nil {
			return fmt.Errorf("unable to parse to address %q: %w", serv, err)
		}

		username := ""
		password := ""
		if u.User != nil {
			username = u.User.Username()
			pw, ok := u.User.Password()
			if ok {
				password = pw
			}
		}

		var address string
		tags := make(map[string]string, 2)
		switch u.Scheme {
		case "tcp":
			address = u.Host
			tags["source"] = u.Hostname()
			tags["port"] = u.Port()
		case "unix":
			address = u.Path
			tags["socket"] = u.Path
		default:
			return fmt.Errorf("invalid scheme %q, expected tcp or unix", u.Scheme)
		}

		sentinel := redis.NewSentinelClient(
			&redis.Options{
				Addr:      address,
				Username:  username,
				Password:  password,
				Network:   u.Scheme,
				PoolSize:  1,
				TLSConfig: tlsConfig,
			},
		)

		r.clients = append(r.clients, &redisSentinelClient{
			sentinel: sentinel,
			tags:     tags,
		})
	}

	return nil
}

func (r *RedisSentinel) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup

	for _, client := range r.clients {
		wg.Add(1)

		go func(acc telegraf.Accumulator, client *redisSentinelClient) {
			defer wg.Done()

			masters, err := client.gatherMasterStats(acc)
			acc.AddError(err)

			for _, master := range masters {
				acc.AddError(client.gatherReplicaStats(acc, master))
				acc.AddError(client.gatherSentinelStats(acc, master))
			}

			acc.AddError(client.gatherInfoStats(acc))
		}(acc, client)
	}

	wg.Wait()

	return nil
}

// Redis list format has string key/values adjacent, so convert to a map for easier use
func toMap(vals []interface{}) map[string]string {
	m := make(map[string]string)

	for idx := 0; idx < len(vals)-1; idx += 2 {
		key, keyOk := vals[idx].(string)
		value, valueOk := vals[idx+1].(string)

		if keyOk && valueOk {
			m[key] = value
		}
	}

	return m
}

func castFieldValue(value string, fieldType configFieldType) (interface{}, error) {
	var castedValue interface{}
	var err error

	switch fieldType {
	case configFieldTypeFloat:
		castedValue, err = strconv.ParseFloat(value, 64)
	case configFieldTypeInteger:
		castedValue, err = strconv.ParseInt(value, 10, 64)
	case configFieldTypeString:
		castedValue = value
	default:
		return nil, fmt.Errorf("unsupported field type %v", fieldType)
	}

	if err != nil {
		return nil, fmt.Errorf("casting value %q failed: %w", value, err)
	}

	return castedValue, nil
}

func prepareFieldValues(fields map[string]string, typeMap map[string]configFieldType) (map[string]interface{}, error) {
	preparedFields := make(map[string]interface{})

	for key, val := range fields {
		key = strings.ReplaceAll(key, "-", "_")

		valType, ok := typeMap[key]
		if !ok {
			continue
		}

		castedVal, err := castFieldValue(val, valType)
		if err != nil {
			return nil, err
		}

		preparedFields[key] = castedVal
	}

	return preparedFields, nil
}

func (client *redisSentinelClient) gatherInfoStats(acc telegraf.Accumulator) error {
	infoCmd := redis.NewStringCmd("info", "all")
	if err := client.sentinel.Process(infoCmd); err != nil {
		return err
	}

	info, err := infoCmd.Result()
	if err != nil {
		return err
	}

	rdr := strings.NewReader(info)
	infoTags, infoFields, err := convertSentinelInfoOutput(client.tags, rdr)
	if err != nil {
		return err
	}

	acc.AddFields(measurementSentinel, infoFields, infoTags)

	return nil
}

func (client *redisSentinelClient) gatherMasterStats(acc telegraf.Accumulator) ([]string, error) {
	mastersCmd := redis.NewSliceCmd("sentinel", "masters")
	if err := client.sentinel.Process(mastersCmd); err != nil {
		return nil, err
	}

	masters, err := mastersCmd.Result()
	if err != nil {
		return nil, err
	}

	// Break out of the loop if one of the items comes out malformed
	// It's safe to assume that if we fail parsing one item that the rest will fail too
	// This is because we are iterating over a single server response
	masterNames := make([]string, 0, len(masters))
	for _, master := range masters {
		master, ok := master.([]interface{})
		if !ok {
			return masterNames, errors.New("unable to process master response")
		}

		m := toMap(master)

		masterName, ok := m["name"]
		if !ok {
			return masterNames, errors.New("unable to resolve master name")
		}
		masterNames = append(masterNames, masterName)

		quorumCmd := redis.NewStringCmd("sentinel", "ckquorum", masterName)
		quorumErr := client.sentinel.Process(quorumCmd)

		sentinelMastersTags, sentinelMastersFields, err := convertSentinelMastersOutput(client.tags, m, quorumErr)
		if err != nil {
			return masterNames, err
		}
		acc.AddFields(measurementMasters, sentinelMastersFields, sentinelMastersTags)
	}

	return masterNames, nil
}

func (client *redisSentinelClient) gatherReplicaStats(acc telegraf.Accumulator, masterName string) error {
	replicasCmd := redis.NewSliceCmd("sentinel", "replicas", masterName)
	if err := client.sentinel.Process(replicasCmd); err != nil {
		return err
	}

	replicas, err := replicasCmd.Result()
	if err != nil {
		return err
	}

	// Break out of the loop if one of the items comes out malformed
	// It's safe to assume that if we fail parsing one item that the rest will fail too
	// This is because we are iterating over a single server response
	for _, replica := range replicas {
		replica, ok := replica.([]interface{})
		if !ok {
			return errors.New("unable to process replica response")
		}

		rm := toMap(replica)
		replicaTags, replicaFields, err := convertSentinelReplicaOutput(client.tags, masterName, rm)
		if err != nil {
			return err
		}

		acc.AddFields(measurementReplicas, replicaFields, replicaTags)
	}

	return nil
}

func (client *redisSentinelClient) gatherSentinelStats(acc telegraf.Accumulator, masterName string) error {
	sentinelsCmd := redis.NewSliceCmd("sentinel", "sentinels", masterName)
	if err := client.sentinel.Process(sentinelsCmd); err != nil {
		return err
	}

	sentinels, err := sentinelsCmd.Result()
	if err != nil {
		return err
	}

	// Break out of the loop if one of the items comes out malformed
	// It's safe to assume that if we fail parsing one item that the rest will fail too
	// This is because we are iterating over a single server response
	for _, sentinel := range sentinels {
		sentinel, ok := sentinel.([]interface{})
		if !ok {
			return errors.New("unable to process sentinel response")
		}

		sm := toMap(sentinel)
		sentinelTags, sentinelFields, err := convertSentinelSentinelsOutput(client.tags, masterName, sm)
		if err != nil {
			return err
		}

		acc.AddFields(measurementSentinels, sentinelFields, sentinelTags)
	}

	return nil
}

// converts `sentinel masters <name>` output to tags and fields
func convertSentinelMastersOutput(globalTags, master map[string]string, quorumErr error) (map[string]string, map[string]interface{}, error) {
	tags := globalTags

	tags["master"] = master["name"]

	fields, err := prepareFieldValues(master, measurementMastersFields)
	if err != nil {
		return nil, nil, err
	}

	fields["has_quorum"] = quorumErr == nil

	return tags, fields, nil
}

// converts `sentinel sentinels <name>` output to tags and fields
func convertSentinelSentinelsOutput(
	globalTags map[string]string,
	masterName string,
	sentinelMaster map[string]string,
) (map[string]string, map[string]interface{}, error) {
	tags := globalTags

	tags["sentinel_ip"] = sentinelMaster["ip"]
	tags["sentinel_port"] = sentinelMaster["port"]
	tags["master"] = masterName

	fields, err := prepareFieldValues(sentinelMaster, measurementSentinelsFields)
	if err != nil {
		return nil, nil, err
	}

	return tags, fields, nil
}

// converts `sentinel replicas <name>` output to tags and fields
func convertSentinelReplicaOutput(
	globalTags map[string]string,
	masterName string,
	replica map[string]string,
) (map[string]string, map[string]interface{}, error) {
	tags := globalTags

	tags["replica_ip"] = replica["ip"]
	tags["replica_port"] = replica["port"]
	tags["master"] = masterName

	fields, err := prepareFieldValues(replica, measurementReplicasFields)
	if err != nil {
		return nil, nil, err
	}

	return tags, fields, nil
}

// convertSentinelInfoOutput parses `INFO` command output
// Largely copied from the Redis input plugin's gatherInfoOutput()
func convertSentinelInfoOutput(globalTags map[string]string, rdr io.Reader) (map[string]string, map[string]interface{}, error) {
	scanner := bufio.NewScanner(rdr)
	rawFields := make(map[string]string)

	tags := globalTags

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		// Redis denotes configuration sections with a hashtag
		// Example of the section header: # Clients
		if line[0] == '#' {
			// Nothing interesting here
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			// Not a valid configuration option
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		rawFields[key] = val
	}

	fields, err := prepareFieldValues(rawFields, measurementSentinelFields)
	if err != nil {
		return nil, nil, err
	}

	// Rename the field and convert it to nanoseconds
	secs, ok := fields["uptime_in_seconds"].(int64)
	if !ok {
		return nil, nil, fmt.Errorf("uptime type %T is not int64", fields["uptime_in_seconds"])
	}
	fields["uptime_ns"] = secs * 1000_000_000
	delete(fields, "uptime_in_seconds")

	// Rename in order to match the "redis" input plugin
	fields["clients"] = fields["connected_clients"]
	delete(fields, "connected_clients")

	return tags, fields, nil
}

func init() {
	inputs.Add("redis_sentinel", func() telegraf.Input {
		return &RedisSentinel{}
	})
}
