package gnmi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/proto/gnmi_ext"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/gnmi/extensions/jnpr_gnmi_extention"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
)

func TestParsePath(t *testing.T) {
	path := "/foo/bar/bla[shoo=woo][shoop=/woop/]/z"
	parsed, err := parsePath("theorigin", path, "thetarget")

	require.NoError(t, err)
	require.Equal(t, "theorigin", parsed.Origin)
	require.Equal(t, "thetarget", parsed.Target)
	require.Equal(t, []*gnmi.PathElem{{Name: "foo"}, {Name: "bar"},
		{Name: "bla", Key: map[string]string{"shoo": "woo", "shoop": "/woop/"}}, {Name: "z"}}, parsed.Elem)

	parsed, err = parsePath("", "", "")
	require.NoError(t, err)
	require.Equal(t, &gnmi.Path{}, parsed)

	parsed, err = parsePath("", "/foo[[", "")
	require.Nil(t, parsed)
	require.Error(t, err)
}

type mockServer struct {
	subscribeF func(gnmi.GNMI_SubscribeServer) error
	grpcServer *grpc.Server
}

func (*mockServer) Capabilities(context.Context, *gnmi.CapabilityRequest) (*gnmi.CapabilityResponse, error) {
	return nil, nil
}

func (*mockServer) Get(context.Context, *gnmi.GetRequest) (*gnmi.GetResponse, error) {
	return nil, nil
}

func (*mockServer) Set(context.Context, *gnmi.SetRequest) (*gnmi.SetResponse, error) {
	return nil, nil
}

func (s *mockServer) Subscribe(server gnmi.GNMI_SubscribeServer) error {
	return s.subscribeF(server)
}

func TestWaitError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	gnmiServer := &mockServer{
		subscribeF: func(gnmi.GNMI_SubscribeServer) error {
			return errors.New("testerror")
		},
		grpcServer: grpcServer,
	}
	gnmi.RegisterGNMIServer(grpcServer, gnmiServer)

	plugin := &GNMI{
		Log:       testutil.Logger{},
		Addresses: []string{listener.Addr().String()},
		Encoding:  "proto",
		Redial:    config.Duration(1 * time.Second),
	}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Start(&acc))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := grpcServer.Serve(listener); err != nil {
			t.Error(err)
		}
	}()

	acc.WaitError(1)
	plugin.Stop()
	grpcServer.Stop()
	wg.Wait()

	// Check if the expected error text is among the errors
	require.Len(t, acc.Errors, 1)
	require.ErrorContains(t, acc.Errors[0], "aborted gNMI subscription: rpc error: code = Unknown desc = testerror")
}

func TestUsernamePassword(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	gnmiServer := &mockServer{
		subscribeF: func(server gnmi.GNMI_SubscribeServer) error {
			metadata, ok := metadata.FromIncomingContext(server.Context())
			if !ok {
				return errors.New("failed to get metadata")
			}

			username := metadata.Get("username")
			if len(username) != 1 || username[0] != "theusername" {
				return errors.New("wrong username")
			}

			password := metadata.Get("password")
			if len(password) != 1 || password[0] != "thepassword" {
				return errors.New("wrong password")
			}

			return errors.New("success")
		},
		grpcServer: grpcServer,
	}
	gnmi.RegisterGNMIServer(grpcServer, gnmiServer)

	plugin := &GNMI{
		Log:       testutil.Logger{},
		Addresses: []string{listener.Addr().String()},
		Username:  config.NewSecret([]byte("theusername")),
		Password:  config.NewSecret([]byte("thepassword")),
		Encoding:  "proto",
		Redial:    config.Duration(1 * time.Second),
	}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Start(&acc))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := grpcServer.Serve(listener); err != nil {
			t.Error(err)
		}
	}()

	acc.WaitError(1)
	plugin.Stop()
	grpcServer.Stop()
	wg.Wait()

	// Check if the expected error text is among the errors
	require.Len(t, acc.Errors, 1)
	require.ErrorContains(t, acc.Errors[0], "aborted gNMI subscription: rpc error: code = Unknown desc = success")
}

