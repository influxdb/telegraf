package marklogic

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestMarklogic(t *testing.T) {
	// Create a test server with the const response JSON
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := fmt.Fprintln(w, response); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer ts.Close()

	// Parse the URL of the test server, used to verify the expected host
	_, err := url.Parse(ts.URL)
	require.NoError(t, err)

	// Create a new Marklogic instance with our given test server

	ml := &Marklogic{
		Hosts: []string{"example1"},
		URL:   ts.URL,
		// sources: []string{"http://localhost:8002/manage/v2/hosts/hostname1?view=status&format=json"},
	}

	// Create a test accumulator
	acc := &testutil.Accumulator{}

	// Init() call to parse all source URL's
	err = ml.Init()
	require.NoError(t, err)

	// Gather data from the test server
	err = ml.Gather(acc)
	require.NoError(t, err)

	// Expect the correct values for all known keys
	expectFields := map[string]interface{}{
		"online":                    true,
		"total_load":                0.00429263804107904,
		"ncpus":                     1,
		"ncores":                    4,
		"total_rate":                15.6527042388916,
		"total_cpu_stat_user":       0.276381999254227,
		"total_cpu_stat_system":     0.636515974998474,
		"total_cpu_stat_idle":       99.0578002929688,
		"total_cpu_stat_iowait":     0.0125628001987934,
		"memory_process_size":       1234,
		"memory_process_rss":        815,
		"memory_system_total":       3947,
		"memory_system_free":        2761,
		"memory_process_swap_size":  0,
		"memory_size":               4096,
		"host_size":                 64,
		"log_device_space":          34968,
		"data_dir_space":            34968,
		"query_read_bytes":          11492428,
		"query_read_load":           0,
		"merge_read_load":           0,
		"merge_write_load":          0,
		"http_server_receive_bytes": 285915,
		"http_server_send_bytes":    0,
	}
	// Expect the correct values for all tags
	expectTags := map[string]string{
		"source": "ml1.local",
		"id":     "2592913110757471141",
	}

	acc.AssertContainsTaggedFields(t, "marklogic", expectFields, expectTags)
}

