package netflow

import (
	"bytes"
	"fmt"
	"net"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflowlegacy"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
)

// Decoder structure
type netflowv5Decoder struct{}

func (*netflowv5Decoder) init() error {
	if err := initL4ProtoMapping(); err != nil {
		return fmt.Errorf("initializing layer 4 protocol mapping failed: %w", err)
	}
	return nil
}

func (*netflowv5Decoder) decode(srcIP net.IP, payload []byte) ([]telegraf.Metric, error) {
	src := srcIP.String()

	// Decode the message
	var msg netflowlegacy.PacketNetFlowV5
	buf := bytes.NewBuffer(payload)
	if err := netflowlegacy.DecodeMessageVersion(buf, &msg); err != nil {
		return nil, err
	}

	// Extract metrics
	t := time.Unix(int64(msg.UnixSecs), int64(msg.UnixNSecs))
	metrics := make([]telegraf.Metric, 0, len(msg.Records))
	for _, record := range msg.Records {
		tags := map[string]string{
			"source":  src,
			"version": "NetFlowV5",
		}
		fields := map[string]interface{}{
			"flows":             msg.Count,
			"sys_uptime":        msg.SysUptime,
			"seq_number":        msg.FlowSequence,
			"engine_type":       mapEngineType(msg.EngineType),
			"sampling_interval": msg.SamplingInterval,
			"in_snmp":           record.Input,
			"out_snmp":          record.Output,
			"in_packets":        record.DPkts,
			"in_bytes":          record.DOctets,
			"first_switched":    record.First,
			"last_switched":     record.Last,
			"src_port":          record.SrcPort,
			"dst_port":          record.DstPort,
			"tcp_flags":         mapTCPFlags(record.TCPFlags),
			"protocol":          mapL4Proto(record.Proto),
			"bgp_src_as":        record.SrcAS,
			"bgp_dst_as":        record.DstAS,
			"src_mask":          record.SrcMask,
			"dst_mask":          record.DstMask,
		}

		var err error
		fields["engine_id"], err = decodeHex([]byte{msg.EngineId})
		if err != nil {
			return nil, fmt.Errorf("decoding 'engine_id' failed: %w", err)
		}
		fields["src"], err = decodeIPFromUint32(uint32(record.SrcAddr))
		if err != nil {
			return nil, fmt.Errorf("decoding 'src' failed: %w", err)
		}
		fields["dst"], err = decodeIPFromUint32(uint32(record.DstAddr))
		if err != nil {
			return nil, fmt.Errorf("decoding 'dst' failed: %w", err)
		}
		fields["next_hop"], err = decodeIPFromUint32(uint32(record.NextHop))
		if err != nil {
			return nil, fmt.Errorf("decoding 'next_hop' failed: %w", err)
		}
		fields["src_tos"], err = decodeHex([]byte{record.Tos})
		if err != nil {
			return nil, fmt.Errorf("decoding 'src_tos' failed: %w", err)
		}

		metrics = append(metrics, metric.New("netflow", tags, fields, t))
	}

	return metrics, nil
}
