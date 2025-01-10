package neptune_apex

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

func TestGather(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("data")); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	c, destroy := fakeHTTPClient(h)
	defer destroy()
	n := &NeptuneApex{
		httpClient: c,
	}
	tests := []struct {
		name    string
		servers []string
	}{
		{
			name:    "Good case, 2 servers",
			servers: []string{"http://abc", "https://def"},
		},
		{
			name: "Good case, 0 servers",
		},
		{
			name:    "Good case nil",
			servers: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var acc testutil.Accumulator
			n.Servers = test.servers
			require.NoError(t, n.Gather(&acc))
			require.Lenf(t, acc.Errors, len(test.servers),
				"Number of servers mismatch. got=%d, want=%d", len(acc.Errors), len(test.servers))
		})
	}
}

func TestParseXML(t *testing.T) {
	goodTime := time.Date(2018, 12, 22, 21, 55, 37, 0, time.FixedZone("PST", 3600*-8))
	tests := []struct {
		name        string
		xmlResponse []byte
		wantMetrics []telegraf.Metric
		wantAccErr  bool
		wantErr     bool
	}{
		{
			name:        "Good test",
			xmlResponse: []byte(apex2016),
			wantMetrics: []telegraf.Metric{
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":   "apex",
						"type":     "controller",
						"software": "5.04_7A18",
						"hardware": "1.0",
					},
					map[string]interface{}{
						"serial":         "AC5:12345",
						"power_failed":   int64(1544814000000000000),
						"power_restored": int64(1544833875000000000),
					},
					goodTime,
				),
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":      "apex",
						"output_id":   "0",
						"device_id":   "base_Var1",
						"name":        "VarSpd1_I1",
						"output_type": "variable",
						"type":        "output",
						"software":    "5.04_7A18",
						"hardware":    "1.0",
					},
					map[string]interface{}{"state": "PF1"},
					goodTime,
				),
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":      "apex",
						"output_id":   "6",
						"device_id":   "base_email",
						"name":        "EmailAlm_I5",
						"output_type": "alert",
						"type":        "output",
						"software":    "5.04_7A18",
						"hardware":    "1.0",
					},
					map[string]interface{}{"state": "AOF"},
					goodTime,
				),
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":      "apex",
						"output_id":   "8",
						"device_id":   "2_1",
						"name":        "RETURN_2_1",
						"output_type": "outlet",
						"type":        "output",
						"software":    "5.04_7A18",
						"hardware":    "1.0",
					},
					map[string]interface{}{
						"state": "AON",
						"watt":  35.0,
						"amp":   0.3,
					},
					goodTime,
				),
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":      "apex",
						"output_id":   "18",
						"device_id":   "3_1",
						"name":        "RVortech_3_1",
						"output_type": "unknown",
						"type":        "output",
						"software":    "5.04_7A18",
						"hardware":    "1.0",
					},
					map[string]interface{}{
						"state":   "TBL",
						"xstatus": "OK",
					},
					goodTime,
				),
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":      "apex",
						"output_id":   "28",
						"device_id":   "4_9",
						"name":        "LinkA_4_9",
						"output_type": "unknown",
						"type":        "output",
						"software":    "5.04_7A18",
						"hardware":    "1.0",
					},
					map[string]interface{}{"state": "AOF"},
					goodTime,
				),
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":      "apex",
						"output_id":   "32",
						"device_id":   "Cntl_A2",
						"name":        "LEAK",
						"output_type": "virtual",
						"type":        "output",
						"software":    "5.04_7A18",
						"hardware":    "1.0",
					},
					map[string]interface{}{"state": "AOF"},
					goodTime,
				),
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":     "apex",
						"name":       "Salt",
						"type":       "probe",
						"probe_type": "Cond",
						"software":   "5.04_7A18",
						"hardware":   "1.0",
					},
					map[string]interface{}{"value": 30.1},
					goodTime,
				),
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":   "apex",
						"name":     "Volt_2",
						"type":     "probe",
						"software": "5.04_7A18",
						"hardware": "1.0",
					},
					map[string]interface{}{"value": 115.0},
					goodTime,
				),
			},
		},
		{
			name:        "Unmarshal error",
			xmlResponse: []byte("Invalid"),
			wantErr:     true,
		},
		{
			name:        "Report time failure",
			xmlResponse: []byte(`<status><date>abc</date></status>`),
			wantErr:     true,
		},
		{
			name: "Power Failed time failure",
			xmlResponse: []byte(
				`<status><date>12/22/2018 21:55:37</date>
				<timezone>-8.0</timezone><power><failed>a</failed>
				<restored>12/22/2018 22:55:37</restored></power></status>`),
			wantMetrics: []telegraf.Metric{
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":   "",
						"type":     "controller",
						"hardware": "",
						"software": "",
					},
					map[string]interface{}{
						"serial":         "",
						"power_restored": int64(1545548137000000000),
					},
					goodTime,
				),
			},
		},
		{
			name: "Power restored time failure",
			xmlResponse: []byte(
				`<status><date>12/22/2018 21:55:37</date>
				<timezone>-8.0</timezone><power><restored>a</restored>
				<failed>12/22/2018 22:55:37</failed></power></status>`),
			wantMetrics: []telegraf.Metric{
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":   "",
						"type":     "controller",
						"hardware": "",
						"software": "",
					},
					map[string]interface{}{
						"serial":       "",
						"power_failed": int64(1545548137000000000),
					},
					goodTime,
				),
			},
		},
		{
			name: "Power failed failure",
			xmlResponse: []byte(
				`<status><power><failed>abc</failed></power></status>`),
			wantErr: true,
		},
		{
			name: "Failed to parse watt to float",
			xmlResponse: []byte(
				`<?xml version="1.0"?><status>
				<date>12/22/2018 21:55:37</date><timezone>-8.0</timezone>
				<power><failed>12/22/2018 21:55:37</failed>
				<restored>12/22/2018 21:55:37</restored></power>
				<outlets><outlet><name>o1</name></outlet></outlets>
				<probes><probe><name>o1W</name><value>abc</value></probe>
				</probes></status>`),
			wantAccErr: true,
			wantMetrics: []telegraf.Metric{
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":   "",
						"type":     "controller",
						"hardware": "",
						"software": "",
					},
					map[string]interface{}{
						"serial":         "",
						"power_failed":   int64(1545544537000000000),
						"power_restored": int64(1545544537000000000),
					},
					goodTime,
				),
			},
		},
		{
			name: "Failed to parse amp to float",
			xmlResponse: []byte(
				`<?xml version="1.0"?><status>
				<date>12/22/2018 21:55:37</date><timezone>-8.0</timezone>
				<power><failed>12/22/2018 21:55:37</failed>
				<restored>12/22/2018 21:55:37</restored></power>
				<outlets><outlet><name>o1</name></outlet></outlets>
				<probes><probe><name>o1A</name><value>abc</value></probe>
				</probes></status>`),
			wantAccErr: true,
			wantMetrics: []telegraf.Metric{
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":   "",
						"type":     "controller",
						"hardware": "",
						"software": "",
					},
					map[string]interface{}{
						"serial":         "",
						"power_failed":   int64(1545544537000000000),
						"power_restored": int64(1545544537000000000),
					},
					goodTime,
				),
			},
		},
		{
			name: "Failed to parse probe value to float",
			xmlResponse: []byte(
				`<?xml version="1.0"?><status>
				<date>12/22/2018 21:55:37</date><timezone>-8.0</timezone>
				<power><failed>12/22/2018 21:55:37</failed>
				<restored>12/22/2018 21:55:37</restored></power>
				<probes><probe><name>p1</name><value>abc</value></probe>
				</probes></status>`),
			wantAccErr: true,
			wantMetrics: []telegraf.Metric{
				testutil.MustMetric(
					Measurement,
					map[string]string{
						"source":   "",
						"type":     "controller",
						"hardware": "",
						"software": "",
					},
					map[string]interface{}{
						"serial":         "",
						"power_failed":   int64(1545544537000000000),
						"power_restored": int64(1545544537000000000),
					},
					goodTime,
				),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var acc testutil.Accumulator
			err := parseXML(&acc, test.xmlResponse)
			if test.wantErr {
				require.Error(t, err, "expected error but got <nil>")
				return
			}
			// No error case
			require.NoErrorf(t, err, "expected no error but got: %v", err)
			require.Equalf(t, test.wantAccErr, len(acc.Errors) > 0,
				"Accumulator errors. got=%v, want=%t", acc.Errors, test.wantAccErr)

			testutil.RequireMetricsEqual(t, acc.GetTelegrafMetrics(), test.wantMetrics)
		})
	}
}

func TestSendRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "Good case",
			statusCode: http.StatusOK,
		},
		{
			name:       "Get error",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name:       "Status 301",
			statusCode: http.StatusMovedPermanently,
			wantErr:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				if _, err := w.Write([]byte("data")); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			})
			c, destroy := fakeHTTPClient(h)
			defer destroy()
			n := &NeptuneApex{
				httpClient: c,
			}
			resp, err := n.sendRequest("http://abc")
			if test.wantErr {
				require.Error(t, err, "expected error but got <nil>")
				return
			}

			// No error case
			require.NoErrorf(t, err, "expected no error but got: %v", err)
			require.Equalf(t, resp, []byte("data"), "Response data mismatch. got=%q, want=%q", resp, "data")
		})
	}
}

func TestParseTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		timeZone float64
		wantTime time.Time
		wantErr  bool
	}{
		{
			name:     "Good case - Timezone positive",
			input:    "01/01/2023 12:34:56",
			timeZone: 5,
			wantTime: time.Date(2023, 1, 1, 12, 34, 56, 0,
				time.FixedZone("a", 3600*5)),
		},
		{
			name:     "Good case - Timezone negative",
			input:    "01/01/2023 12:34:56",
			timeZone: -8,
			wantTime: time.Date(2023, 1, 1, 12, 34, 56, 0,
				time.FixedZone("a", 3600*-8)),
		},
		{
			name:    "Cannot parse",
			input:   "Not a date",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			res, err := parseTime(test.input, test.timeZone)
			if test.wantErr {
				require.Error(t, err, "expected error but got <nil>")
				return
			}

			// No error case
			require.NoErrorf(t, err, "expected no error but got: %v", err)
			require.Truef(t, test.wantTime.Equal(res), "time mismatch. got=%q, want=%q", res, test.wantTime)
		})
	}
}

