package input

import (
	"errors"
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/common/opcua"
	"github.com/influxdata/telegraf/testutil"
)

func TestTagsSliceToMap(t *testing.T) {
	m, err := tagsSliceToMap([][]string{{"foo", "bar"}, {"baz", "bat"}})
	require.NoError(t, err)
	require.Len(t, m, 2)
	require.Equal(t, "bar", m["foo"])
	require.Equal(t, "bat", m["baz"])
}

func TestTagsSliceToMap_twoStrings(t *testing.T) {
	var err error
	_, err = tagsSliceToMap([][]string{{"foo", "bar", "baz"}})
	require.Error(t, err)
	_, err = tagsSliceToMap([][]string{{"foo"}})
	require.Error(t, err)
}

func TestTagsSliceToMap_dupeKey(t *testing.T) {
	_, err := tagsSliceToMap([][]string{{"foo", "bar"}, {"foo", "bat"}})
	require.Error(t, err)
}

func TestTagsSliceToMap_empty(t *testing.T) {
	_, err := tagsSliceToMap([][]string{{"foo", ""}})
	require.Equal(t, errors.New("tag 1 has empty value"), err)
	_, err = tagsSliceToMap([][]string{{"", "bar"}})
	require.Equal(t, errors.New("tag 1 has empty name"), err)
}

func TestValidateOPCTags(t *testing.T) {
	tests := []struct {
		name   string
		config InputClientConfig
		err    error
	}{
		{
			"duplicates",
			InputClientConfig{
				MetricName: "mn",
				RootNodes: []NodeSettings{
					{
						FieldName:      "fn",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "i1",
						TagsSlice:      [][]string{{"t1", "v1"}, {"t2", "v2"}},
					},
				},
				Groups: []NodeGroupSettings{
					{
						Nodes: []NodeSettings{
							{
								FieldName:      "fn",
								Namespace:      "2",
								IdentifierType: "s",
								Identifier:     "i1",
							},
						},
						TagsSlice: [][]string{{"t1", "v1"}, {"t2", "v2"}},
					},
				},
			},
			errors.New(`name "fn" is duplicated (metric name "mn", tags "t1=v1, t2=v2")`),
		},
		{
			"empty tag value not allowed",
			InputClientConfig{
				MetricName: "mn",
				RootNodes: []NodeSettings{
					{
						FieldName:      "fn",
						IdentifierType: "s",
						TagsSlice:      [][]string{{"t1", ""}},
					},
				},
			},
			errors.New("tag 1 has empty value"),
		},
		{
			"empty tag name not allowed",
			InputClientConfig{
				MetricName: "mn",
				RootNodes: []NodeSettings{
					{
						FieldName:      "fn",
						IdentifierType: "s",
						TagsSlice:      [][]string{{"", "1"}},
					},
				},
			},
			errors.New("tag 1 has empty name"),
		},
		{
			"different metric tag names",
			InputClientConfig{
				MetricName: "mn",
				RootNodes: []NodeSettings{
					{
						FieldName:      "fn",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "i1",
						TagsSlice:      [][]string{{"t1", "v1"}, {"t2", "v2"}},
					},
					{
						FieldName:      "fn",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "i1",
						TagsSlice:      [][]string{{"t1", "v1"}, {"t3", "v2"}},
					},
				},
			},
			nil,
		},
		{
			"different metric tag values",
			InputClientConfig{
				MetricName: "mn",
				RootNodes: []NodeSettings{
					{
						FieldName:      "fn",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "i1",
						TagsSlice:      [][]string{{"t1", "foo"}, {"t2", "v2"}},
					},
					{
						FieldName:      "fn",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "i1",
						TagsSlice:      [][]string{{"t1", "bar"}, {"t2", "v2"}},
					},
				},
			},
			nil,
		},
		{
			"different metric names",
			InputClientConfig{
				MetricName: "mn",
				Groups: []NodeGroupSettings{
					{
						MetricName: "mn",
						Namespace:  "2",
						Nodes: []NodeSettings{
							{
								FieldName:      "fn",
								IdentifierType: "s",
								Identifier:     "i1",
								TagsSlice:      [][]string{{"t1", "v1"}, {"t2", "v2"}},
							},
						},
					},
					{
						MetricName: "mn2",
						Namespace:  "2",
						Nodes: []NodeSettings{
							{
								FieldName:      "fn",
								IdentifierType: "s",
								Identifier:     "i1",
								TagsSlice:      [][]string{{"t1", "v1"}, {"t2", "v2"}},
							},
						},
					},
				},
			},
			nil,
		},
		{
			"different field names",
			InputClientConfig{
				MetricName: "mn",
				RootNodes: []NodeSettings{
					{
						FieldName:      "fn",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "i1",
						TagsSlice:      [][]string{{"t1", "v1"}, {"t2", "v2"}},
					},
					{
						FieldName:      "fn2",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "i1",
						TagsSlice:      [][]string{{"t1", "v1"}, {"t2", "v2"}},
					},
				},
			},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := OpcUAInputClient{
				Config: tt.config,
				Log:    testutil.Logger{},
			}
			require.Equal(t, tt.err, o.InitNodeMetricMapping())
		})
	}
}

