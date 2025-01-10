//go:generate ../../../tools/readme_config_includer/generator
package aerospike

import (
	"crypto/tls"
	_ "embed"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	as "github.com/aerospike/aerospike-client-go/v5"

	"github.com/influxdata/telegraf"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Aerospike struct {
	Servers []string `toml:"servers"`

	Username string `toml:"username"`
	Password string `toml:"password"`

	EnableTLS bool   `toml:"enable_tls"`
	EnableSSL bool   `toml:"enable_ssl" deprecated:"1.7.0;1.35.0;use 'enable_tls' instead"`
	TLSName   string `toml:"tls_name"`
	common_tls.ClientConfig

	initialized bool
	tlsConfig   *tls.Config

	DisableQueryNamespaces bool     `toml:"disable_query_namespaces"`
	Namespaces             []string `toml:"namespaces"`

	QuerySets bool     `toml:"query_sets"`
	Sets      []string `toml:"sets"`

	EnableTTLHistogram              bool `toml:"enable_ttl_histogram"`
	EnableObjectSizeLinearHistogram bool `toml:"enable_object_size_linear_histogram"`

	NumberHistogramBuckets int `toml:"num_histogram_buckets"`
}

// On the random chance a hex value is all digits
// these are fields that can contain hex and should always be strings
var protectedHexFields = map[string]bool{
	"node_name":       true,
	"cluster_key":     true,
	"paxos_principal": true,
}

func (*Aerospike) SampleConfig() string {
	return sampleConfig
}

func (a *Aerospike) Gather(acc telegraf.Accumulator) error {
	if !a.initialized {
		tlsConfig, err := a.ClientConfig.TLSConfig()
		if err != nil {
			return err
		}
		if tlsConfig == nil && (a.EnableTLS || a.EnableSSL) {
			tlsConfig = &tls.Config{}
		}
		a.tlsConfig = tlsConfig
		a.initialized = true
	}

	if a.NumberHistogramBuckets == 0 {
		a.NumberHistogramBuckets = 10
	} else if a.NumberHistogramBuckets > 100 {
		a.NumberHistogramBuckets = 100
	} else if a.NumberHistogramBuckets < 1 {
		a.NumberHistogramBuckets = 10
	}

	if len(a.Servers) == 0 {
		return a.gatherServer(acc, "127.0.0.1:3000")
	}

	var wg sync.WaitGroup
	wg.Add(len(a.Servers))
	for _, server := range a.Servers {
		go func(serv string) {
			defer wg.Done()
			acc.AddError(a.gatherServer(acc, serv))
		}(server)
	}

	wg.Wait()
	return nil
}

func (a *Aerospike) gatherServer(acc telegraf.Accumulator, hostPort string) error {
	policy := as.NewClientPolicy()
	policy.User = a.Username
	policy.Password = a.Password
	policy.TlsConfig = a.tlsConfig
	asHosts, err := as.NewHosts(hostPort)
	if err != nil {
		return err
	}
	if a.TLSName != "" && (a.EnableTLS || a.EnableSSL) {
		for _, asHost := range asHosts {
			asHost.TLSName = a.TLSName
		}
	}
	c, err := as.NewClientWithPolicyAndHost(policy, asHosts...)
	if err != nil {
		return err
	}
	asInfoPolicy := as.NewInfoPolicy()
	defer c.Close()

	nodes := c.GetNodes()
	for _, n := range nodes {
		nodeHost := n.GetHost().String()
		stats, err := getNodeInfo(n, asInfoPolicy)
		if err != nil {
			return err
		}
		parseNodeInfo(acc, stats, nodeHost, n.GetName())

		namespaces, err := a.getNamespaces(n, asInfoPolicy)
		if err != nil {
			return err
		}

		if !a.DisableQueryNamespaces {
			// Query Namespaces
			for _, namespace := range namespaces {
				stats, err = getNamespaceInfo(namespace, n, asInfoPolicy)

				if err != nil {
					continue
				}
				parseNamespaceInfo(acc, stats, nodeHost, namespace, n.GetName())

				if a.EnableTTLHistogram {
					err = a.getTTLHistogram(acc, nodeHost, namespace, "", n, asInfoPolicy)
					if err != nil {
						continue
					}
				}
				if a.EnableObjectSizeLinearHistogram {
					err = a.getObjectSizeLinearHistogram(acc, nodeHost, namespace, "", n, asInfoPolicy)
					if err != nil {
						continue
					}
				}
			}
		}

		if a.QuerySets {
			namespaceSets, err := a.getSets(n, asInfoPolicy)
			if err == nil {
				for _, namespaceSet := range namespaceSets {
					namespace, set := splitNamespaceSet(namespaceSet)
					stats, err := getSetInfo(namespaceSet, n, asInfoPolicy)

					if err != nil {
						continue
					}
					parseSetInfo(acc, stats, nodeHost, namespaceSet, n.GetName())

					if a.EnableTTLHistogram {
						err = a.getTTLHistogram(acc, nodeHost, namespace, set, n, asInfoPolicy)
						if err != nil {
							continue
						}
					}

					if a.EnableObjectSizeLinearHistogram {
						err = a.getObjectSizeLinearHistogram(acc, nodeHost, namespace, set, n, asInfoPolicy)
						if err != nil {
							continue
						}
					}
				}
			}
		}
	}
	return nil
}

func getNodeInfo(n *as.Node, infoPolicy *as.InfoPolicy) (map[string]string, error) {
	stats, err := n.RequestInfo(infoPolicy, "statistics")
	if err != nil {
		return nil, err
	}

	return stats, nil
}

func parseNodeInfo(acc telegraf.Accumulator, stats map[string]string, hostPort, nodeName string) {
	nTags := map[string]string{
		"aerospike_host": hostPort,
		"node_name":      nodeName,
	}
	nFields := make(map[string]interface{})
	stat := strings.Split(stats["statistics"], ";")
	for _, pair := range stat {
		parts := strings.Split(pair, "=")
		if len(parts) < 2 {
			continue
		}
		key := strings.ReplaceAll(parts[0], "-", "_")
		nFields[key] = parseAerospikeValue(key, parts[1])
	}
	acc.AddFields("aerospike_node", nFields, nTags, time.Now())
}

func (a *Aerospike) getNamespaces(n *as.Node, infoPolicy *as.InfoPolicy) ([]string, error) {
	var namespaces []string
	if len(a.Namespaces) == 0 {
		info, err := n.RequestInfo(infoPolicy, "namespaces")
		if err != nil {
			return namespaces, err
		}
		namespaces = strings.Split(info["namespaces"], ";")
	} else {
		namespaces = a.Namespaces
	}

	return namespaces, nil
}

func getNamespaceInfo(namespace string, n *as.Node, infoPolicy *as.InfoPolicy) (map[string]string, error) {
	stats, err := n.RequestInfo(infoPolicy, "namespace/"+namespace)
	if err != nil {
		return nil, err
	}

	return stats, err
}

func parseNamespaceInfo(acc telegraf.Accumulator, stats map[string]string, hostPort, namespace, nodeName string) {
	nTags := map[string]string{
		"aerospike_host": hostPort,
		"node_name":      nodeName,
	}
	nTags["namespace"] = namespace
	nFields := make(map[string]interface{})

	stat := strings.Split(stats["namespace/"+namespace], ";")
	for _, pair := range stat {
		parts := strings.Split(pair, "=")
		if len(parts) < 2 {
			continue
		}
		key := strings.ReplaceAll(parts[0], "-", "_")
		nFields[key] = parseAerospikeValue(key, parts[1])
	}
	acc.AddFields("aerospike_namespace", nFields, nTags, time.Now())
}

func (a *Aerospike) getSets(n *as.Node, infoPolicy *as.InfoPolicy) ([]string, error) {
	var namespaceSets []string
	// Gather all sets
	if len(a.Sets) == 0 {
		stats, err := n.RequestInfo(infoPolicy, "sets")
		if err != nil {
			return namespaceSets, err
		}
		stat := strings.Split(stats["sets"], ";")
		for _, setStats := range stat {
			// setInfo is "ns=test:set=foo:objects=1:tombstones=0"
			if len(setStats) > 0 {
				pairs := strings.Split(setStats, ":")
				var ns, set string
				for _, pair := range pairs {
					parts := strings.Split(pair, "=")
					if len(parts) == 2 {
						if parts[0] == "ns" {
							ns = parts[1]
						}
						if parts[0] == "set" {
							set = parts[1]
						}
					}
				}
				if len(ns) > 0 && len(set) > 0 {
					namespaceSets = append(namespaceSets, fmt.Sprintf("%s/%s", ns, set))
				}
			}
		}
	} else { // User has passed in sets
		namespaceSets = a.Sets
	}

	return namespaceSets, nil
}

func getSetInfo(namespaceSet string, n *as.Node, infoPolicy *as.InfoPolicy) (map[string]string, error) {
	stats, err := n.RequestInfo(infoPolicy, "sets/"+namespaceSet)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func parseSetInfo(acc telegraf.Accumulator, stats map[string]string, hostPort, namespaceSet, nodeName string) {
	stat := strings.Split(
		strings.TrimSuffix(
			stats["sets/"+namespaceSet], ";"), ":")
	nTags := map[string]string{
		"aerospike_host": hostPort,
		"node_name":      nodeName,
		"set":            namespaceSet,
	}
	nFields := make(map[string]interface{})
	for _, part := range stat {
		pieces := strings.Split(part, "=")
		if len(pieces) < 2 {
			continue
		}

		key := strings.ReplaceAll(pieces[0], "-", "_")
		nFields[key] = parseAerospikeValue(key, pieces[1])
	}
	acc.AddFields("aerospike_set", nFields, nTags, time.Now())
}

func (a *Aerospike) getTTLHistogram(acc telegraf.Accumulator, hostPort, namespace, set string, n *as.Node, infoPolicy *as.InfoPolicy) error {
	stats, err := getHistogram(namespace, set, "ttl", n, infoPolicy)
	if err != nil {
		return err
	}

	nTags := createTags(hostPort, n.GetName(), namespace, set)
	a.parseHistogram(acc, stats, nTags, "ttl")

	return nil
}

func (a *Aerospike) getObjectSizeLinearHistogram(acc telegraf.Accumulator, hostPort, namespace, set string, n *as.Node, infoPolicy *as.InfoPolicy) error {
	stats, err := getHistogram(namespace, set, "object-size-linear", n, infoPolicy)
	if err != nil {
		return err
	}

	nTags := createTags(hostPort, n.GetName(), namespace, set)
	a.parseHistogram(acc, stats, nTags, "object-size-linear")

	return nil
}

func getHistogram(namespace, set, histogramType string, n *as.Node, infoPolicy *as.InfoPolicy) (map[string]string, error) {
	var queryArg string
	if len(set) > 0 {
		queryArg = fmt.Sprintf("histogram:type=%s;namespace=%v;set=%v", histogramType, namespace, set)
	} else {
		queryArg = fmt.Sprintf("histogram:type=%s;namespace=%v", histogramType, namespace)
	}

	stats, err := n.RequestInfo(infoPolicy, queryArg)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func (a *Aerospike) parseHistogram(acc telegraf.Accumulator, stats, nTags map[string]string, histogramType string) {
	nFields := make(map[string]interface{})

	for _, stat := range stats {
		for _, part := range strings.Split(stat, ":") {
			pieces := strings.Split(part, "=")
			if len(pieces) < 2 {
				continue
			}

			if pieces[0] == "buckets" {
				buckets := strings.Split(pieces[1], ",")

				// Normalize in case of less buckets than expected
				numRecordsPerBucket := 1
				if len(buckets) > a.NumberHistogramBuckets {
					numRecordsPerBucket = int(math.Ceil(float64(len(buckets)) / float64(a.NumberHistogramBuckets)))
				}

				bucketCount := 0
				bucketSum := int64(0) // cast to int64, as can have large object sums
				bucketName := 0
				for i, bucket := range buckets {
					// Sum records and increment bucket collection counter
					if bucketCount < numRecordsPerBucket {
						bucketSum = bucketSum + parseAerospikeValue("", bucket).(int64)
						bucketCount++
					}

					// Store records and reset counters
					// increment bucket name
					if bucketCount == numRecordsPerBucket {
						nFields[strconv.Itoa(bucketName)] = bucketSum

						bucketCount = 0
						bucketSum = 0
						bucketName++
					} else if i == (len(buckets) - 1) {
						// base/edge case where final bucket does not fully
						// fill number of records per bucket
						nFields[strconv.Itoa(bucketName)] = bucketSum
					}
				}
			}
		}
	}

	acc.AddFields(fmt.Sprintf("aerospike_histogram_%v", strings.ReplaceAll(histogramType, "-", "_")), nFields, nTags, time.Now())
}

func splitNamespaceSet(namespaceSet string) (namespace, set string) {
	split := strings.Split(namespaceSet, "/")
	return split[0], split[1]
}

func parseAerospikeValue(key, v string) interface{} {
	if protectedHexFields[key] {
		return v
	} else if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
		return parsed
	} else if parsed, err := strconv.ParseUint(v, 10, 64); err == nil {
		return parsed
	} else if parsed, err := strconv.ParseBool(v); err == nil {
		return parsed
	} else if parsed, err := strconv.ParseFloat(v, 32); err == nil {
		return parsed
	}
	// leave as string
	return v
}

func createTags(hostPort, nodeName, namespace, set string) map[string]string {
	nTags := map[string]string{
		"aerospike_host": hostPort,
		"node_name":      nodeName,
		"namespace":      namespace,
	}

	if len(set) > 0 {
		nTags["set"] = set
	}
	return nTags
}

func init() {
	inputs.Add("aerospike", func() telegraf.Input {
		return &Aerospike{}
	})
}
