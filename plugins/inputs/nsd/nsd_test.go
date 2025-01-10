package nsd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

func nsdControl(output string) func(string, config.Duration, bool, string, string) (*bytes.Buffer, error) {
	return func(string, config.Duration, bool, string, string) (*bytes.Buffer, error) {
		return bytes.NewBufferString(output), nil
	}
}

func TestParseFullOutput(t *testing.T) {
	acc := &testutil.Accumulator{}
	v := &NSD{
		run: nsdControl(fullOutput),
	}
	err := v.Gather(acc)

	require.NoError(t, err)

	require.True(t, acc.HasMeasurement("nsd"))
	require.True(t, acc.HasMeasurement("nsd_servers"))

	require.Len(t, acc.Metrics, 2)
	require.Equal(t, 99, acc.NFields())

	acc.AssertContainsFields(t, "nsd", parsedFullOutput)
	acc.AssertContainsFields(t, "nsd_servers", parsedFullOutputServerAsTag)
}

var parsedFullOutputServerAsTag = map[string]interface{}{
	"queries": float64(75576),
}

var parsedFullOutput = map[string]interface{}{
	"num_queries":         float64(75557),
	"time_boot":           float64(2944405.500253),
	"time_elapsed":        float64(2944405.500253),
	"size_db_disk":        float64(98304),
	"size_db_mem":         float64(22784),
	"size_xfrd_mem":       float64(83956312),
	"size_config_disk":    float64(0),
	"size_config_mem":     float64(6088),
	"num_type_TYPE0":      float64(6),
	"num_type_A":          float64(46311),
	"num_type_NS":         float64(478),
	"num_type_MD":         float64(0),
	"num_type_MF":         float64(0),
	"num_type_CNAME":      float64(272),
	"num_type_SOA":        float64(596),
	"num_type_MB":         float64(0),
	"num_type_MG":         float64(0),
	"num_type_MR":         float64(0),
	"num_type_NULL":       float64(0),
	"num_type_WKS":        float64(0),
	"num_type_PTR":        float64(83),
	"num_type_HINFO":      float64(1),
	"num_type_MINFO":      float64(0),
	"num_type_MX":         float64(296),
	"num_type_TXT":        float64(794),
	"num_type_RP":         float64(0),
	"num_type_AFSDB":      float64(0),
	"num_type_X25":        float64(0),
	"num_type_ISDN":       float64(0),
	"num_type_RT":         float64(0),
	"num_type_NSAP":       float64(0),
	"num_type_SIG":        float64(0),
	"num_type_KEY":        float64(1),
	"num_type_PX":         float64(0),
	"num_type_AAAA":       float64(22736),
	"num_type_LOC":        float64(2),
	"num_type_NXT":        float64(0),
	"num_type_SRV":        float64(93),
	"num_type_NAPTR":      float64(5),
	"num_type_KX":         float64(0),
	"num_type_CERT":       float64(0),
	"num_type_DNAME":      float64(0),
	"num_type_OPT":        float64(0),
	"num_type_APL":        float64(0),
	"num_type_DS":         float64(0),
	"num_type_SSHFP":      float64(0),
	"num_type_IPSECKEY":   float64(0),
	"num_type_RRSIG":      float64(21),
	"num_type_NSEC":       float64(0),
	"num_type_DNSKEY":     float64(325),
	"num_type_DHCID":      float64(0),
	"num_type_NSEC3":      float64(0),
	"num_type_NSEC3PARAM": float64(0),
	"num_type_TLSA":       float64(35),
	"num_type_SMIMEA":     float64(0),
	"num_type_CDS":        float64(0),
	"num_type_CDNSKEY":    float64(0),
	"num_type_OPENPGPKEY": float64(0),
	"num_type_CSYNC":      float64(0),
	"num_type_SPF":        float64(16),
	"num_type_NID":        float64(0),
	"num_type_L32":        float64(0),
	"num_type_L64":        float64(0),
	"num_type_LP":         float64(0),
	"num_type_EUI48":      float64(0),
	"num_type_EUI64":      float64(0),
	"num_type_TYPE252":    float64(962),
	"num_type_TYPE253":    float64(2),
	"num_type_TYPE255":    float64(1840),
	"num_opcode_QUERY":    float64(75527),
	"num_opcode_NOTIFY":   float64(6),
	"num_class_CLASS0":    float64(6),
	"num_class_IN":        float64(75395),
	"num_class_CH":        float64(132),
	"num_rcode_NOERROR":   float64(65541),
	"num_rcode_FORMERR":   float64(8),
	"num_rcode_SERVFAIL":  float64(0),
	"num_rcode_NXDOMAIN":  float64(6642),
	"num_rcode_NOTIMP":    float64(18),
	"num_rcode_REFUSED":   float64(3341),
	"num_rcode_YXDOMAIN":  float64(0),
	"num_rcode_NOTAUTH":   float64(2),
	"num_edns":            float64(71398),
	"num_ednserr":         float64(0),
	"num_udp":             float64(34111),
	"num_udp6":            float64(40429),
	"num_tcp":             float64(1015),
	"num_tcp6":            float64(2),
	"num_tls":             float64(0),
	"num_tls6":            float64(0),
	"num_answer_wo_aa":    float64(13),
	"num_rxerr":           float64(0),
	"num_txerr":           float64(0),
	"num_raxfr":           float64(954),
	"num_truncated":       float64(1),
	"num_dropped":         float64(5),
	"zone_master":         float64(2),
	"zone_slave":          float64(1),
}

