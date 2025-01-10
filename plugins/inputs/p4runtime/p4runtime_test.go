package p4runtime

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	p4_config "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/testutil"
)

// CounterSpec available here https://github.com/p4lang/p4runtime/blob/main/proto/p4/config/v1/p4info.proto#L289
func createCounter(
	name string,
	id uint32,
	unit p4_config.CounterSpec_Unit,
) *p4_config.Counter {
	return &p4_config.Counter{
		Preamble: &p4_config.Preamble{Name: name, Id: id},
		Spec:     &p4_config.CounterSpec{Unit: unit},
	}
}

func createEntityCounterEntry(
	counterID uint32,
	index int64,
	data *p4.CounterData,
) *p4.Entity_CounterEntry {
	return &p4.Entity_CounterEntry{
		CounterEntry: &p4.CounterEntry{
			CounterId: counterID,
			Index:     &p4.Index{Index: index},
			Data:      data,
		},
	}
}

func newTestP4RuntimeClient(
	p4RuntimeClient *fakeP4RuntimeClient,
	addr string,
	t *testing.T,
) *P4runtime {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	return &P4runtime{
		Endpoint: addr,
		DeviceID: uint64(1),
		Log:      testutil.Logger{},
		conn:     conn,
		client:   p4RuntimeClient,
	}
}

func TestInitDefault(t *testing.T) {
	plugin := &P4runtime{Log: testutil.Logger{}}
	require.NoError(t, plugin.Init())
	require.Equal(t, "127.0.0.1:9559", plugin.Endpoint)
	require.Equal(t, uint64(0), plugin.DeviceID)
	require.Empty(t, plugin.CounterNamesInclude)
	require.False(t, plugin.EnableTLS)
}

func TestErrorGetP4Info(t *testing.T) {
	responses := []struct {
		getForwardingPipelineConfigResponse      *p4.GetForwardingPipelineConfigResponse
		getForwardingPipelineConfigResponseError error
	}{
		{
			getForwardingPipelineConfigResponse:      nil,
			getForwardingPipelineConfigResponseError: errors.New("error when retrieving forwarding pipeline config"),
		}, {
			getForwardingPipelineConfigResponse: &p4.GetForwardingPipelineConfigResponse{
				Config: nil,
			},
			getForwardingPipelineConfigResponseError: nil,
		}, {
			getForwardingPipelineConfigResponse: &p4.GetForwardingPipelineConfigResponse{
				Config: &p4.ForwardingPipelineConfig{P4Info: nil},
			},
			getForwardingPipelineConfigResponseError: nil,
		},
	}

	for _, response := range responses {
		p4RtClient := &fakeP4RuntimeClient{
			getForwardingPipelineConfigFn: func() (*p4.GetForwardingPipelineConfigResponse, error) {
				return response.getForwardingPipelineConfigResponse, response.getForwardingPipelineConfigResponseError
			},
		}

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		plugin := newTestP4RuntimeClient(p4RtClient, listener.Addr().String(), t)

		var acc testutil.Accumulator
		require.Error(t, plugin.Gather(&acc))
	}
}

