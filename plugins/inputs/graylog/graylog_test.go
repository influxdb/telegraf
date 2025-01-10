package graylog

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

const validJSON = `
  {
    "total": 3,
    "metrics": [
      {
        "full_name": "jvm.cl.loaded",
        "metric": {
          "value": 18910
        },
        "name": "loaded",
        "type": "gauge"
      },
      {
        "full_name": "jvm.memory.pools.Metaspace.committed",
        "metric": {
          "value": 108040192
        },
        "name": "committed",
        "type": "gauge"
      },
      {
        "full_name": "org.graylog2.shared.journal.KafkaJournal.writeTime",
        "metric": {
          "time": {
            "min": 99
          },
          "rate": {
            "total": 10,
            "mean": 2
          },
          "duration_unit": "microseconds",
          "rate_unit": "events/second"
        },
        "name": "writeTime",
        "type": "hdrtimer"
      }
    ]
  }`

var validTags = map[string]map[string]string{
	"jvm.cl.loaded": {
		"name":   "loaded",
		"type":   "gauge",
		"port":   "12900",
		"server": "localhost",
	},
	"jvm.memory.pools.Metaspace.committed": {
		"name":   "committed",
		"type":   "gauge",
		"port":   "12900",
		"server": "localhost",
	},
	"org.graylog2.shared.journal.KafkaJournal.writeTime": {
		"name":   "writeTime",
		"type":   "hdrtimer",
		"port":   "12900",
		"server": "localhost",
	},
}

var expectedFields = map[string]map[string]interface{}{
	"jvm.cl.loaded": {
		"value": float64(18910),
	},
	"jvm.memory.pools.Metaspace.committed": {
		"value": float64(108040192),
	},
	"org.graylog2.shared.journal.KafkaJournal.writeTime": {
		"time_min":   float64(99),
		"rate_total": float64(10),
		"rate_mean":  float64(2),
	},
}

const invalidJSON = "I don't think this is JSON"

const empty = ""

type mockHTTPClient struct {
	responseBody string
	statusCode   int
}

// Mock implementation of MakeRequest. Usually returns an http.Response with
// hard-coded responseBody and statusCode. However, if the request uses a
// nonstandard method, it uses status code 405 (method not allowed)
func (c *mockHTTPClient) makeRequest(req *http.Request) (*http.Response, error) {
	resp := http.Response{}
	resp.StatusCode = c.statusCode

	// basic error checking on request method
	allowedMethods := []string{"GET", "HEAD", "POST", "PUT", "DELETE", "TRACE", "CONNECT"}
	methodValid := false
	for _, method := range allowedMethods {
		if req.Method == method {
			methodValid = true
			break
		}
	}

	if !methodValid {
		resp.StatusCode = 405 // Method not allowed
	}

	resp.Body = io.NopCloser(strings.NewReader(c.responseBody))
	return &resp, nil
}

func (*mockHTTPClient) setHTTPClient(*http.Client) {
}

func (*mockHTTPClient) httpClient() *http.Client {
	return nil
}

// Generates a pointer to an HttpJson object that uses a mock HTTP client.
// Parameters:
//
//	response  : Body of the response that the mock HTTP client should return
//	statusCode: HTTP status code the mock HTTP client should return
//
// Returns:
//
//	*HttpJson: Pointer to an HttpJson object that uses the generated mock HTTP client
func genMockGrayLog(response string, statusCode int) []*GrayLog {
	return []*GrayLog{
		{
			client: &mockHTTPClient{responseBody: response, statusCode: statusCode},
			Servers: []string{
				"http://localhost:12900/system/metrics/multiple",
			},
			Metrics: []string{
				"jvm.memory.pools.Metaspace.committed",
				"jvm.cl.loaded",
				"org.graylog2.shared.journal.KafkaJournal.writeTime",
			},
			Username: "test",
			Password: "test",
		},
	}
}

// Test that the proper values are ignored or collected
func TestNormalResponse(t *testing.T) {
	graylog := genMockGrayLog(validJSON, 200)

	for _, service := range graylog {
		var acc testutil.Accumulator
		err := acc.GatherError(service.Gather)
		require.NoError(t, err)
		for k, v := range expectedFields {
			acc.AssertContainsTaggedFields(t, k, v, validTags[k])
		}
	}
}

// Test response to HTTP 500
func TestHttpJson500(t *testing.T) {
	graylog := genMockGrayLog(validJSON, 500)

	var acc testutil.Accumulator
	err := acc.GatherError(graylog[0].Gather)

	require.Error(t, err)
	require.Equal(t, 0, acc.NFields())
}

// Test response to malformed JSON
func TestHttpJsonBadJson(t *testing.T) {
	graylog := genMockGrayLog(invalidJSON, 200)

	var acc testutil.Accumulator
	err := acc.GatherError(graylog[0].Gather)

	require.Error(t, err)
	require.Equal(t, 0, acc.NFields())
}

// Test response to empty string as response objectgT
func TestHttpJsonEmptyResponse(t *testing.T) {
	graylog := genMockGrayLog(empty, 200)

	var acc testutil.Accumulator
	err := acc.GatherError(graylog[0].Gather)

	require.Error(t, err)
	require.Equal(t, 0, acc.NFields())
}