var fullOutput = `server0.queries=75576
num.queries=75557
time.boot=2944405.500253
time.elapsed=2944405.500253
size.db.disk=98304
size.db.mem=22784
size.xfrd.mem=83956312
size.config.disk=0
size.config.mem=6088
num.type.TYPE0=6
num.type.A=46311
num.type.NS=478
num.type.MD=0
num.type.MF=0
num.type.CNAME=272
num.type.SOA=596
num.type.MB=0
num.type.MG=0
num.type.MR=0
num.type.NULL=0
num.type.WKS=0
num.type.PTR=83
num.type.HINFO=1
num.type.MINFO=0
num.type.MX=296
num.type.TXT=794
num.type.RP=0
num.type.AFSDB=0
num.type.X25=0
num.type.ISDN=0
num.type.RT=0
num.type.NSAP=0
num.type.SIG=0
num.type.KEY=1
num.type.PX=0
num.type.AAAA=22736
num.type.LOC=2
num.type.NXT=0
num.type.SRV=93
num.type.NAPTR=5
num.type.KX=0
num.type.CERT=0
num.type.DNAME=0
num.type.OPT=0
num.type.APL=0
num.type.DS=0
num.type.SSHFP=0
num.type.IPSECKEY=0
num.type.RRSIG=21
num.type.NSEC=0
num.type.DNSKEY=325
num.type.DHCID=0
num.type.NSEC3=0
num.type.NSEC3PARAM=0
num.type.TLSA=35
num.type.SMIMEA=0
num.type.CDS=0
num.type.CDNSKEY=0
num.type.OPENPGPKEY=0
num.type.CSYNC=0
num.type.SPF=16
num.type.NID=0
num.type.L32=0
num.type.L64=0
num.type.LP=0
num.type.EUI48=0
num.type.EUI64=0
num.type.TYPE252=962
num.type.TYPE253=2
num.type.TYPE255=1840
num.opcode.QUERY=75527
num.opcode.NOTIFY=6
num.class.CLASS0=6
num.class.IN=75395
num.class.CH=132
num.rcode.NOERROR=65541
num.rcode.FORMERR=8
num.rcode.SERVFAIL=0
num.rcode.NXDOMAIN=6642
num.rcode.NOTIMP=18
num.rcode.REFUSED=3341
num.rcode.YXDOMAIN=0
num.rcode.NOTAUTH=2
num.edns=71398
num.ednserr=0
num.udp=34111
num.udp6=40429
num.tcp=1015
num.tcp6=2
num.tls=0
num.tls6=0
num.answer_wo_aa=13
num.rxerr=0
num.txerr=0
num.raxfr=954
num.truncated=1
num.dropped=5
zone.master=2
zone.slave=1`