var response = `
{
  "host-status": {
    "id": "2592913110757471141",
    "name": "ml1.local",
    "version": "10.0-1",
    "effective-version": 10000100,
    "host-mode": "normal",
    "host-mode-description": "",
    "meta": {
      "uri": "/manage/v2/hosts/ml1.local?view=status",
      "current-time": "2019-07-28T22:32:19.056203Z",
      "elapsed-time": {
        "units": "sec",
        "value": 0.013035
      }
    },
    "relations": {
      "relation-group": [
        {
          "uriref": "/manage/v2/forests?view=status&host-id=ml1.local",
          "typeref": "forests",
          "relation": [
            {
              "uriref": "/manage/v2/forests/App-Services",
              "idref": "8573569457346659714",
              "nameref": "App-Services"
            },
            {
              "uriref": "/manage/v2/forests/Documents",
              "idref": "17189472171231792168",
              "nameref": "Documents"
            },
            {
              "uriref": "/manage/v2/forests/Extensions",
              "idref": "1510244530748962553",
              "nameref": "Extensions"
            },
            {
              "uriref": "/manage/v2/forests/Fab",
              "idref": "16221965829238302106",
              "nameref": "Fab"
            },
            {
              "uriref": "/manage/v2/forests/Last-Login",
              "idref": "1093671762706318022",
              "nameref": "Last-Login"
            },
            {
              "uriref": "/manage/v2/forests/Meters",
              "idref": "1573439446779995954",
              "nameref": "Meters"
            },
            {
              "uriref": "/manage/v2/forests/Modules",
              "idref": "18320951141685848719",
              "nameref": "Modules"
            },
            {
              "uriref": "/manage/v2/forests/Schemas",
              "idref": "18206720449696085936",
              "nameref": "Schemas"
            },
            {
              "uriref": "/manage/v2/forests/Security",
              "idref": "9348728036360382939",
              "nameref": "Security"
            },
            {
              "uriref": "/manage/v2/forests/Triggers",
              "idref": "10142793547905338229",
              "nameref": "Triggers"
            }
          ]
        },
        {
          "typeref": "groups",
          "relation": [
            {
              "uriref": "/manage/v2/groups/Default?view=status",
              "idref": "16808579782544283978",
              "nameref": "Default"
            }
          ]
        }
      ]
    },
    "status-properties": {
      "online": {
        "units": "bool",
        "value": true
      },
      "secure": {
        "units": "bool",
        "value": false
      },
      "cache-properties": {
        "cache-detail": {
          "compressed-tree-cache-partition": [
            {
              "partition-size": 64,
              "partition-table": 3.40000009536743,
              "partition-used": 29.7000007629395,
              "partition-free": 70.1999969482422,
              "partition-overhead": 0.100000001490116
            }
          ],
          "expanded-tree-cache-partition": [
            {
              "partition-size": 128,
              "partition-table": 6.19999980926514,
              "partition-busy": 0,
              "partition-used": 87.3000030517578,
              "partition-free": 12.3999996185303,
              "partition-overhead": 0.300000011920929
            }
          ],
          "triple-cache-partition": [
            {
              "partition-size": 64,
              "partition-busy": 0,
              "partition-used": 0,
              "partition-free": 100
            }
          ],
          "triple-value-cache-partition": [
            {
              "partition-size": 128,
              "partition-busy": 0,
              "partition-used": 0,
              "partition-free": 100,
              "value-count": 0,
              "value-bytes-total": 0,
              "value-bytes-average": 0
            }
          ]
        }
      },
      "load-properties": {
        "total-load": {
          "units": "sec/sec",
          "value": 0.00429263804107904
        },
        "load-detail": {
          "query-read-load": {
            "units": "sec/sec",
            "value": 0
          },
          "journal-write-load": {
            "units": "sec/sec",
            "value": 0
          },
          "save-write-load": {
            "units": "sec/sec",
            "value": 0
          },
          "merge-read-load": {
            "units": "sec/sec",
            "value": 0
          },
          "merge-write-load": {
            "units": "sec/sec",
            "value": 0
          },
          "backup-read-load": {
            "units": "sec/sec",
            "value": 0
          },
          "backup-write-load": {
            "units": "sec/sec",
            "value": 0
          },
          "restore-read-load": {
            "units": "sec/sec",
            "value": 0
          },
          "restore-write-load": {
            "units": "sec/sec",
            "value": 0
          },
          "large-read-load": {
            "units": "sec/sec",
            "value": 0
          },
          "large-write-load": {
            "units": "sec/sec",
            "value": 0
          },
          "external-binary-read-load": {
            "units": "sec/sec",
            "value": 0
          },
          "xdqp-client-receive-load": {
            "units": "sec/sec",
            "value": 0
          },
          "xdqp-client-send-load": {
            "units": "sec/sec",
            "value": 0
          },
          "xdqp-server-receive-load": {
            "units": "sec/sec",
            "value": 0
          },
          "xdqp-server-send-load": {
            "units": "sec/sec",
            "value": 0
          },
          "foreign-xdqp-client-receive-load": {
            "units": "sec/sec",
            "value": 0
          },
          "foreign-xdqp-client-send-load": {
            "units": "sec/sec",
            "value": 0
          },
          "foreign-xdqp-server-receive-load": {
            "units": "sec/sec",
            "value": 0
          },
          "foreign-xdqp-server-send-load": {
            "units": "sec/sec",
            "value": 0
          },
          "read-lock-wait-load": {
            "units": "sec/sec",
            "value": 0
          },
          "read-lock-hold-load": {
            "units": "sec/sec",
            "value": 0
          },
          "write-lock-wait-load": {
            "units": "sec/sec",
            "value": 0
          },
          "write-lock-hold-load": {
            "units": "sec/sec",
            "value": 0.00429263804107904
          },
          "deadlock-wait-load": {
            "units": "sec/sec",
            "value": 0
          }
        }
      },
      "rate-properties": {
        "total-rate": {
          "units": "MB/sec",
          "value": 15.6527042388916
        },
        "rate-detail": {
          "memory-system-pagein-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "memory-system-pageout-rate": {
            "units": "MB/sec",
            "value": 15.6420001983643
          },
          "memory-system-swapin-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "memory-system-swapout-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "query-read-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "journal-write-rate": {
            "units": "MB/sec",
            "value": 0.00372338597662747
          },
          "save-write-rate": {
            "units": "MB/sec",
            "value": 0.0024786819703877
          },
          "merge-read-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "merge-write-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "backup-read-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "backup-write-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "restore-read-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "restore-write-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "large-read-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "large-write-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "external-binary-read-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "xdqp-client-receive-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "xdqp-client-send-rate": {
            "units": "MB/sec",
            "value": 0.00293614692054689
          },
          "xdqp-server-receive-rate": {
            "units": "MB/sec",
            "value": 0.00156576896551996
          },
          "xdqp-server-send-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "foreign-xdqp-client-receive-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "foreign-xdqp-client-send-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "foreign-xdqp-server-receive-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "foreign-xdqp-server-send-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "read-lock-rate": {
            "units": "MB/sec",
            "value": 0
          },
          "write-lock-rate": {
            "units": "MB/sec",
            "value": 0.251882910728455
          },
          "deadlock-rate": {
            "units": "MB/sec",
            "value": 0
          }
        }
      },
      "status-detail": {
        "bind-port": 7999,
        "connect-port": 7999,
        "ssl-fips-enabled": {
          "units": "bool",
          "value": true
        },
        "foreign-bind-port": 7998,
        "foreign-connect-port": 7998,
        "background-io-limit": {
          "units": "quantity",
          "value": 0
        },
        "metering-enabled": {
          "units": "bool",
          "value": true
        },
        "meters-database": {
          "units": "quantity",
          "value": "11952918530142281790"
        },
        "performance-metering-enabled": {
          "units": "bool",
          "value": true
        },
        "performance-metering-period": {
          "units": "second",
          "value": 60
        },
        "performance-metering-retain-raw": {
          "units": "day",
          "value": 7
        },
        "performance-metering-retain-hourly": {
          "units": "day",
          "value": 30
        },
        "performance-metering-retain-daily": {
          "units": "day",
          "value": 90
        },
        "last-startup": {
          "units": "datetime",
          "value": "2019-07-26T17:23:36.412644Z"
        },
        "version": "10.0-1",
        "effective-version": {
          "units": "quantity",
          "value": 10000100
        },
        "software-version": {
          "units": "quantity",
          "value": 10000100
        },
        "os-version": "NA",
        "converters-version": "10.0-1",
        "host-mode": {
          "units": "enum",
          "value": "normal"
        },
        "architecture": "x86_64",
        "platform": "linux",
        "license-key": "000-000-000-000-000-000-000",
        "licensee": "NA",
        "license-key-expires": {
          "units": "datetime",
          "value": "2999-01-23T00:00:00Z"
        },
        "license-key-cpus": {
          "units": "quantity",
          "value": 0
        },
        "license-key-cores": {
          "units": "quantity",
          "value": 0
        },
        "license-key-size": {
          "units": "MB",
          "value": 0
        },
        "license-key-option": [
          {
            "units": "enum",
            "value": "conversion"
          },
          {
            "units": "enum",
            "value": "failover"
          },
          {
            "units": "enum",
            "value": "alerting"
          },
          {
            "units": "enum",
            "value": "geospatial"
          },
          {
            "units": "enum",
            "value": "flexible replication"
          },
          {
            "units": "enum",
            "value": "tiered storage"
          },
          {
            "units": "enum",
            "value": "semantics"
          },
          {
            "units": "enum",
            "value": "French"
          },
          {
            "units": "enum",
            "value": "Italian"
          },
          {
            "units": "enum",
            "value": "German"
          },
          {
            "units": "enum",
            "value": "Spanish"
          },
          {
            "units": "enum",
            "value": "Traditional Chinese"
          },
          {
            "units": "enum",
            "value": "Simplified Chinese"
          },
          {
            "units": "enum",
            "value": "Arabic"
          },
          {
            "units": "enum",
            "value": "Russian"
          },
          {
            "units": "enum",
            "value": "Dutch"
          },
          {
            "units": "enum",
            "value": "Korean"
          },
          {
            "units": "enum",
            "value": "Persian"
          },
          {
            "units": "enum",
            "value": "Japanese"
          },
          {
            "units": "enum",
            "value": "Portuguese"
          },
          {
            "units": "enum",
            "value": "English"
          }
        ],
        "edition": {
          "units": "enum",
          "value": "Enterprise Edition"
        },
        "environment": {
          "units": "enum",
          "value": "developer"
        },
        "cpus": {
          "units": "quantity",
          "value": 1
        },
        "cores": {
          "units": "quantity",
          "value": 4
        },
        "core-threads": {
          "units": "quantity",
          "value": 4
        },
        "total-cpu-stat-user": 0.276381999254227,
        "total-cpu-stat-nice": 0,
        "total-cpu-stat-system": 0.636515974998474,
        "total-cpu-stat-idle": 99.0578002929688,
        "total-cpu-stat-iowait": 0.0125628001987934,
        "total-cpu-stat-irq": 0,
        "total-cpu-stat-softirq": 0.0167504008859396,
        "total-cpu-stat-steal": 0,
        "total-cpu-stat-guest": 0,
        "total-cpu-stat-guest-nice": 0,
        "memory-process-size": {
          "units": "fraction",
          "value": 1234
        },
        "memory-process-rss": {
          "units": "fraction",
          "value": 815
        },
        "memory-process-anon": {
          "units": "fraction",
          "value": 743
        },
        "memory-process-rss-hwm": {
          "units": "fraction",
          "value": 1072
        },
        "memory-process-swap-size": {
          "units": "fraction",
          "value": 0
        },
        "memory-process-huge-pages-size": {
          "units": "fraction",
          "value": 0
        },
        "memory-system-total": {
          "units": "fraction",
          "value": 3947
        },
        "memory-system-free": {
          "units": "fraction",
          "value": 2761
        },
        "memory-system-pagein-rate": {
          "units": "fraction",
          "value": 0
        },
        "memory-system-pageout-rate": {
          "units": "fraction",
          "value": 15.6420001983643
        },
        "memory-system-swapin-rate": {
          "units": "fraction",
          "value": 0
        },
        "memory-system-swapout-rate": {
          "units": "fraction",
          "value": 0
        },
        "memory-size": {
          "units": "quantity",
          "value": 4096
        },
        "memory-file-size": {
          "units": "quantity",
          "value": 5
        },
        "memory-forest-size": {
          "units": "quantity",
          "value": 849
        },
        "memory-unclosed-size": {
          "units": "quantity",
          "value": 0
        },
        "memory-cache-size": {
          "units": "quantity",
          "value": 320
        },
        "memory-registry-size": {
          "units": "quantity",
          "value": 1
        },
        "memory-join-size": {
          "units": "quantity",
          "value": 0
        },
        "host-size": {
          "units": "MB",
          "value": 64
        },
        "host-large-data-size": {
          "units": "MB",
          "value": 0
        },
        "log-device-space": {
          "units": "MB",
          "value": 34968
        },
        "data-dir-space": {
          "units": "MB",
          "value": 34968
        },
        "query-read-bytes": {
          "units": "bytes",
          "value": 11492428
        },
        "query-read-time": {
          "units": "time",
          "value": "PT0.141471S"
        },
        "query-read-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "query-read-load": {
          "units": "",
          "value": 0
        },
        "journal-write-bytes": {
          "units": "bytes",
          "value": 285717868
        },
        "journal-write-time": {
          "units": "time",
          "value": "PT17.300832S"
        },
        "journal-write-rate": {
          "units": "MB/sec",
          "value": 0.00372338597662747
        },
        "journal-write-load": {
          "units": "",
          "value": 0
        },
        "save-write-bytes": {
          "units": "bytes",
          "value": 95818597
        },
        "save-write-time": {
          "units": "time",
          "value": "PT2.972855S"
        },
        "save-write-rate": {
          "units": "MB/sec",
          "value": 0.0024786819703877
        },
        "save-write-load": {
          "units": "",
          "value": 0
        },
        "merge-read-bytes": {
          "units": "bytes",
          "value": 55374848
        },
        "merge-read-time": {
          "units": "time",
          "value": "PT0.535705S"
        },
        "merge-read-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "merge-read-load": {
          "units": "",
          "value": 0
        },
        "merge-write-bytes": {
          "units": "bytes",
          "value": 146451731
        },
        "merge-write-time": {
          "units": "time",
          "value": "PT5.392288S"
        },
        "merge-write-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "merge-write-load": {
          "units": "",
          "value": 0
        },
        "backup-read-bytes": {
          "units": "bytes",
          "value": 0
        },
        "backup-read-time": {
          "units": "time",
          "value": "PT0S"
        },
        "backup-read-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "backup-read-load": {
          "units": "",
          "value": 0
        },
        "backup-write-bytes": {
          "units": "bytes",
          "value": 0
        },
        "backup-write-time": {
          "units": "time",
          "value": "PT0S"
        },
        "backup-write-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "backup-write-load": {
          "units": "",
          "value": 0
        },
        "restore-read-bytes": {
          "units": "bytes",
          "value": 0
        },
        "restore-read-time": {
          "units": "time",
          "value": "PT0S"
        },
        "restore-read-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "restore-read-load": {
          "units": "",
          "value": 0
        },
        "restore-write-bytes": {
          "units": "bytes",
          "value": 0
        },
        "restore-write-time": {
          "units": "time",
          "value": "PT0S"
        },
        "restore-write-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "restore-write-load": {
          "units": "",
          "value": 0
        },
        "large-read-bytes": {
          "units": "bytes",
          "value": 0
        },
        "large-read-time": {
          "units": "time",
          "value": "PT0S"
        },
        "large-read-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "large-read-load": {
          "units": "",
          "value": 0
        },
        "large-write-bytes": {
          "units": "bytes",
          "value": 0
        },
        "large-write-time": {
          "units": "time",
          "value": "PT0S"
        },
        "large-write-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "large-write-load": {
          "units": "",
          "value": 0
        },
        "external-binary-read-bytes": {
          "units": "bytes",
          "value": 0
        },
        "external-binary-read-time": {
          "units": "time",
          "value": "PT0S"
        },
        "external-binary-read-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "external-binary-read-load": {
          "units": "",
          "value": 0
        },
        "webDAV-server-receive-bytes": {
          "units": "bytes",
          "value": 0
        },
        "webDAV-server-receive-time": {
          "units": "sec",
          "value": "PT0S"
        },
        "webDAV-server-receive-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "webDAV-server-receive-load": {
          "units": "",
          "value": 0
        },
        "webDAV-server-send-bytes": {
          "units": "bytes",
          "value": 0
        },
        "webDAV-server-send-time": {
          "units": "sec",
          "value": "PT0S"
        },
        "webDAV-server-send-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "webDAV-server-send-load": {
          "units": "",
          "value": 0
        },
        "http-server-receive-bytes": {
          "units": "bytes",
          "value": 285915
        },
        "http-server-receive-time": {
          "units": "sec",
          "value": "PT0.02028S"
        },
        "http-server-receive-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "http-server-receive-load": {
          "units": "",
          "value": 0
        },
        "http-server-send-bytes": {
          "units": "bytes",
          "value": 0
        },
        "http-server-send-time": {
          "units": "sec",
          "value": "PT0S"
        },
        "http-server-send-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "http-server-send-load": {
          "units": "",
          "value": 0
        },
        "xdbc-server-receive-bytes": {
          "units": "bytes",
          "value": 0
        },
        "xdbc-server-receive-time": {
          "units": "sec",
          "value": "PT0S"
        },
        "xdbc-server-receive-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "xdbc-server-receive-load": {
          "units": "",
          "value": 0
        },
        "xdbc-server-send-bytes": {
          "units": "bytes",
          "value": 0
        },
        "xdbc-server-send-time": {
          "units": "sec",
          "value": "PT0S"
        },
        "xdbc-server-send-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "xdbc-server-send-load": {
          "units": "",
          "value": 0
        },
        "odbc-server-receive-bytes": {
          "units": "bytes",
          "value": 0
        },
        "odbc-server-receive-time": {
          "units": "sec",
          "value": "PT0S"
        },
        "odbc-server-receive-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "odbc-server-receive-load": {
          "units": "",
          "value": 0
        },
        "odbc-server-send-bytes": {
          "units": "bytes",
          "value": 0
        },
        "odbc-server-send-time": {
          "units": "sec",
          "value": "PT0S"
        },
        "odbc-server-send-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "odbc-server-send-load": {
          "units": "",
          "value": 0
        },
        "xdqp-client-receive-bytes": {
          "units": "bytes",
          "value": 3020032
        },
        "xdqp-client-receive-time": {
          "units": "time",
          "value": "PT0.046612S"
        },
        "xdqp-client-receive-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "xdqp-client-receive-load": {
          "units": "",
          "value": 0
        },
        "xdqp-client-send-bytes": {
          "units": "bytes",
          "value": 163513952
        },
        "xdqp-client-send-time": {
          "units": "time",
          "value": "PT22.700289S"
        },
        "xdqp-client-send-rate": {
          "units": "MB/sec",
          "value": 0.00293614692054689
        },
        "xdqp-client-send-load": {
          "units": "",
          "value": 0
        },
        "xdqp-server-receive-bytes": {
          "units": "bytes",
          "value": 131973888
        },
        "xdqp-server-receive-time": {
          "units": "time",
          "value": "PT3.474521S"
        },
        "xdqp-server-receive-rate": {
          "units": "MB/sec",
          "value": 0.00156576896551996
        },
        "xdqp-server-receive-load": {
          "units": "",
          "value": 0
        },
        "xdqp-server-send-bytes": {
          "units": "bytes",
          "value": 10035300
        },
        "xdqp-server-send-time": {
          "units": "time",
          "value": "PT4.275597S"
        },
        "xdqp-server-send-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "xdqp-server-send-load": {
          "units": "",
          "value": 0
        },
        "xdqp-server-request-time": {
          "units": "milliseconds",
          "value": 0.743777990341187
        },
        "xdqp-server-request-rate": {
          "units": "requests/sec",
          "value": 0.371862411499023
        },
        "foreign-xdqp-client-receive-bytes": {
          "units": "bytes",
          "value": 0
        },
        "foreign-xdqp-client-receive-time": {
          "units": "time",
          "value": "PT0S"
        },
        "foreign-xdqp-client-receive-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "foreign-xdqp-client-receive-load": {
          "units": "",
          "value": 0
        },
        "foreign-xdqp-client-send-bytes": {
          "units": "bytes",
          "value": 0
        },
        "foreign-xdqp-client-send-time": {
          "units": "time",
          "value": "PT0S"
        },
        "foreign-xdqp-client-send-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "foreign-xdqp-client-send-load": {
          "units": "",
          "value": 0
        },
        "foreign-xdqp-server-receive-bytes": {
          "units": "bytes",
          "value": 0
        },
        "foreign-xdqp-server-receive-time": {
          "units": "time",
          "value": "PT0S"
        },
        "foreign-xdqp-server-receive-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "foreign-xdqp-server-receive-load": {
          "units": "",
          "value": 0
        },
        "foreign-xdqp-server-send-bytes": {
          "units": "bytes",
          "value": 0
        },
        "foreign-xdqp-server-send-time": {
          "units": "time",
          "value": "PT0S"
        },
        "foreign-xdqp-server-send-rate": {
          "units": "MB/sec",
          "value": 0
        },
        "foreign-xdqp-server-send-load": {
          "units": "",
          "value": 0
        },
        "read-lock-count": {
          "units": "locks",
          "value": 104
        },
        "read-lock-wait-time": {
          "units": "seconds",
          "value": "PT0.001464S"
        },
        "read-lock-hold-time": {
          "units": "seconds",
          "value": "PT3.022913S"
        },
        "read-lock-rate": {
          "units": "locks/sec",
          "value": 0
        },
        "read-lock-wait-load": {
          "units": "",
          "value": 0
        },
        "read-lock-hold-load": {
          "units": "",
          "value": 0
        },
        "write-lock-count": {
          "units": "locks",
          "value": 15911
        },
        "write-lock-wait-time": {
          "units": "seconds",
          "value": "PT0.317098S"
        },
        "write-lock-hold-time": {
          "units": "seconds",
          "value": "PT11M46.9923759S"
        },
        "write-lock-rate": {
          "units": "locks/sec",
          "value": 0.251882910728455
        },
        "write-lock-wait-load": {
          "units": "",
          "value": 0
        },
        "write-lock-hold-load": {
          "units": "",
          "value": 0.00429263804107904
        },
        "deadlock-count": {
          "units": "locks",
          "value": 0
        },
        "deadlock-wait-time": {
          "units": "seconds",
          "value": "PT0S"
        },
        "deadlock-rate": {
          "units": "locks/sec",
          "value": 0
        },
        "deadlock-wait-load": {
          "units": "",
          "value": 0
        },
        "external-kms-request-rate": {
          "units": "requests/sec",
          "value": 0
        },
        "external-kms-request-time": {
          "units": "milliseconds",
          "value": 0
        },
        "keystore-status": "normal",
        "ldap-request-rate": {
          "units": "requests/sec",
          "value": 0
        },
        "ldap-request-time": {
          "units": "milliseconds",
          "value": 0
        }
      }
    },
    "related-views": {
      "related-view": [
        {
          "view-type": "item",
          "view-name": "default",
          "view-uri": "/manage/v2/hosts/example"
        }
      ]
    }
  }
}
`