func TestNewNodeMetricMappingTags(t *testing.T) {
	tests := []struct {
		name         string
		settings     NodeSettings
		groupTags    map[string]string
		expectedTags map[string]string
		err          error
	}{
		{
			name: "empty tags",
			settings: NodeSettings{
				FieldName:      "f",
				Namespace:      "2",
				IdentifierType: "s",
				Identifier:     "h",
			},
			groupTags:    map[string]string{},
			expectedTags: map[string]string{},
			err:          nil,
		},
		{
			name: "node tags only",
			settings: NodeSettings{
				FieldName:      "f",
				Namespace:      "2",
				IdentifierType: "s",
				Identifier:     "h",
				TagsSlice:      [][]string{{"t1", "v1"}},
			},
			groupTags:    map[string]string{},
			expectedTags: map[string]string{"t1": "v1"},
			err:          nil,
		},
		{
			name: "group tags only",
			settings: NodeSettings{
				FieldName:      "f",
				Namespace:      "2",
				IdentifierType: "s",
				Identifier:     "h",
			},
			groupTags:    map[string]string{"t1": "v1"},
			expectedTags: map[string]string{"t1": "v1"},
			err:          nil,
		},
		{
			name: "node tag overrides group tags",
			settings: NodeSettings{
				FieldName:      "f",
				Namespace:      "2",
				IdentifierType: "s",
				Identifier:     "h",
				TagsSlice:      [][]string{{"t1", "v2"}},
			},
			groupTags:    map[string]string{"t1": "v1"},
			expectedTags: map[string]string{"t1": "v2"},
			err:          nil,
		},
		{
			name: "node tag merged with group tags",
			settings: NodeSettings{
				FieldName:      "f",
				Namespace:      "2",
				IdentifierType: "s",
				Identifier:     "h",
				TagsSlice:      [][]string{{"t2", "v2"}},
			},
			groupTags:    map[string]string{"t1": "v1"},
			expectedTags: map[string]string{"t1": "v1", "t2": "v2"},
			err:          nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nmm, err := NewNodeMetricMapping("testmetric", tt.settings, tt.groupTags)
			require.Equal(t, tt.err, err)
			require.Equal(t, tt.expectedTags, nmm.MetricTags)
		})
	}
}

