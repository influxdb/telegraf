package clickhouse

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestClusterIncludeExcludeFilter(t *testing.T) {
	ch := ClickHouse{}
	require.Equal(t, "", ch.clusterIncludeExcludeFilter())
	ch.ClusterExclude = []string{"test_cluster"}
	require.Equal(t, "WHERE cluster NOT IN ('test_cluster')", ch.clusterIncludeExcludeFilter())

	ch.ClusterExclude = []string{"test_cluster"}
	ch.ClusterInclude = []string{"cluster"}
	require.Equal(t, "WHERE cluster IN ('cluster') OR cluster NOT IN ('test_cluster')", ch.clusterIncludeExcludeFilter())

	ch.ClusterExclude = make([]string, 0)
	ch.ClusterInclude = []string{"cluster1", "cluster2"}
	require.Equal(t, "WHERE cluster IN ('cluster1', 'cluster2')", ch.clusterIncludeExcludeFilter())

	ch.ClusterExclude = []string{"cluster1", "cluster2"}
	ch.ClusterInclude = make([]string, 0)
	require.Equal(t, "WHERE cluster NOT IN ('cluster1', 'cluster2')", ch.clusterIncludeExcludeFilter())
}

func TestChInt64(t *testing.T) {
	assets := map[string]uint64{
		`"1"`:                  1,
		"1":                    1,
		"42":                   42,
		`"42"`:                 42,
		"18446743937525109187": 18446743937525109187,
	}
	for src, expected := range assets {
		var v chUInt64
		err := v.UnmarshalJSON([]byte(src))
		require.NoError(t, err)
		require.Equal(t, expected, uint64(v))
	}
}