func mockGNMINotification() *gnmi.Notification {
	return &gnmi.Notification{
		Timestamp: 1543236572000000000,
		Prefix: &gnmi.Path{
			Origin: "type",
			Elem: []*gnmi.PathElem{
				{
					Name: "model",
					Key:  map[string]string{"foo": "bar"},
				},
			},
			Target: "subscription",
		},
		Update: []*gnmi.Update{
			{
				Path: &gnmi.Path{
					Elem: []*gnmi.PathElem{
						{Name: "some"},
						{
							Name: "path",
							Key:  map[string]string{"name": "str", "uint64": "1234"}},
					},
				},
				Val: &gnmi.TypedValue{Value: &gnmi.TypedValue_IntVal{IntVal: 5678}},
			},
			{
				Path: &gnmi.Path{
					Elem: []*gnmi.PathElem{
						{Name: "other"},
						{Name: "path"},
					},
				},
				Val: &gnmi.TypedValue{Value: &gnmi.TypedValue_StringVal{StringVal: "foobar"}},
			},
			{
				Path: &gnmi.Path{
					Elem: []*gnmi.PathElem{
						{Name: "other"},
						{Name: "this"},
					},
				},
				Val: &gnmi.TypedValue{Value: &gnmi.TypedValue_StringVal{StringVal: "that"}},
			},
		},
	}
}