func TestFindProbe(t *testing.T) {
	t.Parallel()

	fakeProbes := []probe{
		{
			Name: "test1",
		},
		{
			Name: "good",
		},
	}
	tests := []struct {
		name      string
		probeName string
		wantIndex int
	}{
		{
			name:      "Good case - Found",
			probeName: "good",
			wantIndex: 1,
		},
		{
			name:      "Not found",
			probeName: "bad",
			wantIndex: -1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			index := findProbe(test.probeName, fakeProbes)
			require.Equalf(t, test.wantIndex, index, "probe index mismatch; got=%d, want %d", index, test.wantIndex)
		})
	}
}

// This fakeHttpClient creates a server and binds a client to it.
// That way, it is possible to control the http
// output from within the test without changes to the main code.
func fakeHTTPClient(h http.Handler) (*http.Client, func()) {
	s := httptest.NewServer(h)
	c := &http.Client{
		Transport: &http.Transport{
			DialContext: func(
				_ context.Context, network, _ string) (net.Conn, error) {
				return net.Dial(network, s.Listener.Addr().String())
			},
		},
	}
	return c, s.Close
}

// Sample configuration from a 2016 version Neptune Apex.
const apex2016 = `<?xml version="1.0"?>
<status software="5.04_7A18" hardware="1.0">
<hostname>apex</hostname>
<serial>AC5:12345</serial>
<timezone>-8.00</timezone>
<date>12/22/2018 21:55:37</date>
<power><failed>12/14/2018 11:00:00</failed>
<restored>12/14/2018 16:31:15</restored></power>
<probes>
<probe>
 <name>Salt</name> <value>30.1 </value>
 <type>Cond</type></probe><probe>
 <name>RETURN_2_1A</name> <value>0.3  </value>
</probe><probe>
 <name>RETURN_2_1W</name> <value>  35 </value>
</probe><probe>
 <name>Volt_2</name> <value>115  </value>
</probe></probes>
<outlets>
<outlet>
 <name>VarSpd1_I1</name>
 <outputID>0</outputID>
 <state>PF1</state>
 <deviceID>base_Var1</deviceID>
</outlet>
<outlet>
 <name>EmailAlm_I5</name>
 <outputID>6</outputID>
 <state>AOF</state>
 <deviceID>base_email</deviceID>
</outlet>
<outlet>
 <name>RETURN_2_1</name>
 <outputID>8</outputID>
 <state>AON</state>
 <deviceID>2_1</deviceID>
</outlet>
<outlet>
 <name>RVortech_3_1</name>
 <outputID>18</outputID>
 <state>TBL</state>
 <deviceID>3_1</deviceID>
<xstatus>OK</xstatus></outlet>
<outlet>
 <name>LinkA_4_9</name>
 <outputID>28</outputID>
 <state>AOF</state>
 <deviceID>4_9</deviceID>
</outlet>
<outlet>
 <name>LEAK</name>
 <outputID>32</outputID>
 <state>AOF</state>
 <deviceID>Cntl_A2</deviceID>
</outlet>
</outlets></status>
`