func TestOneCounterRead(t *testing.T) {
	tests := []struct {
		forwardingPipelineConfig *p4.ForwardingPipelineConfig
		EntityCounterEntry       *p4.Entity_CounterEntry
		expected                 []telegraf.Metric
	}{
		{
			forwardingPipelineConfig: &p4.ForwardingPipelineConfig{
				P4Info: &p4_config.P4Info{
					Counters: []*p4_config.Counter{
						createCounter("foo", 1111, p4_config.CounterSpec_BOTH),
					},
					PkgInfo: &p4_config.PkgInfo{Name: "P4Program"},
				},
			},
			EntityCounterEntry: createEntityCounterEntry(
				1111,
				5,
				&p4.CounterData{ByteCount: 5, PacketCount: 1},
			),
			expected: []telegraf.Metric{testutil.MustMetric(
				"p4_runtime",
				map[string]string{
					"p4program_name": "P4Program",
					"counter_name":   "foo",
					"counter_type":   "BOTH",
				},
				map[string]interface{}{
					"bytes":         int64(5),
					"packets":       int64(1),
					"counter_index": 5},
				time.Unix(0, 0)),
			},
		}, {
			forwardingPipelineConfig: &p4.ForwardingPipelineConfig{
				P4Info: &p4_config.P4Info{
					Counters: []*p4_config.Counter{
						createCounter(
							"foo",
							2222,
							p4_config.CounterSpec_BYTES,
						),
					},
					PkgInfo: &p4_config.PkgInfo{Name: "P4Program"},
				},
			},
			EntityCounterEntry: createEntityCounterEntry(
				2222,
				5,
				&p4.CounterData{ByteCount: 5},
			),
			expected: []telegraf.Metric{testutil.MustMetric(
				"p4_runtime",
				map[string]string{
					"p4program_name": "P4Program",
					"counter_name":   "foo",
					"counter_type":   "BYTES",
				},
				map[string]interface{}{
					"bytes":         int64(5),
					"packets":       int64(0),
					"counter_index": 5},
				time.Unix(0, 0)),
			},
		}, {
			forwardingPipelineConfig: &p4.ForwardingPipelineConfig{
				P4Info: &p4_config.P4Info{
					Counters: []*p4_config.Counter{
						createCounter(
							"foo",
							3333,
							p4_config.CounterSpec_PACKETS,
						),
					},
					PkgInfo: &p4_config.PkgInfo{Name: "P4Program"},
				},
			},
			EntityCounterEntry: createEntityCounterEntry(
				3333,
				5,
				&p4.CounterData{PacketCount: 1},
			),
			expected: []telegraf.Metric{testutil.MustMetric(
				"p4_runtime",
				map[string]string{
					"p4program_name": "P4Program",
					"counter_name":   "foo",
					"counter_type":   "PACKETS",
				},
				map[string]interface{}{
					"bytes":         int64(0),
					"packets":       int64(1),
					"counter_index": 5},
				time.Unix(0, 0)),
			},
		}, {
			forwardingPipelineConfig: &p4.ForwardingPipelineConfig{
				P4Info: &p4_config.P4Info{
					Counters: []*p4_config.Counter{
						createCounter("foo", 4444, p4_config.CounterSpec_BOTH),
					},
					PkgInfo: &p4_config.PkgInfo{Name: "P4Program"},
				},
			},
			EntityCounterEntry: createEntityCounterEntry(
				4444,
				5,
				&p4.CounterData{},
			),
			expected: nil,
		},
	}

	for _, tt := range tests {
		p4RtReadClient := &fakeP4RuntimeReadClient{
			recvFn: func() (*p4.ReadResponse, error) {
				return &p4.ReadResponse{
					Entities: []*p4.Entity{{Entity: tt.EntityCounterEntry}},
				}, nil
			},
		}

		p4RtClient := &fakeP4RuntimeClient{
			readFn: func(*p4.ReadRequest) (p4.P4Runtime_ReadClient, error) {
				return p4RtReadClient, nil
			},
			getForwardingPipelineConfigFn: func() (*p4.GetForwardingPipelineConfigResponse, error) {
				return &p4.GetForwardingPipelineConfigResponse{
					Config: tt.forwardingPipelineConfig,
				}, nil
			},
		}

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		plugin := newTestP4RuntimeClient(p4RtClient, listener.Addr().String(), t)

		var acc testutil.Accumulator
		require.NoError(t, plugin.Gather(&acc))

		testutil.RequireMetricsEqual(
			t,
			tt.expected,
			acc.GetTelegrafMetrics(),
			testutil.IgnoreTime(),
		)
	}
}

func TestMultipleEntitiesSingleCounterRead(t *testing.T) {
	totalNumOfEntriesArr := [3]int{2, 10, 100}

	for _, totalNumOfEntries := range totalNumOfEntriesArr {
		var expected []telegraf.Metric

		fmt.Println(
			"Running TestMultipleEntitiesSingleCounterRead with ",
			totalNumOfEntries,
			"totalNumOfCounters",
		)
		entities := make([]*p4.Entity, 0, totalNumOfEntries)
		p4InfoCounters := make([]*p4_config.Counter, 0, totalNumOfEntries)
		p4InfoCounters = append(
			p4InfoCounters,
			createCounter("foo", 0, p4_config.CounterSpec_BOTH),
		)

		for i := 0; i < totalNumOfEntries; i++ {
			counterEntry := &p4.Entity{
				Entity: createEntityCounterEntry(
					0,
					int64(i),
					&p4.CounterData{
						ByteCount:   int64(10),
						PacketCount: int64(10),
					},
				),
			}

			entities = append(entities, counterEntry)
			expected = append(expected, testutil.MustMetric(
				"p4_runtime",
				map[string]string{
					"p4program_name": "P4Program",
					"counter_name":   "foo",
					"counter_type":   "BOTH",
				},
				map[string]interface{}{
					"bytes":         int64(10),
					"packets":       int64(10),
					"counter_index": i,
				},
				time.Unix(0, 0),
			))
		}

		forwardingPipelineConfig := &p4.ForwardingPipelineConfig{
			P4Info: &p4_config.P4Info{
				Counters: p4InfoCounters,
				PkgInfo:  &p4_config.PkgInfo{Name: "P4Program"},
			},
		}

		p4RtReadClient := &fakeP4RuntimeReadClient{
			recvFn: func() (*p4.ReadResponse, error) {
				return &p4.ReadResponse{Entities: entities}, nil
			},
		}

		p4RtClient := &fakeP4RuntimeClient{
			readFn: func(*p4.ReadRequest) (p4.P4Runtime_ReadClient, error) {
				return p4RtReadClient, nil
			},
			getForwardingPipelineConfigFn: func() (*p4.GetForwardingPipelineConfigResponse, error) {
				return &p4.GetForwardingPipelineConfigResponse{
					Config: forwardingPipelineConfig,
				}, nil
			},
		}

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		plugin := newTestP4RuntimeClient(p4RtClient, listener.Addr().String(), t)

		var acc testutil.Accumulator
		require.NoError(t, plugin.Gather(&acc))
		acc.Wait(totalNumOfEntries)

		testutil.RequireMetricsEqual(
			t,
			expected,
			acc.GetTelegrafMetrics(),
			testutil.IgnoreTime(),
		)
	}
}

