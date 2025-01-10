package lanz

import (
	"net/url"
	"strconv"
	"testing"

	pb "github.com/aristanetworks/goarista/lanz/proto"
	"google.golang.org/protobuf/proto"

	"github.com/influxdata/telegraf/testutil"
)

var testProtoBufCongestionRecord1 = &pb.LanzRecord{
	CongestionRecord: &pb.CongestionRecord{
		Timestamp:          proto.Uint64(100000000000000),
		IntfName:           proto.String("eth1"),
		SwitchId:           proto.Uint32(1),
		PortId:             proto.Uint32(1),
		QueueSize:          proto.Uint32(1),
		EntryType:          pb.CongestionRecord_EntryType.Enum(1),
		TrafficClass:       proto.Uint32(1),
		TimeOfMaxQLen:      proto.Uint64(100000000000000),
		TxLatency:          proto.Uint32(100),
		QDropCount:         proto.Uint32(1),
		FabricPeerIntfName: proto.String("FabricPeerIntfName1"),
	},
}
var testProtoBufCongestionRecord2 = &pb.LanzRecord{
	CongestionRecord: &pb.CongestionRecord{
		Timestamp:          proto.Uint64(200000000000000),
		IntfName:           proto.String("eth2"),
		SwitchId:           proto.Uint32(2),
		PortId:             proto.Uint32(2),
		QueueSize:          proto.Uint32(2),
		EntryType:          pb.CongestionRecord_EntryType.Enum(2),
		TrafficClass:       proto.Uint32(2),
		TimeOfMaxQLen:      proto.Uint64(200000000000000),
		TxLatency:          proto.Uint32(200),
		QDropCount:         proto.Uint32(2),
		FabricPeerIntfName: proto.String("FabricPeerIntfName2"),
	},
}

var testProtoBufGlobalBufferUsageRecord = &pb.LanzRecord{
	GlobalBufferUsageRecord: &pb.GlobalBufferUsageRecord{
		EntryType:  pb.GlobalBufferUsageRecord_EntryType.Enum(1),
		Timestamp:  proto.Uint64(100000000000000),
		BufferSize: proto.Uint32(1),
		Duration:   proto.Uint32(10),
	},
}

func TestLanzGeneratesMetrics(t *testing.T) {
	l := &Lanz{Servers: []string{
		"tcp://switch01.int.example.com:50001",
		"tcp://switch02.int.example.com:50001",
	}}

	deviceURL1, err := url.Parse(l.Servers[0])
	if err != nil {
		t.Fail()
	}
	deviceURL2, err := url.Parse(l.Servers[1])
	if err != nil {
		t.Fail()
	}

	var acc testutil.Accumulator
	msgToAccumulator(&acc, testProtoBufCongestionRecord1, deviceURL1)
	acc.Wait(1)

	vals1 := map[string]interface{}{
		"timestamp":        int64(100000000000000),
		"queue_size":       int64(1),
		"time_of_max_qlen": int64(100000000000000),
		"tx_latency":       int64(100),
		"q_drop_count":     int64(1),
	}
	tags1 := map[string]string{
		"intf_name":             "eth1",
		"switch_id":             strconv.FormatInt(int64(1), 10),
		"port_id":               strconv.FormatInt(int64(1), 10),
		"entry_type":            strconv.FormatInt(int64(1), 10),
		"traffic_class":         strconv.FormatInt(int64(1), 10),
		"fabric_peer_intf_name": "FabricPeerIntfName1",
		"source":                "switch01.int.example.com",
		"port":                  "50001",
	}

	acc.AssertContainsFields(t, "lanz_congestion_record", vals1)
	acc.AssertContainsTaggedFields(t, "lanz_congestion_record", vals1, tags1)

	acc.ClearMetrics()
	msgToAccumulator(&acc, testProtoBufCongestionRecord2, deviceURL2)
	acc.Wait(1)

	vals2 := map[string]interface{}{
		"timestamp":        int64(200000000000000),
		"queue_size":       int64(2),
		"time_of_max_qlen": int64(200000000000000),
		"tx_latency":       int64(200),
		"q_drop_count":     int64(2),
	}
	tags2 := map[string]string{
		"intf_name":             "eth2",
		"switch_id":             strconv.FormatInt(int64(2), 10),
		"port_id":               strconv.FormatInt(int64(2), 10),
		"entry_type":            strconv.FormatInt(int64(2), 10),
		"traffic_class":         strconv.FormatInt(int64(2), 10),
		"fabric_peer_intf_name": "FabricPeerIntfName2",
		"source":                "switch02.int.example.com",
		"port":                  "50001",
	}

	acc.AssertContainsFields(t, "lanz_congestion_record", vals2)
	acc.AssertContainsTaggedFields(t, "lanz_congestion_record", vals2, tags2)

	acc.ClearMetrics()
	msgToAccumulator(&acc, testProtoBufGlobalBufferUsageRecord, deviceURL1)
	acc.Wait(1)

	gburVals1 := map[string]interface{}{
		"timestamp":   int64(100000000000000),
		"buffer_size": int64(1),
		"duration":    int64(10),
	}
	gburTags1 := map[string]string{
		"entry_type": strconv.FormatInt(int64(1), 10),
		"source":     "switch01.int.example.com",
		"port":       "50001",
	}

	acc.AssertContainsFields(t, "lanz_global_buffer_usage_record", gburVals1)
	acc.AssertContainsTaggedFields(t, "lanz_global_buffer_usage_record", gburVals1, gburTags1)
}