func TestNotification(t *testing.T) {
	tests := []struct {
		name     string
		plugin   *GNMI
		server   *mockServer
		expected []telegraf.Metric
	}{
		{
			name: "multiple metrics",
			plugin: &GNMI{
				Log:      testutil.Logger{},
				Encoding: "proto",
				Redial:   config.Duration(1 * time.Second),
				Subscriptions: []subscription{
					{
						Name:             "alias",
						Origin:           "type",
						Path:             "/model",
						SubscriptionMode: "sample",
					},
				},
			},
			server: &mockServer{
				subscribeF: func(server gnmi.GNMI_SubscribeServer) error {
					notification := mockGNMINotification()
					err := server.Send(&gnmi.SubscribeResponse{Response: &gnmi.SubscribeResponse_Update{Update: notification}})
					if err != nil {
						return err
					}
					err = server.Send(&gnmi.SubscribeResponse{Response: &gnmi.SubscribeResponse_SyncResponse{SyncResponse: true}})
					if err != nil {
						return err
					}
					notification.Prefix.Elem[0].Key["foo"] = "bar2"
					notification.Update[0].Path.Elem[1].Key["name"] = "str2"
					notification.Update[0].Val = &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonVal{JsonVal: []byte{'"', '1', '2', '3', '"'}}}
					return server.Send(&gnmi.SubscribeResponse{Response: &gnmi.SubscribeResponse_Update{Update: notification}})
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"alias",
					map[string]string{
						"path":   "type:/model",
						"source": "127.0.0.1",
						"foo":    "bar",
						"name":   "str",
						"uint64": "1234",
					},
					map[string]interface{}{
						"some/path": int64(5678),
					},
					time.Unix(0, 0),
				),
				testutil.MustMetric(
					"alias",
					map[string]string{
						"path":   "type:/model",
						"source": "127.0.0.1",
						"foo":    "bar",
					},
					map[string]interface{}{
						"other/path": "foobar",
						"other/this": "that",
					},
					time.Unix(0, 0),
				),
				testutil.MustMetric(
					"alias",
					map[string]string{
						"path":   "type:/model",
						"foo":    "bar2",
						"source": "127.0.0.1",
						"name":   "str2",
						"uint64": "1234",
					},
					map[string]interface{}{
						"some/path": "123",
					},
					time.Unix(0, 0),
				),
				testutil.MustMetric(
					"alias",
					map[string]string{
						"path":   "type:/model",
						"source": "127.0.0.1",
						"foo":    "bar2",
					},
					map[string]interface{}{
						"other/path": "foobar",
						"other/this": "that",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "full path field key",
			plugin: &GNMI{
				Log:      testutil.Logger{},
				Encoding: "proto",
				Redial:   config.Duration(1 * time.Second),
				Subscriptions: []subscription{
					{
						Name:             "PHY_COUNTERS",
						Origin:           "type",
						Path:             "/state/port[port-id=*]/ethernet/oper-speed",
						SubscriptionMode: "sample",
					},
				},
			},
			server: &mockServer{
				subscribeF: func(server gnmi.GNMI_SubscribeServer) error {
					response := &gnmi.SubscribeResponse{
						Response: &gnmi.SubscribeResponse_Update{
							Update: &gnmi.Notification{
								Timestamp: 1543236572000000000,
								Prefix: &gnmi.Path{
									Origin: "type",
									Elem: []*gnmi.PathElem{
										{
											Name: "state",
										},
										{
											Name: "port",
											Key:  map[string]string{"port-id": "1"},
										},
										{
											Name: "ethernet",
										},
										{
											Name: "oper-speed",
										},
									},
									Target: "subscription",
								},
								Update: []*gnmi.Update{
									{
										Path: &gnmi.Path{},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_IntVal{IntVal: 42},
										},
									},
								},
							},
						},
					}
					return server.Send(response)
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"PHY_COUNTERS",
					map[string]string{
						"path":    "type:/state/port/ethernet/oper-speed",
						"source":  "127.0.0.1",
						"port_id": "1",
					},
					map[string]interface{}{
						"oper_speed": 42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "legacy tagged update pair",
			plugin: &GNMI{
				Log:      testutil.Logger{},
				Encoding: "proto",
				Redial:   config.Duration(1 * time.Second),
				Subscriptions: []subscription{
					{
						Name:             "oc-intf-desc",
						Origin:           "openconfig-interfaces",
						Path:             "/interfaces/interface/state/description",
						SubscriptionMode: "on_change",
						TagOnly:          true,
					},
					{
						Name:             "oc-intf-counters",
						Origin:           "openconfig-interfaces",
						Path:             "/interfaces/interface/state/counters",
						SubscriptionMode: "sample",
					},
				},
			},
			server: &mockServer{
				subscribeF: func(server gnmi.GNMI_SubscribeServer) error {
					tagResponse := &gnmi.SubscribeResponse{
						Response: &gnmi.SubscribeResponse_Update{
							Update: &gnmi.Notification{
								Timestamp: 1543236571000000000,
								Prefix:    &gnmi.Path{},
								Update: []*gnmi.Update{
									{
										Path: &gnmi.Path{
											Origin: "",
											Elem: []*gnmi.PathElem{
												{
													Name: "interfaces",
												},
												{
													Name: "interface",
													Key:  map[string]string{"name": "Ethernet1"},
												},
												{
													Name: "state",
												},
												{
													Name: "description",
												},
											},
											Target: "",
										},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_StringVal{StringVal: "foo"},
										},
									},
								},
							},
						},
					}
					if err := server.Send(tagResponse); err != nil {
						return err
					}
					if err := server.Send(&gnmi.SubscribeResponse{Response: &gnmi.SubscribeResponse_SyncResponse{SyncResponse: true}}); err != nil {
						return err
					}
					taggedResponse := &gnmi.SubscribeResponse{
						Response: &gnmi.SubscribeResponse_Update{
							Update: &gnmi.Notification{
								Timestamp: 1543236572000000000,
								Prefix:    &gnmi.Path{},
								Update: []*gnmi.Update{
									{
										Path: &gnmi.Path{
											Origin: "",
											Elem: []*gnmi.PathElem{
												{
													Name: "interfaces",
												},
												{
													Name: "interface",
													Key:  map[string]string{"name": "Ethernet1"},
												},
												{
													Name: "state",
												},
												{
													Name: "counters",
												},
												{
													Name: "in-broadcast-pkts",
												},
											},
											Target: "",
										},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_IntVal{IntVal: 42},
										},
									},
								},
							},
						},
					}
					return server.Send(taggedResponse)
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"oc-intf-counters",
					map[string]string{
						"source":                   "127.0.0.1",
						"name":                     "Ethernet1",
						"oc-intf-desc/description": "foo",
					},
					map[string]interface{}{
						"in_broadcast_pkts": 42,
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "issue #11011",
			plugin: &GNMI{
				Log:      testutil.Logger{},
				Encoding: "proto",
				Redial:   config.Duration(1 * time.Second),
				TagSubscriptions: []tagSubscription{
					{
						subscription: subscription{
							Name:             "oc-neigh-desc",
							Origin:           "openconfig",
							Path:             "/network-instances/network-instance/protocols/protocol/bgp/neighbors/neighbor/state/description",
							SubscriptionMode: "on_change",
						},
						Elements: []string{"network-instance", "protocol", "neighbor"},
					},
				},
				Subscriptions: []subscription{
					{
						Name:             "oc-neigh-state",
						Origin:           "openconfig",
						Path:             "/network-instances/network-instance/protocols/protocol/bgp/neighbors/neighbor/state/session-state",
						SubscriptionMode: "on_change",
					},
				},
			},
			server: &mockServer{
				subscribeF: func(server gnmi.GNMI_SubscribeServer) error {
					tagResponse := &gnmi.SubscribeResponse{
						Response: &gnmi.SubscribeResponse_Update{
							Update: &gnmi.Notification{
								Timestamp: 1543236571000000000,
								Prefix:    &gnmi.Path{},
								Update: []*gnmi.Update{
									{
										Path: &gnmi.Path{
											Origin: "",
											Elem: []*gnmi.PathElem{
												{
													Name: "network-instances",
												},
												{
													Name: "network-instance",
													Key:  map[string]string{"name": "default"},
												},
												{
													Name: "protocols",
												},
												{
													Name: "protocol",
													Key:  map[string]string{"name": "BGP", "identifier": "BGP"},
												},
												{
													Name: "bgp",
												},
												{
													Name: "neighbors",
												},
												{
													Name: "neighbor",
													Key:  map[string]string{"neighbor_address": "192.0.2.1"},
												},
												{
													Name: "state",
												},
												{
													Name: "description",
												},
											},
											Target: "",
										},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_StringVal{StringVal: "EXAMPLE-PEER"},
										},
									},
								},
							},
						},
					}
					if err := server.Send(tagResponse); err != nil {
						return err
					}
					if err := server.Send(&gnmi.SubscribeResponse{Response: &gnmi.SubscribeResponse_SyncResponse{SyncResponse: true}}); err != nil {
						return err
					}
					taggedResponse := &gnmi.SubscribeResponse{
						Response: &gnmi.SubscribeResponse_Update{
							Update: &gnmi.Notification{
								Timestamp: 1543236572000000000,
								Prefix:    &gnmi.Path{},
								Update: []*gnmi.Update{
									{
										Path: &gnmi.Path{
											Origin: "",
											Elem: []*gnmi.PathElem{
												{
													Name: "network-instances",
												},
												{
													Name: "network-instance",
													Key:  map[string]string{"name": "default"},
												},
												{
													Name: "protocols",
												},
												{
													Name: "protocol",
													Key:  map[string]string{"name": "BGP", "identifier": "BGP"},
												},
												{
													Name: "bgp",
												},
												{
													Name: "neighbors",
												},
												{
													Name: "neighbor",
													Key:  map[string]string{"neighbor_address": "192.0.2.1"},
												},
												{
													Name: "state",
												},
												{
													Name: "session-state",
												},
											},
											Target: "",
										},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_StringVal{StringVal: "ESTABLISHED"},
										},
									},
								},
							},
						},
					}

					return server.Send(taggedResponse)
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"oc-neigh-state",
					map[string]string{
						"source":                    "127.0.0.1",
						"neighbor_address":          "192.0.2.1",
						"name":                      "default",
						"oc-neigh-desc/description": "EXAMPLE-PEER",
						"/network-instances/network-instance/protocols/protocol/name": "BGP",
						"identifier": "BGP",
					},
					map[string]interface{}{
						"session_state": "ESTABLISHED",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "issue #12257 Arista",
			plugin: &GNMI{
				Log:      testutil.Logger{},
				Encoding: "proto",
				Redial:   config.Duration(1 * time.Second),
				Subscriptions: []subscription{
					{
						Name:             "interfaces",
						Origin:           "openconfig",
						Path:             "/interfaces/interface/state/counters",
						SubscriptionMode: "sample",
						SampleInterval:   config.Duration(1 * time.Second),
					},
				},
			},
			server: &mockServer{
				subscribeF: func(server gnmi.GNMI_SubscribeServer) error {
					if err := server.Send(&gnmi.SubscribeResponse{Response: &gnmi.SubscribeResponse_SyncResponse{SyncResponse: true}}); err != nil {
						return err
					}
					response := &gnmi.SubscribeResponse{
						Response: &gnmi.SubscribeResponse_Update{
							Update: &gnmi.Notification{
								Timestamp: 1668762813698611837,
								Prefix: &gnmi.Path{
									Origin: "openconfig",
									Elem: []*gnmi.PathElem{
										{Name: "interfaces"},
										{Name: "interface", Key: map[string]string{"name": "Ethernet1"}},
										{Name: "state"},
										{Name: "counters"},
									},
									Target: "OC-YANG",
								},
								Update: []*gnmi.Update{
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "in-broadcast-pkts"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "in-discards"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "in-errors"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "in-fcs-errors"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "in-unicast-pkts"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "out-broadcast-pkts"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "out-discards"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "out-errors"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "out-multicast-pkts"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "out-octets"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "out-pkts"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
									{
										Path: &gnmi.Path{Elem: []*gnmi.PathElem{{Name: "out-unicast-pkts"}}},
										Val:  &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: 0}},
									},
								},
							},
						},
					}
					return server.Send(response)
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"interfaces",
					map[string]string{
						"path":   "openconfig:/interfaces/interface/state/counters",
						"source": "127.0.0.1",
						"name":   "Ethernet1",
					},
					map[string]interface{}{
						"in_broadcast_pkts":  uint64(0),
						"in_discards":        uint64(0),
						"in_errors":          uint64(0),
						"in_fcs_errors":      uint64(0),
						"in_unicast_pkts":    uint64(0),
						"out_broadcast_pkts": uint64(0),
						"out_discards":       uint64(0),
						"out_errors":         uint64(0),
						"out_multicast_pkts": uint64(0),
						"out_octets":         uint64(0),
						"out_pkts":           uint64(0),
						"out_unicast_pkts":   uint64(0),
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "issue #12257 Sonic",
			plugin: &GNMI{
				Log:                           testutil.Logger{},
				Encoding:                      "proto",
				Redial:                        config.Duration(1 * time.Second),
				EnforceFirstNamespaceAsOrigin: true,
				Subscriptions: []subscription{
					{
						Name:             "temperature",
						Origin:           "openconfig-platform",
						Path:             "/components/component[name=TEMP 1]/state",
						SubscriptionMode: "sample",
						SampleInterval:   config.Duration(1 * time.Second),
					},
				},
			},
			server: &mockServer{
				subscribeF: func(server gnmi.GNMI_SubscribeServer) error {
					if err := server.Send(&gnmi.SubscribeResponse{Response: &gnmi.SubscribeResponse_SyncResponse{SyncResponse: true}}); err != nil {
						return err
					}
					response := &gnmi.SubscribeResponse{
						Response: &gnmi.SubscribeResponse_Update{
							Update: &gnmi.Notification{
								Timestamp: 1668771585733542546,
								Prefix: &gnmi.Path{
									Elem: []*gnmi.PathElem{
										{Name: "openconfig-platform:components"},
										{Name: "component", Key: map[string]string{"name": "TEMP 1"}},
										{Name: "state"},
									},
									Target: "OC-YANG",
								},
								Update: []*gnmi.Update{
									{
										Path: &gnmi.Path{
											Elem: []*gnmi.PathElem{
												{Name: "temperature"},
												{Name: "low-threshold"},
											}},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_FloatVal{FloatVal: 0},
										},
									},
									{
										Path: &gnmi.Path{
											Elem: []*gnmi.PathElem{
												{Name: "temperature"},
												{Name: "timestamp"},
											}},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_StringVal{StringVal: "2022-11-18T11:39:26Z"},
										},
									},
									{
										Path: &gnmi.Path{
											Elem: []*gnmi.PathElem{
												{Name: "temperature"},
												{Name: "warning-status"},
											}},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_BoolVal{BoolVal: false},
										},
									},
									{
										Path: &gnmi.Path{
											Elem: []*gnmi.PathElem{
												{Name: "name"},
											}},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_StringVal{StringVal: "CPU On-board"},
										},
									},
									{
										Path: &gnmi.Path{
											Elem: []*gnmi.PathElem{
												{Name: "temperature"},
												{Name: "critical-high-threshold"},
											}},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_FloatVal{FloatVal: 94},
										},
									},
									{
										Path: &gnmi.Path{
											Elem: []*gnmi.PathElem{
												{Name: "temperature"},
												{Name: "current"},
											}},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_FloatVal{FloatVal: 29},
										},
									},
									{
										Path: &gnmi.Path{
											Elem: []*gnmi.PathElem{
												{Name: "temperature"},
												{Name: "high-threshold"},
											}},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_FloatVal{FloatVal: 90},
										},
									},
								},
							},
						},
					}
					return server.Send(response)
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"temperature",
					map[string]string{
						"path":   "openconfig-platform:/components/component/state",
						"source": "127.0.0.1",
						"name":   "TEMP 1",
					},
					map[string]interface{}{
						"temperature/timestamp":               "2022-11-18T11:39:26Z",
						"temperature/low_threshold":           float64(0),
						"temperature/current":                 float64(29),
						"temperature/high_threshold":          float64(90),
						"temperature/critical_high_threshold": float64(94),
						"temperature/warning_status":          false,
						"name":                                "CPU On-board",
					},
					time.Unix(0, 0),
				),
			},
		},
		{
			name: "Juniper Extension",
			plugin: &GNMI{
				Log:                           testutil.Logger{},
				Encoding:                      "proto",
				VendorSpecific:                []string{"juniper_header"},
				Redial:                        config.Duration(1 * time.Second),
				EnforceFirstNamespaceAsOrigin: true,
				Subscriptions: []subscription{
					{
						Name:             "type",
						Origin:           "openconfig-platform",
						Path:             "/components/component[name=CHASSIS0:FPC0]/state",
						SubscriptionMode: "sample",
						SampleInterval:   config.Duration(1 * time.Second),
					},
				},
			},
			server: &mockServer{
				subscribeF: func(server gnmi.GNMI_SubscribeServer) error {
					if err := server.Send(&gnmi.SubscribeResponse{Response: &gnmi.SubscribeResponse_SyncResponse{SyncResponse: true}}); err != nil {
						return err
					}
					response := &gnmi.SubscribeResponse{
						Response: &gnmi.SubscribeResponse_Update{
							Update: &gnmi.Notification{
								Timestamp: 1668771585733542546,
								Prefix: &gnmi.Path{
									Elem: []*gnmi.PathElem{
										{Name: "openconfig-platform:components"},
										{Name: "component", Key: map[string]string{"name": "CHASSIS0:FPC0"}},
										{Name: "state"},
									},
									Target: "OC-YANG",
								},
								Update: []*gnmi.Update{
									{
										Path: &gnmi.Path{
											Elem: []*gnmi.PathElem{
												{Name: "type"},
											}},
										Val: &gnmi.TypedValue{
											Value: &gnmi.TypedValue_StringVal{StringVal: "LINECARD"},
										},
									},
								},
							},
						},
						Extension: []*gnmi_ext.Extension{{
							Ext: &gnmi_ext.Extension_RegisteredExt{
								RegisteredExt: &gnmi_ext.RegisteredExtension{
									// Juniper Header Extension
									// EID_JUNIPER_TELEMETRY_HEADER = 1;
									Id: 1,
									Msg: func(jnprExt *jnpr_gnmi_extention.GnmiJuniperTelemetryHeaderExtension) []byte {
										b, err := proto.Marshal(jnprExt)
										if err != nil {
											return nil
										}
										return b
									}(&jnpr_gnmi_extention.GnmiJuniperTelemetryHeaderExtension{ComponentId: 15, SubComponentId: 1, Component: "PICD"}),
								},
							},
						}},
					}
					return server.Send(response)
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"type",
					map[string]string{
						"path":             "openconfig-platform:/components/component/state",
						"source":           "127.0.0.1",
						"name":             "CHASSIS0:FPC0",
						"component_id":     "15",
						"sub_component_id": "1",
						"component":        "PICD",
					},
					map[string]interface{}{
						"type": "LINECARD",
					},
					time.Unix(0, 0),
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)

			tt.plugin.Addresses = []string{listener.Addr().String()}

			grpcServer := grpc.NewServer()
			tt.server.grpcServer = grpcServer
			gnmi.RegisterGNMIServer(grpcServer, tt.server)

			var acc testutil.Accumulator
			require.NoError(t, tt.plugin.Init())
			require.NoError(t, tt.plugin.Start(&acc))

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := grpcServer.Serve(listener); err != nil {
					t.Error(err)
				}
			}()

			acc.Wait(len(tt.expected))
			tt.plugin.Stop()
			grpcServer.Stop()
			wg.Wait()

			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics(),
				testutil.IgnoreTime())
		})
	}
}

