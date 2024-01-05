//go:generate ../../../tools/readme_config_includer/generator
package ublox

import (
	_ "embed"
	"fmt"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

type UbloxDataCollector struct {
	UbloxPTY string          `toml:"ublox_pty"`
	Log      telegraf.Logger `toml:"-"`

	mut sync.Mutex

	lastPos  *GPSPos
	timeDiff *int64
	err      error
}

func (*UbloxDataCollector) Description() string {
	return "Read ublox metrics"
}

func (*UbloxDataCollector) SampleConfig() string {
	return `
[[inputs.ublox]]
    ublox_pty = "/tmp/ptyGPSRO_tlg"
`
}

// Init is for setup, and validating config.
func (s *UbloxDataCollector) Init() error {
	go func() {
		reader := NewUbloxReader(s.UbloxPTY)
		lastFusionMode := None
		for {
			pos, err := reader.Pop(true)
			if err != nil {
				s.mut.Lock()
				s.err = err
				s.mut.Unlock()
				continue
			} else if pos == nil {
				time.Sleep(time.Second * 1)
				continue
			}

			// aggregate fusion mode
			if pos.FusionMode == None {
				pos.FusionMode = lastFusionMode
			} else {
				lastFusionMode = pos.FusionMode
			}

			if pos.Active {
				now := time.Now()
				td := now.Sub(pos.Ts).Milliseconds()

				s.mut.Lock()
				s.timeDiff = &td
				s.mut.Unlock()
			}

			s.mut.Lock()
			s.lastPos = pos
			s.mut.Unlock()
		}
	}()
	return nil
}

func (s *UbloxDataCollector) Gather(acc telegraf.Accumulator) error {
	s.mut.Lock()
	defer s.mut.Unlock()

	if s.lastPos != nil {
		metrics := make(map[string]interface{}, 12)
		sensors := make(map[string]interface{}, 4)
		sensorsTags := make(map[string]string, 1)

		metrics["active"] = s.lastPos.Active
		metrics["lon"] = s.lastPos.Lon
		metrics["lat"] = s.lastPos.Lat
		metrics["horizontal_acc"] = s.lastPos.HorizontalAcc

		metrics["heading"] = s.lastPos.Heading
		metrics["heading_of_motion"] = s.lastPos.HeadingOfMotion
		metrics["heading_acc"] = s.lastPos.HeadingAcc
		metrics["heading_is_valid"] = s.lastPos.HeadingIsValid

		metrics["speed"] = s.lastPos.Speed
		metrics["speed_acc"] = s.lastPos.SpeedAcc

		metrics["pdop"] = s.lastPos.Pdop
		metrics["sat_num"] = s.lastPos.SatNum
		metrics["fix_type"] = s.lastPos.FixType

		if s.lastPos.FusionMode != None {
			metrics["fusion_mode"] = s.lastPos.FusionMode
		}

		for i := 0; i*4 < len(s.lastPos.Sensors); i++ {
			sensorsTags["name"] = fmt.Sprintf("Sensor %d", i)

			sensors["s_status1"] = s.lastPos.Sensors[i*4+0]
			sensors["s_status2"] = s.lastPos.Sensors[i*4+1]
			sensors["s_freq"] = s.lastPos.Sensors[i*4+2]
			sensors["s_faults"] = s.lastPos.Sensors[i*4+3]

			acc.AddFields("ublox-data-sensors", sensors, sensorsTags)
		}

		s.lastPos = nil

		if s.timeDiff != nil {
			metrics["system_gps_time_diff_ms"] = s.timeDiff

			s.timeDiff = nil
		}

		acc.AddFields("ublox-data", metrics, nil)
	} else if s.err != nil {
		retval := s.err
		s.err = nil
		return retval
	}

	return nil
}

func init() {
	inputs.Add("ublox", func() telegraf.Input { return &UbloxDataCollector{} })
}