func TestSingleEntitiesMultipleCounterRead(t *testing.T) {
	totalNumOfCountersArr := [3]int{2, 10, 100}

	for _, totalNumOfCounters := range totalNumOfCountersArr {
		var expected []telegraf.Metric

		fmt.Println(
			"Running TestSingleEntitiesMultipleCounterRead with ",
			totalNumOfCounters,
			"totalNumOfCounters",
		)
		p4InfoCounters := make([]*p4_config.Counter, 0, totalNumOfCounters)

		for i := 1; i <= totalNumOfCounters; i++ {
			counterName := fmt.Sprintf("foo%v", i)
			p4InfoCounters = append(
				p4InfoCounters,
				createCounter(
					counterName,
					uint32(i),
					p4_config.CounterSpec_BOTH,
				),
			)

			expected = append(expected, testutil.MustMetric(
				"p4_runtime",
				map[string]string{
					"p4program_name": "P4Program",
					"counter_name":   counterName,
					"counter_type":   "BOTH",
				},
				map[string]interface{}{
					"bytes":         int64(10),
					"packets":       int64(10),
					"counter_index": 1,
				},
				time.Unix(0, 0),
			))
		}

		forwardingPipelineConfig := &p4.ForwardingPipelineConfig{
			P4Info: &p4_config.P4Info{
				Counters: p4InfoCounters,
				PkgInfo:  &p4_config.PkgInfo{Name: "P4Program"},
			},
		}

		p4RtClient := &fakeP4RuntimeClient{
			readFn: func(in *p4.ReadRequest) (p4.P4Runtime_ReadClient, error) {
				counterID := in.Entities[0].GetCounterEntry().CounterId
				return &fakeP4RuntimeReadClient{
					recvFn: func() (*p4.ReadResponse, error) {
						return &p4.ReadResponse{
							Entities: []*p4.Entity{{
								Entity: createEntityCounterEntry(
									counterID,
									1,
									&p4.CounterData{
										ByteCount:   10,
										PacketCount: 10,
									},
								),
							}},
						}, nil
					},
				}, nil
			},
			getForwardingPipelineConfigFn: func() (*p4.GetForwardingPipelineConfigResponse, error) {
				return &p4.GetForwardingPipelineConfigResponse{
					Config: forwardingPipelineConfig,
				}, nil
			},
		}

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		plugin := newTestP4RuntimeClient(p4RtClient, listener.Addr().String(), t)

		var acc testutil.Accumulator
		require.NoError(t, plugin.Gather(&acc))
		acc.Wait(totalNumOfCounters)

		testutil.RequireMetricsEqual(
			t,
			expected,
			acc.GetTelegrafMetrics(),
			testutil.SortMetrics(),
			testutil.IgnoreTime(),
		)
	}
}

func TestNoCountersAvailable(t *testing.T) {
	forwardingPipelineConfig := &p4.ForwardingPipelineConfig{
		P4Info: &p4_config.P4Info{Counters: make([]*p4_config.Counter, 0)},
	}

	p4RtClient := &fakeP4RuntimeClient{
		getForwardingPipelineConfigFn: func() (*p4.GetForwardingPipelineConfigResponse, error) {
			return &p4.GetForwardingPipelineConfigResponse{
				Config: forwardingPipelineConfig,
			}, nil
		},
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	plugin := newTestP4RuntimeClient(p4RtClient, listener.Addr().String(), t)

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))
}