func TestRedial(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	plugin := &GNMI{
		Log:       testutil.Logger{},
		Addresses: []string{listener.Addr().String()},
		Encoding:  "proto",
		Redial:    config.Duration(10 * time.Millisecond),
		Aliases:   map[string]string{"dummy": "type:/model"},
	}

	grpcServer := grpc.NewServer()
	gnmiServer := &mockServer{
		subscribeF: func(server gnmi.GNMI_SubscribeServer) error {
			notification := mockGNMINotification()
			return server.Send(&gnmi.SubscribeResponse{Response: &gnmi.SubscribeResponse_Update{Update: notification}})
		},
		grpcServer: grpcServer,
	}
	gnmi.RegisterGNMIServer(grpcServer, gnmiServer)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := grpcServer.Serve(listener); err != nil {
			t.Error(err)
		}
	}()

	var acc testutil.Accumulator
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Start(&acc))

	acc.Wait(2)
	grpcServer.Stop()
	wg.Wait()

	// Restart gNMI server at the same address
	listener, err = net.Listen("tcp", listener.Addr().String())
	require.NoError(t, err)

	grpcServer = grpc.NewServer()
	gnmiServer = &mockServer{
		subscribeF: func(server gnmi.GNMI_SubscribeServer) error {
			notification := mockGNMINotification()
			notification.Prefix.Elem[0].Key["foo"] = "bar2"
			notification.Update[0].Path.Elem[1].Key["name"] = "str2"
			notification.Update[0].Val = &gnmi.TypedValue{Value: &gnmi.TypedValue_BoolVal{BoolVal: false}}
			return server.Send(&gnmi.SubscribeResponse{Response: &gnmi.SubscribeResponse_Update{Update: notification}})
		},
		grpcServer: grpcServer,
	}
	gnmi.RegisterGNMIServer(grpcServer, gnmiServer)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := grpcServer.Serve(listener); err != nil {
			t.Error(err)
		}
	}()

	acc.Wait(4)
	plugin.Stop()
	grpcServer.Stop()
	wg.Wait()
}

