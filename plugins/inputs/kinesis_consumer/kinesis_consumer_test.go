package kinesis_consumer

import (
	"encoding/base64"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	consumer "github.com/harlow/kinesis-consumer"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/parsers/json"
	"github.com/influxdata/telegraf/testutil"
)

func TestInvalidCoding(t *testing.T) {
	plugin := &KinesisConsumer{
		ContentEncoding: "notsupported",
	}
	require.ErrorContains(t, plugin.Init(), "unknown content encoding")
}

func TestOnMessage(t *testing.T) {
	// Prepare messages
	zlibBytpes, err := base64.StdEncoding.DecodeString(
		"eF5FjlFrgzAUhf9KuM+2aNB2zdsQ2xe3whQGW8qIeqdhaiSJK0P874u1Y4+Hc/jON0GHxoga858BgUF8fs5fzunHU5Jlj6cEPFDXHvXStGqsrsKWTapq44pW1SetxsF1a8qsRtGt0Yy" +
			"FKbUcrFT9UbYWtQH2frntkm/s7RInkNU6t9JpWNE5WBAFPo3CcHeg+9D703OziUOhCg6MQ/yakrspuZsyEjdYfsm+Jg2K1jZEfZLKQWUvFglylBobZXDLwSP8//EGpD4NNj7dUJpT6" +
			"hQY3W33h/AhCt84zDBf5l/MDl08",
	)
	require.NoError(t, err)

	gzippedBytes, err := base64.StdEncoding.DecodeString(
		"H4sIAAFXNGAAA0WOUWuDMBSF/0q4z7Zo0HbN2xDbF7fCFAZbyoh6p2FqJIkrQ/zvi7Vjj4dz+M43QYfGiBrznwGBQXx+zl/O6cdTkmWPpwQ8UNce9dK0aqyuwpZNqmrjilbVJ63GwXVr" +
			"yqxG0a3RjIUptRysVP1Rtha1AfZ+ue2Sb+ztEieQ1Tq30mlY0TlYEAU+jcJwd6D70PvTc7OJQ6EKDoxD/JqSuym5mzISN1h+yb4mDYrWNkR9kspBZS8WCXKUGhtlcMvBI/z/8QakPg02" +
			"Pt1QmlPqFBjdbfeH8CEK3zjMMF/mX0TaxZUpAQAA",
	)
	require.NoError(t, err)

	notZippedBytes := []byte(`
	{
		"messageType": "CONTROL_MESSAGE",
		"owner": "CloudwatchLogs",
		"logGroup": "",
		"logStream": "",
		"subscriptionFilters": [],
		"logEvents": [
			{
				"id": "",
				"timestamp": 1510254469274,
				"message": "{\"bob\":\"CWL CONTROL MESSAGE: Checking health of destination Firehose.\", \"timestamp\":\"2021-02-22T22:15:26.794854Z\"},"
			},
			{
				"id": "",
				"timestamp": 1510254469274,
				"message": "{\"bob\":\"CWL CONTROL MESSAGE: Checking health of destination Firehose.\", \"timestamp\":\"2021-02-22T22:15:26.794854Z\"}"
			}
		]
	}
  `)

	tests := []struct {
		name            string
		encoding        string
		records         map[telegraf.TrackingID]string
		args            *consumer.Record
		expectedNumber  int
		expectedContent string
	}{
		{
			name:     "test no compression",
			encoding: "none",
			records:  make(map[telegraf.TrackingID]string),
			args: &consumer.Record{
				Record: types.Record{
					Data:           notZippedBytes,
					SequenceNumber: aws.String("anything"),
				},
			},
			expectedNumber:  2,
			expectedContent: "bob",
		},
		{
			name:    "test no compression via empty string for ContentEncoding",
			records: make(map[telegraf.TrackingID]string),
			args: &consumer.Record{
				Record: types.Record{
					Data:           notZippedBytes,
					SequenceNumber: aws.String("anything"),
				},
			},
			expectedNumber:  2,
			expectedContent: "bob",
		},
		{
			name:     "test no compression via identity ContentEncoding",
			encoding: "identity",
			records:  make(map[telegraf.TrackingID]string),
			args: &consumer.Record{
				Record: types.Record{
					Data:           notZippedBytes,
					SequenceNumber: aws.String("anything"),
				},
			},
			expectedNumber:  2,
			expectedContent: "bob",
		},
		{
			name:    "test no compression via no ContentEncoding",
			records: make(map[telegraf.TrackingID]string),
			args: &consumer.Record{
				Record: types.Record{
					Data:           notZippedBytes,
					SequenceNumber: aws.String("anything"),
				},
			},
			expectedNumber:  2,
			expectedContent: "bob",
		},
		{
			name:     "test gzip compression",
			encoding: "gzip",
			records:  make(map[telegraf.TrackingID]string),
			args: &consumer.Record{
				Record: types.Record{
					Data:           gzippedBytes,
					SequenceNumber: aws.String("anything"),
				},
			},
			expectedNumber:  1,
			expectedContent: "bob",
		},
		{
			name:     "test zlib compression",
			encoding: "zlib",
			records:  make(map[telegraf.TrackingID]string),
			args: &consumer.Record{
				Record: types.Record{
					Data:           zlibBytpes,
					SequenceNumber: aws.String("anything"),
				},
			},
			expectedNumber:  1,
			expectedContent: "bob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Prepare JSON parser
			parser := &json.Parser{
				MetricName:   "json_test",
				Query:        "logEvents",
				StringFields: []string{"message"},
			}
			require.NoError(t, parser.Init())

			// Setup plugin
			plugin := &KinesisConsumer{
				ContentEncoding: tt.encoding,
				parser:          parser,
				records:         tt.records,
			}
			require.NoError(t, plugin.Init())

			var acc testutil.Accumulator
			require.NoError(t, plugin.onMessage(acc.WithTracking(tt.expectedNumber), tt.args))

			actual := acc.GetTelegrafMetrics()
			require.Len(t, actual, tt.expectedNumber)

			for _, metric := range actual {
				raw, found := metric.GetField("message")
				require.True(t, found, "no message present")
				message, ok := raw.(string)
				require.Truef(t, ok, "message not a string but %T", raw)
				require.Contains(t, message, tt.expectedContent)
			}
		})
	}
}