func TestFilterCounters(t *testing.T) {
	forwardingPipelineConfig := &p4.ForwardingPipelineConfig{
		P4Info: &p4_config.P4Info{
			Counters: []*p4_config.Counter{
				createCounter("foo", 1, p4_config.CounterSpec_BOTH),
			},
			PkgInfo: &p4_config.PkgInfo{Name: "P4Program"},
		},
	}

	p4RtClient := &fakeP4RuntimeClient{
		getForwardingPipelineConfigFn: func() (*p4.GetForwardingPipelineConfigResponse, error) {
			return &p4.GetForwardingPipelineConfigResponse{
				Config: forwardingPipelineConfig,
			}, nil
		},
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	plugin := newTestP4RuntimeClient(p4RtClient, listener.Addr().String(), t)

	plugin.CounterNamesInclude = []string{"oof"}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))
	testutil.RequireMetricsEqual(
		t,
		nil,
		acc.GetTelegrafMetrics(),
		testutil.IgnoreTime(),
	)
}

func TestFailReadCounterEntryFromEntry(t *testing.T) {
	p4RtReadClient := &fakeP4RuntimeReadClient{
		recvFn: func() (*p4.ReadResponse, error) {
			return &p4.ReadResponse{
				Entities: []*p4.Entity{{
					Entity: &p4.Entity_TableEntry{
						TableEntry: &p4.TableEntry{},
					}}}}, nil
		},
	}

	p4RtClient := &fakeP4RuntimeClient{
		readFn: func(*p4.ReadRequest) (p4.P4Runtime_ReadClient, error) {
			return p4RtReadClient, nil
		},
		getForwardingPipelineConfigFn: func() (*p4.GetForwardingPipelineConfigResponse, error) {
			return &p4.GetForwardingPipelineConfigResponse{
				Config: &p4.ForwardingPipelineConfig{
					P4Info: &p4_config.P4Info{
						Counters: []*p4_config.Counter{
							createCounter(
								"foo",
								1111,
								p4_config.CounterSpec_BOTH,
							),
						},
						PkgInfo: &p4_config.PkgInfo{Name: "P4Program"},
					},
				},
			}, nil
		},
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	plugin := newTestP4RuntimeClient(p4RtClient, listener.Addr().String(), t)

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))
	require.Equal(
		t,
		errors.New("reading counter entry from entry table_entry:{} failed"),
		acc.Errors[0],
	)
	testutil.RequireMetricsEqual(
		t,
		nil,
		acc.GetTelegrafMetrics(),
		testutil.IgnoreTime(),
	)
}

func TestFailReadAllEntries(t *testing.T) {
	p4RtClient := &fakeP4RuntimeClient{
		readFn: func(*p4.ReadRequest) (p4.P4Runtime_ReadClient, error) {
			return nil, errors.New("connection error")
		},
		getForwardingPipelineConfigFn: func() (*p4.GetForwardingPipelineConfigResponse, error) {
			return &p4.GetForwardingPipelineConfigResponse{
				Config: &p4.ForwardingPipelineConfig{
					P4Info: &p4_config.P4Info{
						Counters: []*p4_config.Counter{
							createCounter(
								"foo",
								1111,
								p4_config.CounterSpec_BOTH,
							),
						},
						PkgInfo: &p4_config.PkgInfo{Name: "P4Program"},
					},
				},
			}, nil
		},
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	plugin := newTestP4RuntimeClient(p4RtClient, listener.Addr().String(), t)

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))
	require.Equal(
		t,
		acc.Errors[0],
		fmt.Errorf("reading counter entries with ID=1111 failed with error: %w", errors.New("connection error")),
	)
	testutil.RequireMetricsEqual(
		t,
		nil,
		acc.GetTelegrafMetrics(),
		testutil.IgnoreTime(),
	)
}

func TestFilterCounterNamesInclude(t *testing.T) {
	counters := []*p4_config.Counter{
		createCounter("foo", 1, p4_config.CounterSpec_BOTH),
		createCounter("bar", 2, p4_config.CounterSpec_BOTH),
		nil,
		createCounter("", 3, p4_config.CounterSpec_BOTH),
	}

	counterNamesInclude := []string{"bar"}

	filteredCounters := filterCounters(counters, counterNamesInclude)
	require.Equal(
		t,
		[]*p4_config.Counter{
			createCounter("bar", 2, p4_config.CounterSpec_BOTH),
		}, filteredCounters,
	)
}