func TestCases(t *testing.T) {
	// Get all testcase directories
	folders, err := os.ReadDir("testcases")
	require.NoError(t, err)

	// Register the plugin
	inputs.Add("gnmi", func() telegraf.Input {
		return &GNMI{
			Redial:                        config.Duration(10 * time.Second),
			EnforceFirstNamespaceAsOrigin: true,
		}
	})

	for _, f := range folders {
		// Only handle folders
		if !f.IsDir() {
			continue
		}

		t.Run(f.Name(), func(t *testing.T) {
			testcasePath := filepath.Join("testcases", f.Name())
			configFilename := filepath.Join(testcasePath, "telegraf.conf")
			inputFilename := filepath.Join(testcasePath, "responses.json")
			expectedFilename := filepath.Join(testcasePath, "expected.out")
			expectedErrorFilename := filepath.Join(testcasePath, "expected.err")

			// Load the input data
			buf, err := os.ReadFile(inputFilename)
			require.NoError(t, err)
			var entries []json.RawMessage
			require.NoError(t, json.Unmarshal(buf, &entries))
			responses := make([]gnmi.SubscribeResponse, len(entries))
			for i, entry := range entries {
				require.NoError(t, protojson.Unmarshal(entry, &responses[i]))
			}

			// Prepare the influx parser for expectations
			parser := &influx.Parser{}
			require.NoError(t, parser.Init())

			// Read the expected output if any
			var expected []telegraf.Metric
			if _, err := os.Stat(expectedFilename); err == nil {
				var err error
				expected, err = testutil.ParseMetricsFromFile(expectedFilename, parser)
				require.NoError(t, err)
			}

			// Read the expected output if any
			var expectedErrors []string
			if _, err := os.Stat(expectedErrorFilename); err == nil {
				var err error
				expectedErrors, err = testutil.ParseLinesFromFile(expectedErrorFilename)
				require.NoError(t, err)
				require.NotEmpty(t, expectedErrors)
			}

			// Configure the plugin
			cfg := config.NewConfig()
			require.NoError(t, cfg.LoadConfig(configFilename))
			require.Len(t, cfg.Inputs, 1)

			// Prepare the server response
			responseFunction := func(server gnmi.GNMI_SubscribeServer) error {
				for i := range responses {
					if err := server.Send(&responses[i]); err != nil {
						return err
					}
				}

				return nil
			}

			// Setup a mock server
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			grpcServer := grpc.NewServer()
			gnmiServer := &mockServer{
				subscribeF: responseFunction,
				grpcServer: grpcServer,
			}
			gnmi.RegisterGNMIServer(grpcServer, gnmiServer)

			// Setup the plugin
			plugin := cfg.Inputs[0].Input.(*GNMI)
			plugin.Addresses = []string{listener.Addr().String()}
			plugin.Log = testutil.Logger{}

			// Start the server
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := grpcServer.Serve(listener); err != nil {
					t.Error(err)
				}
			}()

			var acc testutil.Accumulator
			require.NoError(t, plugin.Init())
			require.NoError(t, plugin.Start(&acc))

			require.Eventually(t,
				func() bool {
					return acc.NMetrics() >= uint64(len(expected))
				}, 15*time.Second, 100*time.Millisecond)
			plugin.Stop()
			grpcServer.Stop()
			wg.Wait()

			// Check for errors
			require.Len(t, acc.Errors, len(expectedErrors))
			if len(acc.Errors) > 0 {
				var actualErrorMsgs []string
				for _, err := range acc.Errors {
					actualErrorMsgs = append(actualErrorMsgs, err.Error())
				}
				require.ElementsMatch(t, actualErrorMsgs, expectedErrors)
			}

			// Check the metric nevertheless as we might get some metrics despite errors.
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, expected, actual, testutil.SortMetrics())
		})
	}
}
