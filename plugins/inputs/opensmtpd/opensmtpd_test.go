package opensmtpd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

func smtpCTL(output string) func(string, config.Duration, bool) (*bytes.Buffer, error) {
	return func(string, config.Duration, bool) (*bytes.Buffer, error) {
		return bytes.NewBufferString(output), nil
	}
}

func TestFilterSomeStats(t *testing.T) {
	acc := &testutil.Accumulator{}
	v := &Opensmtpd{
		run: smtpCTL(fullOutput),
	}
	err := v.Gather(acc)

	require.NoError(t, err)
	require.True(t, acc.HasMeasurement("opensmtpd"))
	require.Equal(t, uint64(1), acc.NMetrics())

	require.Equal(t, 36, acc.NFields())
	acc.AssertContainsFields(t, "opensmtpd", parsedFullOutput)
}

var parsedFullOutput = map[string]interface{}{
	"bounce_envelope":             float64(0),
	"bounce_message":              float64(0),
	"bounce_session":              float64(0),
	"control_session":             float64(1),
	"mda_envelope":                float64(0),
	"mda_pending":                 float64(0),
	"mda_running":                 float64(0),
	"mda_user":                    float64(0),
	"mta_connector":               float64(1),
	"mta_domain":                  float64(1),
	"mta_envelope":                float64(0),
	"mta_host":                    float64(6),
	"mta_relay":                   float64(1),
	"mta_route":                   float64(1),
	"mta_session":                 float64(1),
	"mta_source":                  float64(1),
	"mta_task":                    float64(0),
	"mta_task_running":            float64(5),
	"queue_bounce":                float64(11495),
	"queue_evpcache_load_hit":     float64(3927539),
	"queue_evpcache_size":         float64(0),
	"queue_evpcache_update_hit":   float64(508),
	"scheduler_delivery_ok":       float64(1922951),
	"scheduler_delivery_permfail": float64(45967),
	"scheduler_delivery_tempfail": float64(493),
	"scheduler_envelope":          float64(0),
	"scheduler_envelope_expired":  float64(17),
	"scheduler_envelope_incoming": float64(0),
	"scheduler_envelope_inflight": float64(0),
	"scheduler_ramqueue_envelope": float64(0),
	"scheduler_ramqueue_message":  float64(0),
	"scheduler_ramqueue_update":   float64(0),
	"smtp_session":                float64(0),
	"smtp_session_inet4":          float64(1903412),
	"smtp_session_local":          float64(10827),
	"uptime":                      float64(9253995),
}

var fullOutput = `bounce.envelope=0
bounce.message=0
bounce.session=0
control.session=1
mda.envelope=0
mda.pending=0
mda.running=0
mda.user=0
mta.connector=1
mta.domain=1
mta.envelope=0
mta.host=6
mta.relay=1
mta.route=1
mta.session=1
mta.source=1
mta.task=0
mta.task.running=5
queue.bounce=11495
queue.evpcache.load.hit=3927539
queue.evpcache.size=0
queue.evpcache.update.hit=508
scheduler.delivery.ok=1922951
scheduler.delivery.permfail=45967
scheduler.delivery.tempfail=493
scheduler.envelope=0
scheduler.envelope.expired=17
scheduler.envelope.incoming=0
scheduler.envelope.inflight=0
scheduler.ramqueue.envelope=0
scheduler.ramqueue.message=0
scheduler.ramqueue.update=0
smtp.session=0
smtp.session.inet4=1903412
smtp.session.local=10827
uptime=9253995
uptime.human=107d2h33m15s`
