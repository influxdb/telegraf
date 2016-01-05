package nsq

import (
        "fmt"
        "net/http"
        "net/http/httptest"
        "testing"
	"net/url"

	"github.com/influxdb/telegraf/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNSQStats(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, response)
	}))
	defer ts.Close()

	n := &NSQ{
		Endpoints: []string{ts.URL},
	}

	var acc testutil.Accumulator
	err := n.Gather(&acc)
	require.NoError(t, err)

	u, err := url.Parse(ts.URL)
	require.NoError(t, err)
	host := u.Host

	// actually validate the tests
	tests := []struct {
		m string
		v int64
		g map[string]string
	}{
		{`nsq_server_count`, int64(1), map[string]string{`server_host`:host, `server_version`:`0.3.6`}},
		{`nsq_server_topic_count`, int64(2), map[string]string{`server_host`:host, `server_version`:`0.3.6`}},
		{`nsq_topic_depth`, int64(12), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`}},
		{`nsq_topic_backend_depth`, int64(13), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`}},
		{`nsq_topic_message_count`, int64(14), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`}},
		{`nsq_topic_channel_count`, int64(1), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`}},
		{`nsq_channel_depth`, int64(0), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`}},
		{`nsq_channel_backend_depth`, int64(1), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`}},
		{`nsq_channel_inflight_count`, int64(2), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`}},
		{`nsq_channel_deferred_count`, int64(3), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`}},
		{`nsq_channel_message_count`, int64(4), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`}},
		{`nsq_channel_requeue_count`, int64(5), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`}},
		{`nsq_channel_timeout_count`, int64(6), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`}},
		{`nsq_channel_client_count`, int64(1), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`}},
		{`nsq_client_ready_count`, int64(200), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`, `client_name`:`373a715cd990`, `client_id`:`373a715cd990`, `client_hostname`:`373a715cd990`, `client_version`:`V2`, `client_address`:`172.17.0.11:35560`, `client_tls`:`false`, `client_snappy`:`false`, `client_deflate`:`false`, `client_user_agent`:`nsq_to_nsq/0.3.6 go-nsq/1.0.5`}},
		{`nsq_client_inflight_count`, int64(7), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`, `client_name`:`373a715cd990`, `client_id`:`373a715cd990`, `client_hostname`:`373a715cd990`, `client_version`:`V2`, `client_address`:`172.17.0.11:35560`, `client_tls`:`false`, `client_snappy`:`false`, `client_deflate`:`false`, `client_user_agent`:`nsq_to_nsq/0.3.6 go-nsq/1.0.5`}},
		{`nsq_client_message_count`, int64(8), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`, `client_name`:`373a715cd990`, `client_id`:`373a715cd990`, `client_hostname`:`373a715cd990`, `client_version`:`V2`, `client_address`:`172.17.0.11:35560`, `client_tls`:`false`, `client_snappy`:`false`, `client_deflate`:`false`, `client_user_agent`:`nsq_to_nsq/0.3.6 go-nsq/1.0.5`}},
		{`nsq_client_finish_count`, int64(9), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`, `client_name`:`373a715cd990`, `client_id`:`373a715cd990`, `client_hostname`:`373a715cd990`, `client_version`:`V2`, `client_address`:`172.17.0.11:35560`, `client_tls`:`false`, `client_snappy`:`false`, `client_deflate`:`false`, `client_user_agent`:`nsq_to_nsq/0.3.6 go-nsq/1.0.5`}},
		{`nsq_client_requeue_count`, int64(10), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t1`, `channel`:`c1`, `client_name`:`373a715cd990`, `client_id`:`373a715cd990`, `client_hostname`:`373a715cd990`, `client_version`:`V2`, `client_address`:`172.17.0.11:35560`, `client_tls`:`false`, `client_snappy`:`false`, `client_deflate`:`false`, `client_user_agent`:`nsq_to_nsq/0.3.6 go-nsq/1.0.5`}},
		{`nsq_topic_depth`, int64(28), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`}},
		{`nsq_topic_backend_depth`, int64(29), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`}},
		{`nsq_topic_message_count`, int64(30), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`}},
		{`nsq_topic_channel_count`, int64(1), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`}},
		{`nsq_channel_depth`, int64(15), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`}},
		{`nsq_channel_backend_depth`, int64(16), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`}},
		{`nsq_channel_inflight_count`, int64(17), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`}},
		{`nsq_channel_deferred_count`, int64(18), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`}},
		{`nsq_channel_message_count`, int64(19), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`}},
		{`nsq_channel_requeue_count`, int64(20), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`}},
		{`nsq_channel_timeout_count`, int64(21), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`}},
		{`nsq_channel_client_count`, int64(1), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`}},
		{`nsq_client_ready_count`, int64(22), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`, `client_name`:`377569bd462b`, `client_id`:`377569bd462b`, `client_hostname`:`377569bd462b`, `client_version`:`V2`, `client_address`:`172.17.0.8:48145`, `client_user_agent`:`go-nsq/1.0.5`, `client_tls`:`true`, `client_snappy`:`true`, `client_deflate`:`true`}},
		{`nsq_client_inflight_count`, int64(23), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`, `client_name`:`377569bd462b`, `client_id`:`377569bd462b`, `client_hostname`:`377569bd462b`, `client_version`:`V2`, `client_address`:`172.17.0.8:48145`, `client_user_agent`:`go-nsq/1.0.5`, `client_tls`:`true`, `client_snappy`:`true`, `client_deflate`:`true`}},
		{`nsq_client_message_count`, int64(24), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`, `client_name`:`377569bd462b`, `client_id`:`377569bd462b`, `client_hostname`:`377569bd462b`, `client_version`:`V2`, `client_address`:`172.17.0.8:48145`, `client_user_agent`:`go-nsq/1.0.5`, `client_tls`:`true`, `client_snappy`:`true`, `client_deflate`:`true`}},
		{`nsq_client_finish_count`, int64(25), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`, `client_name`:`377569bd462b`, `client_id`:`377569bd462b`, `client_hostname`:`377569bd462b`, `client_version`:`V2`, `client_address`:`172.17.0.8:48145`, `client_user_agent`:`go-nsq/1.0.5`, `client_tls`:`true`, `client_snappy`:`true`, `client_deflate`:`true`}},
		{`nsq_client_requeue_count`, int64(26), map[string]string{`server_host`:host, `server_version`:`0.3.6`, `topic`:`t2`, `channel`:`c2`, `client_name`:`377569bd462b`, `client_id`:`377569bd462b`, `client_hostname`:`377569bd462b`, `client_version`:`V2`, `client_address`:`172.17.0.8:48145`, `client_user_agent`:`go-nsq/1.0.5`, `client_tls`:`true`, `client_snappy`:`true`, `client_deflate`:`true`}},
	}

	for _, test := range tests {
		assert.True(t, acc.CheckTaggedValue(test.m, test.v, test.g), "Failed expectation: (\"%v\", \"%v\", \"%v\")", test.m, test.v, fmt.Sprint(test.g))
	}
}

var response = `
{
  "status_code": 200,
  "status_txt": "OK",
  "data": {
    "version": "0.3.6",
    "health": "OK",
    "start_time": 1452021674,
    "topics": [
      {
        "topic_name": "t1",
        "channels": [
          {
            "channel_name": "c1",
            "depth": 0,
            "backend_depth": 1,
            "in_flight_count": 2,
            "deferred_count": 3,
            "message_count": 4,
            "requeue_count": 5,
            "timeout_count": 6,
            "clients": [
              {
                "name": "373a715cd990",
                "client_id": "373a715cd990",
                "hostname": "373a715cd990",
                "version": "V2",
                "remote_address": "172.17.0.11:35560",
                "state": 3,
                "ready_count": 200,
                "in_flight_count": 7,
                "message_count": 8,
                "finish_count": 9,
                "requeue_count": 10,
                "connect_ts": 1452021675,
                "sample_rate": 11,
                "deflate": false,
                "snappy": false,
                "user_agent": "nsq_to_nsq\/0.3.6 go-nsq\/1.0.5",
                "tls": false,
                "tls_cipher_suite": "",
                "tls_version": "",
                "tls_negotiated_protocol": "",
                "tls_negotiated_protocol_is_mutual": false
              }
            ],
            "paused": false,
            "e2e_processing_latency": {
              "count": 0,
              "percentiles": null
            }
          }
        ],
        "depth": 12,
        "backend_depth": 13,
        "message_count": 14,
        "paused": false,
        "e2e_processing_latency": {
          "count": 0,
          "percentiles": null
        }
      },
      {
        "topic_name": "t2",
        "channels": [
          {
            "channel_name": "c2",
            "depth": 15,
            "backend_depth": 16,
            "in_flight_count": 17,
            "deferred_count": 18,
            "message_count": 19,
            "requeue_count": 20,
            "timeout_count": 21,
            "clients": [
              {
                "name": "377569bd462b",
                "client_id": "377569bd462b",
                "hostname": "377569bd462b",
                "version": "V2",
                "remote_address": "172.17.0.8:48145",
                "state": 3,
                "ready_count": 22,
                "in_flight_count": 23,
                "message_count": 24,
                "finish_count": 25,
                "requeue_count": 26,
                "connect_ts": 1452021678,
                "sample_rate": 27,
                "deflate": true,
                "snappy": true,
                "user_agent": "go-nsq\/1.0.5",
                "tls": true,
                "tls_cipher_suite": "",
                "tls_version": "",
                "tls_negotiated_protocol": "",
                "tls_negotiated_protocol_is_mutual": false
              }
            ],
            "paused": false,
            "e2e_processing_latency": {
              "count": 0,
              "percentiles": null
            }
          }
        ],
        "depth": 28,
        "backend_depth": 29,
        "message_count": 30,
        "paused": false,
        "e2e_processing_latency": {
          "count": 0,
          "percentiles": null
        }
      }
    ]
  }
}
`