func TestGather(t *testing.T) {
	var (
		ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			type result struct {
				Data interface{} `json:"data"`
			}
			enc := json.NewEncoder(w)
			switch query := r.URL.Query().Get("query"); {
			case strings.Contains(query, "system.parts"):
				err := enc.Encode(result{
					Data: []struct {
						Database string   `json:"database"`
						Table    string   `json:"table"`
						Bytes    chUInt64 `json:"bytes"`
						Parts    chUInt64 `json:"parts"`
						Rows     chUInt64 `json:"rows"`
					}{
						{
							Database: "test_database",
							Table:    "test_table",
							Bytes:    1,
							Parts:    10,
							Rows:     100,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "system.events"):
				err := enc.Encode(result{
					Data: []struct {
						Metric string   `json:"metric"`
						Value  chUInt64 `json:"value"`
					}{
						{
							Metric: "TestSystemEvent",
							Value:  1000,
						},
						{
							Metric: "TestSystemEvent2",
							Value:  2000,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "system.metrics"):
				err := enc.Encode(result{
					Data: []struct {
						Metric string   `json:"metric"`
						Value  chUInt64 `json:"value"`
					}{
						{
							Metric: "TestSystemMetric",
							Value:  1000,
						},
						{
							Metric: "TestSystemMetric2",
							Value:  2000,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "system.asynchronous_metrics"):
				err := enc.Encode(result{
					Data: []struct {
						Metric string   `json:"metric"`
						Value  chUInt64 `json:"value"`
					}{
						{
							Metric: "TestSystemAsynchronousMetric",
							Value:  1000,
						},
						{
							Metric: "TestSystemAsynchronousMetric2",
							Value:  2000,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "zk_exists"):
				err := enc.Encode(result{
					Data: []struct {
						ZkExists chUInt64 `json:"zk_exists"`
					}{
						{
							ZkExists: 1,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "zk_root_nodes"):
				err := enc.Encode(result{
					Data: []struct {
						ZkRootNodes chUInt64 `json:"zk_root_nodes"`
					}{
						{
							ZkRootNodes: 2,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "replication_queue_exists"):
				err := enc.Encode(result{
					Data: []struct {
						ReplicationQueueExists chUInt64 `json:"replication_queue_exists"`
					}{
						{
							ReplicationQueueExists: 1,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "replication_too_many_tries_replicas"):
				err := enc.Encode(result{
					Data: []struct {
						TooManyTriesReplicas chUInt64 `json:"replication_too_many_tries_replicas"`
						NumTriesReplicas     chUInt64 `json:"replication_num_tries_replicas"`
					}{
						{
							TooManyTriesReplicas: 10,
							NumTriesReplicas:     100,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "system.detached_parts"):
				err := enc.Encode(result{
					Data: []struct {
						DetachedParts chUInt64 `json:"detached_parts"`
					}{
						{
							DetachedParts: 10,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "system.dictionaries"):
				err := enc.Encode(result{
					Data: []struct {
						Origin         string   `json:"origin"`
						Status         string   `json:"status"`
						BytesAllocated chUInt64 `json:"bytes_allocated"`
					}{
						{
							Origin:         "default.test_dict",
							Status:         "NOT_LOADED",
							BytesAllocated: 100,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "system.mutations"):
				err := enc.Encode(result{
					Data: []struct {
						Failed    chUInt64 `json:"failed"`
						Completed chUInt64 `json:"completed"`
						Running   chUInt64 `json:"running"`
					}{
						{
							Failed:    10,
							Running:   1,
							Completed: 100,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "system.disks"):
				err := enc.Encode(result{
					Data: []struct {
						Name            string   `json:"name"`
						Path            string   `json:"path"`
						FreePercent     chUInt64 `json:"free_space_percent"`
						KeepFreePercent chUInt64 `json:"keep_free_space_percent"`
					}{
						{
							Name:            "default",
							Path:            "/var/lib/clickhouse",
							FreePercent:     1,
							KeepFreePercent: 10,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "system.processes"):
				err := enc.Encode(result{
					Data: []struct {
						QueryType      string  `json:"query_type"`
						Percentile50   float64 `json:"p50"`
						Percentile90   float64 `json:"p90"`
						LongestRunning float64 `json:"longest_running"`
					}{
						{
							QueryType:      "select",
							Percentile50:   0.1,
							Percentile90:   0.5,
							LongestRunning: 10,
						},
						{
							QueryType:      "insert",
							Percentile50:   0.2,
							Percentile90:   1.5,
							LongestRunning: 100,
						},
						{
							QueryType:      "other",
							Percentile50:   0.4,
							Percentile90:   4.5,
							LongestRunning: 1000,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "text_log_exists"):
				err := enc.Encode(result{
					Data: []struct {
						TextLogExists chUInt64 `json:"text_log_exists"`
					}{
						{
							TextLogExists: 1,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "system.text_log"):
				err := enc.Encode(result{
					Data: []struct {
						Level                 string   `json:"level"`
						LastMessagesLast10Min chUInt64 `json:"messages_last_10_min"`
					}{
						{
							Level:                 "Fatal",
							LastMessagesLast10Min: 0,
						},
						{
							Level:                 "Critical",
							LastMessagesLast10Min: 10,
						},
						{
							Level:                 "Error",
							LastMessagesLast10Min: 20,
						},
						{
							Level:                 "Warning",
							LastMessagesLast10Min: 30,
						},
						{
							Level:                 "Notice",
							LastMessagesLast10Min: 40,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			}
		}))
		ch = &ClickHouse{
			Servers: []string{
				ts.URL,
			},
		}
		acc = &testutil.Accumulator{}
	)
	defer ts.Close()
	require.NoError(t, ch.Gather(acc))

	acc.AssertContainsTaggedFields(t, "clickhouse_tables",
		map[string]interface{}{
			"bytes": uint64(1),
			"parts": uint64(10),
			"rows":  uint64(100),
		},
		map[string]string{
			"source":   "127.0.0.1",
			"table":    "test_table",
			"database": "test_database",
		},
	)
	acc.AssertContainsFields(t, "clickhouse_events",
		map[string]interface{}{
			"test_system_event":  uint64(1000),
			"test_system_event2": uint64(2000),
		},
	)
	acc.AssertContainsFields(t, "clickhouse_metrics",
		map[string]interface{}{
			"test_system_metric":  uint64(1000),
			"test_system_metric2": uint64(2000),
		},
	)
	acc.AssertContainsFields(t, "clickhouse_asynchronous_metrics",
		map[string]interface{}{
			"test_system_asynchronous_metric":  float64(1000),
			"test_system_asynchronous_metric2": float64(2000),
		},
	)
	acc.AssertContainsFields(t, "clickhouse_zookeeper",
		map[string]interface{}{
			"root_nodes": uint64(2),
		},
	)
	acc.AssertContainsFields(t, "clickhouse_replication_queue",
		map[string]interface{}{
			"too_many_tries_replicas": uint64(10),
			"num_tries_replicas":      uint64(100),
		},
	)
	acc.AssertContainsFields(t, "clickhouse_detached_parts",
		map[string]interface{}{
			"detached_parts": uint64(10),
		},
	)
	acc.AssertContainsTaggedFields(t, "clickhouse_dictionaries",
		map[string]interface{}{
			"is_loaded":       uint64(0),
			"bytes_allocated": uint64(100),
		},
		map[string]string{
			"source":      "127.0.0.1",
			"dict_origin": "default.test_dict",
		},
	)
	acc.AssertContainsFields(t, "clickhouse_mutations",
		map[string]interface{}{
			"running":   uint64(1),
			"failed":    uint64(10),
			"completed": uint64(100),
		},
	)
	acc.AssertContainsTaggedFields(t, "clickhouse_disks",
		map[string]interface{}{
			"free_space_percent":      uint64(1),
			"keep_free_space_percent": uint64(10),
		},
		map[string]string{
			"source": "127.0.0.1",
			"name":   "default",
			"path":   "/var/lib/clickhouse",
		},
	)
	acc.AssertContainsTaggedFields(t, "clickhouse_processes",
		map[string]interface{}{
			"percentile_50":   0.1,
			"percentile_90":   0.5,
			"longest_running": float64(10),
		},
		map[string]string{
			"source":     "127.0.0.1",
			"query_type": "select",
		},
	)

	acc.AssertContainsTaggedFields(t, "clickhouse_processes",
		map[string]interface{}{
			"percentile_50":   0.2,
			"percentile_90":   1.5,
			"longest_running": float64(100),
		},
		map[string]string{
			"source":     "127.0.0.1",
			"query_type": "insert",
		},
	)
	acc.AssertContainsTaggedFields(t, "clickhouse_processes",
		map[string]interface{}{
			"percentile_50":   0.4,
			"percentile_90":   4.5,
			"longest_running": float64(1000),
		},
		map[string]string{
			"source":     "127.0.0.1",
			"query_type": "other",
		},
	)

	for i, level := range []string{"Fatal", "Critical", "Error", "Warning", "Notice"} {
		acc.AssertContainsTaggedFields(t, "clickhouse_text_log",
			map[string]interface{}{
				"messages_last_10_min": uint64(i * 10),
			},
			map[string]string{
				"source": "127.0.0.1",
				"level":  level,
			},
		)
	}
}

func TestGatherWithSomeTablesNotExists(t *testing.T) {
	var (
		ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			type result struct {
				Data interface{} `json:"data"`
			}
			enc := json.NewEncoder(w)
			switch query := r.URL.Query().Get("query"); {
			case strings.Contains(query, "zk_exists"):
				err := enc.Encode(result{
					Data: []struct {
						ZkExists chUInt64 `json:"zk_exists"`
					}{
						{
							ZkExists: 0,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "replication_queue_exists"):
				err := enc.Encode(result{
					Data: []struct {
						ReplicationQueueExists chUInt64 `json:"replication_queue_exists"`
					}{
						{
							ReplicationQueueExists: 0,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			case strings.Contains(query, "text_log_exists"):
				err := enc.Encode(result{
					Data: []struct {
						TextLogExists chUInt64 `json:"text_log_exists"`
					}{
						{
							TextLogExists: 0,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			}
		}))
		ch = &ClickHouse{
			Servers: []string{
				ts.URL,
			},
			Username: "default",
		}
		acc = &testutil.Accumulator{}
	)
	defer ts.Close()
	require.NoError(t, ch.Gather(acc))

	acc.AssertDoesNotContainMeasurement(t, "clickhouse_zookeeper")
	acc.AssertDoesNotContainMeasurement(t, "clickhouse_replication_queue")
	acc.AssertDoesNotContainMeasurement(t, "clickhouse_text_log")
}

func TestGatherClickhouseCloud(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		type result struct {
			Data interface{} `json:"data"`
		}
		enc := json.NewEncoder(w)
		switch query := r.URL.Query().Get("query"); {
		case strings.Contains(query, "zk_exists"):
			err := enc.Encode(result{
				Data: []struct {
					ZkExists chUInt64 `json:"zk_exists"`
				}{
					{
						ZkExists: 1,
					},
				},
			})
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		case strings.Contains(query, "zk_root_nodes"):
			err := enc.Encode(result{
				Data: []struct {
					ZkRootNodes chUInt64 `json:"zk_root_nodes"`
				}{
					{
						ZkRootNodes: 2,
					},
				},
			})
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		}
	}))
	defer ts.Close()

	ch := &ClickHouse{
		Servers: []string{ts.URL},
		Variant: "managed",
	}
	acc := &testutil.Accumulator{}
	require.NoError(t, ch.Gather(acc))
	acc.AssertDoesNotContainMeasurement(t, "clickhouse_zookeeper")
}

func TestWrongJSONMarshalling(t *testing.T) {
	var (
		ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			type result struct {
				Data interface{} `json:"data"`
			}
			enc := json.NewEncoder(w)
			// wrong data section json
			err := enc.Encode(result{
				Data: make([]struct{}, 0),
			})
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		}))
		ch = &ClickHouse{
			Servers: []string{
				ts.URL,
			},
			Username: "default",
		}
		acc = &testutil.Accumulator{}
	)
	defer ts.Close()
	require.NoError(t, ch.Gather(acc))

	require.Empty(t, acc.Metrics)
	allMeasurements := []string{
		"clickhouse_events",
		"clickhouse_metrics",
		"clickhouse_asynchronous_metrics",
		"clickhouse_tables",
		"clickhouse_zookeeper",
		"clickhouse_replication_queue",
		"clickhouse_detached_parts",
		"clickhouse_dictionaries",
		"clickhouse_mutations",
		"clickhouse_disks",
		"clickhouse_processes",
		"clickhouse_text_log",
	}
	require.GreaterOrEqual(t, len(allMeasurements), len(acc.Errors))
}

func TestOfflineServer(t *testing.T) {
	var (
		acc = &testutil.Accumulator{}
		ch  = &ClickHouse{
			Servers: []string{
				"http://wrong-domain.local:8123",
			},
			Username: "default",
			HTTPClient: http.Client{
				Timeout: 1 * time.Millisecond,
			},
		}
	)
	require.NoError(t, ch.Gather(acc))

	require.Empty(t, acc.Metrics)
	allMeasurements := []string{
		"clickhouse_events",
		"clickhouse_metrics",
		"clickhouse_asynchronous_metrics",
		"clickhouse_tables",
		"clickhouse_zookeeper",
		"clickhouse_replication_queue",
		"clickhouse_detached_parts",
		"clickhouse_dictionaries",
		"clickhouse_mutations",
		"clickhouse_disks",
		"clickhouse_processes",
		"clickhouse_text_log",
	}
	require.GreaterOrEqual(t, len(allMeasurements), len(acc.Errors))
}

func TestAutoDiscovery(t *testing.T) {
	var (
		ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			type result struct {
				Data interface{} `json:"data"`
			}
			enc := json.NewEncoder(w)
			query := r.URL.Query().Get("query")
			if strings.Contains(query, "system.clusters") {
				err := enc.Encode(result{
					Data: []struct {
						Cluster  string   `json:"test"`
						Hostname string   `json:"localhost"`
						ShardNum chUInt64 `json:"shard_num"`
					}{
						{
							Cluster:  "test_database",
							Hostname: "test_table",
							ShardNum: 1,
						},
					},
				})
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			}
		}))
		ch = &ClickHouse{
			Servers: []string{
				ts.URL,
			},
			Username:      "default",
			AutoDiscovery: true,
		}
		acc = &testutil.Accumulator{}
	)
	defer ts.Close()
	require.NoError(t, ch.Gather(acc))
}
