//go:build linux

package wireless

import (
	"bytes"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

// length of wireless interface fields
const interfaceFieldLength = 10

var newLineByte = []byte("\n")

type wirelessInterface struct {
	Interface string
	Status    int64
	Link      int64
	Level     int64
	Noise     int64
	Nwid      int64
	Crypt     int64
	Frag      int64
	Retry     int64
	Misc      int64
	Beacon    int64
}

// Gather collects the wireless information.
func (w *Wireless) Gather(acc telegraf.Accumulator) error {
	// load proc path, get default value if config value and env variable are empty
	if w.HostProc == "" {
		w.HostProc = internal.GetProcPath()
	}

	wirelessPath := path.Join(w.HostProc, "net", "wireless")
	table, err := os.ReadFile(wirelessPath)
	if err != nil {
		return err
	}

	interfaces, err := w.loadWirelessTable(table)
	if err != nil {
		return err
	}
	for _, w := range interfaces {
		tags := map[string]string{
			"interface": w.Interface,
		}
		fieldsG := map[string]interface{}{
			"status": w.Status,
			"link":   w.Link,
			"level":  w.Level,
			"noise":  w.Noise,
		}
		fieldsC := map[string]interface{}{
			"nwid":   w.Nwid,
			"crypt":  w.Crypt,
			"frag":   w.Frag,
			"retry":  w.Retry,
			"misc":   w.Misc,
			"beacon": w.Beacon,
		}
		acc.AddGauge("wireless", fieldsG, tags)
		acc.AddCounter("wireless", fieldsC, tags)
	}

	return nil
}

func (w *Wireless) loadWirelessTable(table []byte) ([]*wirelessInterface, error) {
	var wi []*wirelessInterface
	lines := bytes.Split(table, newLineByte)

	// iterate over interfaces
	for i := 2; i < len(lines); i = i + 1 {
		if len(lines[i]) == 0 {
			continue
		}
		values := make([]int64, 0, interfaceFieldLength)
		fields := strings.Fields(string(lines[i]))
		for j := 1; j < len(fields); j = j + 1 {
			v, err := strconv.ParseInt(strings.Trim(fields[j], "."), 10, 64)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
		}
		if len(values) != interfaceFieldLength {
			w.Log.Error("invalid length of interface values")
			continue
		}
		wi = append(wi, &wirelessInterface{
			Interface: strings.Trim(fields[0], ":"),
			Status:    values[0],
			Link:      values[1],
			Level:     values[2],
			Noise:     values[3],
			Nwid:      values[4],
			Crypt:     values[5],
			Frag:      values[6],
			Retry:     values[7],
			Misc:      values[8],
			Beacon:    values[9],
		})
	}
	return wi, nil
}

func init() {
	inputs.Add("wireless", func() telegraf.Input {
		return &Wireless{}
	})
}