func TestNewNodeMetricMappingIdStrInstantiated(t *testing.T) {
	nmm, err := NewNodeMetricMapping("testmetric", NodeSettings{
		FieldName:      "f",
		Namespace:      "2",
		IdentifierType: "s",
		Identifier:     "h",
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "ns=2;s=h", nmm.idStr)
}

func TestValidateNodeToAdd(t *testing.T) {
	tests := []struct {
		name     string
		existing map[metricParts]struct{}
		nmm      *NodeMetricMapping
		err      error
	}{
		{
			name:     "valid",
			existing: map[metricParts]struct{}{},
			nmm: func() *NodeMetricMapping {
				nmm, err := NewNodeMetricMapping("testmetric", NodeSettings{
					FieldName:      "f",
					Namespace:      "2",
					IdentifierType: "s",
					Identifier:     "hf",
				}, map[string]string{})
				require.NoError(t, err)
				return nmm
			}(),
			err: nil,
		},
		{
			name:     "empty field name not allowed",
			existing: map[metricParts]struct{}{},
			nmm: func() *NodeMetricMapping {
				nmm, err := NewNodeMetricMapping("testmetric", NodeSettings{
					FieldName:      "",
					Namespace:      "2",
					IdentifierType: "s",
					Identifier:     "hf",
				}, map[string]string{})
				require.NoError(t, err)
				return nmm
			}(),
			err: errors.New(`empty name in ""`),
		},
		{
			name:     "empty namespace not allowed",
			existing: map[metricParts]struct{}{},
			nmm: func() *NodeMetricMapping {
				nmm, err := NewNodeMetricMapping("testmetric", NodeSettings{
					FieldName:      "f",
					Namespace:      "",
					IdentifierType: "s",
					Identifier:     "hf",
				}, map[string]string{})
				require.NoError(t, err)
				return nmm
			}(),
			err: errors.New("empty node namespace not allowed"),
		},
		{
			name:     "empty identifier type not allowed",
			existing: map[metricParts]struct{}{},
			nmm: func() *NodeMetricMapping {
				nmm, err := NewNodeMetricMapping("testmetric", NodeSettings{
					FieldName:      "f",
					Namespace:      "2",
					IdentifierType: "",
					Identifier:     "hf",
				}, map[string]string{})
				require.NoError(t, err)
				return nmm
			}(),
			err: errors.New(`invalid identifier type "" in "f"`),
		},
		{
			name:     "invalid identifier type not allowed",
			existing: map[metricParts]struct{}{},
			nmm: func() *NodeMetricMapping {
				nmm, err := NewNodeMetricMapping("testmetric", NodeSettings{
					FieldName:      "f",
					Namespace:      "2",
					IdentifierType: "j",
					Identifier:     "hf",
				}, map[string]string{})
				require.NoError(t, err)
				return nmm
			}(),
			err: errors.New(`invalid identifier type "j" in "f"`),
		},
		{
			name: "duplicate metric not allowed",
			existing: map[metricParts]struct{}{
				{metricName: "testmetric", fieldName: "f", tags: "t1=v1, t2=v2"}: {},
			},
			nmm: func() *NodeMetricMapping {
				nmm, err := NewNodeMetricMapping("testmetric", NodeSettings{
					FieldName:      "f",
					Namespace:      "2",
					IdentifierType: "s",
					Identifier:     "hf",
					TagsSlice:      [][]string{{"t1", "v1"}, {"t2", "v2"}},
				}, map[string]string{})
				require.NoError(t, err)
				return nmm
			}(),
			err: errors.New(`name "f" is duplicated (metric name "testmetric", tags "t1=v1, t2=v2")`),
		},
		{
			name:     "identifier type mismatch",
			existing: map[metricParts]struct{}{},
			nmm: func() *NodeMetricMapping {
				nmm, err := NewNodeMetricMapping("testmetric", NodeSettings{
					FieldName:      "f",
					Namespace:      "2",
					IdentifierType: "i",
					Identifier:     "hf",
				}, map[string]string{})
				require.NoError(t, err)
				return nmm
			}(),
			err: errors.New(`identifier type "i" does not match the type of identifier "hf"`),
		},
	}

	for idT, idV := range map[string]string{
		"s": "hf",
		"i": "1",
		"g": "849683f0-ce92-4fa2-836f-a02cde61d75d",
		"b": "aGVsbG8gSSBhbSBhIHRlc3QgaWRlbnRpZmllcg=="} {
		tests = append(tests, struct {
			name     string
			existing map[metricParts]struct{}
			nmm      *NodeMetricMapping
			err      error
		}{
			name:     "identifier type " + idT + " allowed",
			existing: map[metricParts]struct{}{},
			nmm: func() *NodeMetricMapping {
				nmm, err := NewNodeMetricMapping("testmetric", NodeSettings{
					FieldName:      "f",
					Namespace:      "2",
					IdentifierType: idT,
					Identifier:     idV,
				}, map[string]string{})
				require.NoError(t, err)
				return nmm
			}(),
			err: nil,
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNodeToAdd(tt.existing, tt.nmm)
			require.Equal(t, tt.err, err)
		})
	}
}

func TestInitNodeMetricMapping(t *testing.T) {
	tests := []struct {
		testname string
		config   InputClientConfig
		expected []NodeMetricMapping
		err      error
	}{
		{
			testname: "only root node",
			config: InputClientConfig{
				MetricName: "testmetric",
				Timestamp:  TimestampSourceTelegraf,
				RootNodes: []NodeSettings{
					{
						FieldName:      "f",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "id1",
						TagsSlice:      [][]string{{"t1", "v1"}},
					},
				},
			},
			expected: []NodeMetricMapping{
				{
					Tag: NodeSettings{
						FieldName:      "f",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "id1",
						TagsSlice:      [][]string{{"t1", "v1"}},
					},
					idStr:      "ns=2;s=id1",
					metricName: "testmetric",
					MetricTags: map[string]string{"t1": "v1"},
				},
			},
			err: nil,
		},
		{
			testname: "root node and group node",
			config: InputClientConfig{
				MetricName: "testmetric",
				Timestamp:  TimestampSourceTelegraf,
				RootNodes: []NodeSettings{
					{
						FieldName:      "f",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "id1",
						TagsSlice:      [][]string{{"t1", "v1"}},
					},
				},
				Groups: []NodeGroupSettings{
					{
						MetricName:     "groupmetric",
						Namespace:      "3",
						IdentifierType: "s",
						Nodes: []NodeSettings{
							{
								FieldName:  "f",
								Identifier: "id2",
								TagsSlice:  [][]string{{"t2", "v2"}},
							},
						},
					},
				},
			},
			expected: []NodeMetricMapping{
				{
					Tag: NodeSettings{
						FieldName:      "f",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "id1",
						TagsSlice:      [][]string{{"t1", "v1"}},
					},
					idStr:      "ns=2;s=id1",
					metricName: "testmetric",
					MetricTags: map[string]string{"t1": "v1"},
				},
				{
					Tag: NodeSettings{
						FieldName:      "f",
						Namespace:      "3",
						IdentifierType: "s",
						Identifier:     "id2",
						TagsSlice:      [][]string{{"t2", "v2"}},
					},
					idStr:      "ns=3;s=id2",
					metricName: "groupmetric",
					MetricTags: map[string]string{"t2": "v2"},
				},
			},
			err: nil,
		},
		{
			testname: "only group node",
			config: InputClientConfig{
				MetricName: "testmetric",
				Timestamp:  TimestampSourceTelegraf,
				Groups: []NodeGroupSettings{
					{
						MetricName:     "groupmetric",
						Namespace:      "3",
						IdentifierType: "s",
						Nodes: []NodeSettings{
							{
								FieldName:  "f",
								Identifier: "id2",
								TagsSlice:  [][]string{{"t2", "v2"}},
							},
						},
					},
				},
			},
			expected: []NodeMetricMapping{
				{
					Tag: NodeSettings{
						FieldName:      "f",
						Namespace:      "3",
						IdentifierType: "s",
						Identifier:     "id2",
						TagsSlice:      [][]string{{"t2", "v2"}},
					},
					idStr:      "ns=3;s=id2",
					metricName: "groupmetric",
					MetricTags: map[string]string{"t2": "v2"},
				},
			},
			err: nil,
		},
		{
			testname: "tags and default only default tags used",
			config: InputClientConfig{
				MetricName: "testmetric",
				Timestamp:  TimestampSourceTelegraf,
				Groups: []NodeGroupSettings{
					{
						MetricName:     "groupmetric",
						Namespace:      "3",
						IdentifierType: "s",
						Nodes: []NodeSettings{
							{
								FieldName:   "f",
								Identifier:  "id2",
								TagsSlice:   [][]string{{"t2", "v2"}},
								DefaultTags: map[string]string{"t3": "v3"},
							},
						},
					},
				},
			},
			expected: []NodeMetricMapping{
				{
					Tag: NodeSettings{
						FieldName:      "f",
						Namespace:      "3",
						IdentifierType: "s",
						Identifier:     "id2",
						TagsSlice:      [][]string{{"t2", "v2"}},
						DefaultTags:    map[string]string{"t3": "v3"},
					},
					idStr:      "ns=3;s=id2",
					metricName: "groupmetric",
					MetricTags: map[string]string{"t3": "v3"},
				},
			},
			err: nil,
		},
		{
			testname: "only root node default overrides slice",
			config: InputClientConfig{
				MetricName: "testmetric",
				Timestamp:  TimestampSourceTelegraf,
				RootNodes: []NodeSettings{
					{
						FieldName:      "f",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "id1",
						TagsSlice:      [][]string{{"t1", "v1"}},
						DefaultTags:    map[string]string{"t3": "v3"},
					},
				},
			},
			expected: []NodeMetricMapping{
				{
					Tag: NodeSettings{
						FieldName:      "f",
						Namespace:      "2",
						IdentifierType: "s",
						Identifier:     "id1",
						TagsSlice:      [][]string{{"t1", "v1"}},
						DefaultTags:    map[string]string{"t3": "v3"},
					},
					idStr:      "ns=2;s=id1",
					metricName: "testmetric",
					MetricTags: map[string]string{"t3": "v3"},
				},
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.testname, func(t *testing.T) {
			o := OpcUAInputClient{Config: tt.config}
			err := o.InitNodeMetricMapping()
			require.NoError(t, err)
			require.Equal(t, tt.expected, o.NodeMetricMapping)
		})
	}
}

func TestUpdateNodeValue(t *testing.T) {
	type testStep struct {
		nodeIdx  int
		value    interface{}
		status   ua.StatusCode
		expected interface{}
	}
	tests := []struct {
		testname string
		steps    []testStep
	}{
		{
			"value should update when code ok",
			[]testStep{
				{
					0,
					"Harmony",
					ua.StatusOK,
					"Harmony",
				},
			},
		},
		{
			"value should not update when code bad",
			[]testStep{
				{
					0,
					"Harmony",
					ua.StatusOK,
					"Harmony",
				},
				{
					0,
					"Odium",
					ua.StatusBad,
					"Harmony",
				},
				{
					0,
					"Ati",
					ua.StatusOK,
					"Ati",
				},
			},
		},
	}

	conf := &opcua.OpcUAClientConfig{
		Endpoint:       "opc.tcp://localhost:4930",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		AuthMethod:     "",
		ConnectTimeout: config.Duration(2 * time.Second),
		RequestTimeout: config.Duration(2 * time.Second),
		Workarounds:    opcua.OpcUAWorkarounds{},
	}
	c, err := conf.CreateClient(testutil.Logger{})
	require.NoError(t, err)
	o := OpcUAInputClient{
		OpcUAClient: c,
		Log:         testutil.Logger{},
		NodeMetricMapping: []NodeMetricMapping{
			{
				Tag: NodeSettings{
					FieldName: "f",
				},
			},
			{
				Tag: NodeSettings{
					FieldName: "f2",
				},
			},
		},
		LastReceivedData: make([]NodeValue, 2),
	}

	for _, tt := range tests {
		t.Run(tt.testname, func(t *testing.T) {
			o.LastReceivedData = make([]NodeValue, 2)
			for i, step := range tt.steps {
				v, err := ua.NewVariant(step.value)
				require.NoError(t, err)
				o.UpdateNodeValue(0, &ua.DataValue{
					Value:             v,
					Status:            step.status,
					SourceTimestamp:   time.Date(2022, 03, 17, 8, 33, 00, 00, &time.Location{}).Add(time.Duration(i) * time.Second),
					SourcePicoseconds: 0,
					ServerTimestamp:   time.Date(2022, 03, 17, 8, 33, 00, 500, &time.Location{}).Add(time.Duration(i) * time.Second),
					ServerPicoseconds: 0,
				})
				require.Equal(t, step.expected, o.LastReceivedData[0].Value)
			}
		})
	}
}

func TestMetricForNode(t *testing.T) {
	conf := &opcua.OpcUAClientConfig{
		Endpoint:       "opc.tcp://localhost:4930",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		AuthMethod:     "",
		ConnectTimeout: config.Duration(2 * time.Second),
		RequestTimeout: config.Duration(2 * time.Second),
		Workarounds:    opcua.OpcUAWorkarounds{},
	}
	c, err := conf.CreateClient(testutil.Logger{})
	require.NoError(t, err)
	o := OpcUAInputClient{
		Config: InputClientConfig{
			Timestamp: TimestampSourceSource,
		},
		OpcUAClient:      c,
		Log:              testutil.Logger{},
		LastReceivedData: make([]NodeValue, 2),
	}

	tests := []struct {
		testname string
		nmm      []NodeMetricMapping
		v        interface{}
		time     time.Time
		status   ua.StatusCode
		expected telegraf.Metric
	}{
		{
			testname: "metric build correctly",
			nmm: []NodeMetricMapping{
				{
					Tag: NodeSettings{
						FieldName: "fn",
					},
					idStr:      "ns=3;s=hi",
					metricName: "testingmetric",
					MetricTags: map[string]string{"t1": "v1"},
				},
			},
			v:      16,
			time:   time.Date(2022, 03, 17, 8, 55, 00, 00, &time.Location{}),
			status: ua.StatusOK,
			expected: metric.New("testingmetric",
				map[string]string{"t1": "v1", "id": "ns=3;s=hi"},
				map[string]interface{}{"Quality": "The operation succeeded. StatusGood (0x0)", "fn": 16},
				time.Date(2022, 03, 17, 8, 55, 00, 00, &time.Location{})),
		},
	}

	for _, tt := range tests {
		t.Run(tt.testname, func(t *testing.T) {
			o.NodeMetricMapping = tt.nmm
			o.LastReceivedData[0].SourceTime = tt.time
			o.LastReceivedData[0].Quality = tt.status
			o.LastReceivedData[0].Value = tt.v
			actual := o.MetricForNode(0)
			require.Equal(t, tt.expected.Tags(), actual.Tags())
			require.Equal(t, tt.expected.Fields(), actual.Fields())
			require.Equal(t, tt.expected.Time(), actual.Time())
		})
	}
}
