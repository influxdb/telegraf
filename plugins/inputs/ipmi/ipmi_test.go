// ipmi_test
package ipmi

import (
	"testing"

	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const serv = "USERID:PASSW0RD@lan(192.168.1.1)"

const cmdReturn = `
Ambient Temp     | 20 degrees C      | ok
Altitude         | 80 feet           | ok
Avg Power        | 210 Watts         | ok
Planar 3.3V      | 3.29 Volts        | ok
Planar 5V        | 4.90 Volts        | ok
Planar 12V       | 12.04 Volts       | ok
Planar VBAT      | 3.05 Volts        | ok
Fan 1A Tach      | 2610 RPM          | ok
Fan 1B Tach      | 1775 RPM          | ok
Fan 2A Tach      | 2001 RPM          | ok
Fan 2B Tach      | 1275 RPM          | ok
Fan 3A Tach      | 2929 RPM          | ok
Fan 3B Tach      | 2125 RPM          | ok
Fan 1            | 0x00              | ok
Fan 2            | 0x00              | ok
Fan 3            | 0x00              | ok
Front Panel      | 0x00              | ok
Video USB        | 0x00              | ok
DASD Backplane 1 | 0x00              | ok
SAS Riser        | 0x00              | ok
PCI Riser 1      | 0x00              | ok
PCI Riser 2      | 0x00              | ok
CPU 1            | 0x00              | ok
CPU 2            | 0x00              | ok
All CPUs         | 0x00              | ok
One of The CPUs  | 0x00              | ok
IOH Temp Status  | 0x00              | ok
CPU 1 OverTemp   | 0x00              | ok
CPU 2 OverTemp   | 0x00              | ok
CPU Fault Reboot | 0x00              | ok
Aux Log          | 0x00              | ok
NMI State        | 0x00              | ok
ABR Status       | 0x00              | ok
Firmware Error   | 0x00              | ok
PCIs             | 0x00              | ok
CPUs             | 0x00              | ok
DIMMs            | 0x00              | ok
Sys Board Fault  | 0x00              | ok
Power Supply 1   | 0x00              | ok
Power Supply 2   | 0x00              | ok
PS 1 Fan Fault   | 0x00              | ok
PS 2 Fan Fault   | 0x00              | ok
VT Fault         | 0x00              | ok
Pwr Rail A Fault | 0x00              | ok
Pwr Rail B Fault | 0x00              | ok
Pwr Rail C Fault | 0x00              | ok
Pwr Rail D Fault | 0x00              | ok
Pwr Rail E Fault | 0x00              | ok
PS 1 Therm Fault | 0x00              | ok
PS 2 Therm Fault | 0x00              | ok
PS1 12V OV Fault | 0x00              | ok
PS2 12V OV Fault | 0x00              | ok
PS1 12V UV Fault | 0x00              | ok
PS2 12V UV Fault | 0x00              | ok
PS1 12V OC Fault | 0x00              | ok
PS2 12V OC Fault | 0x00              | ok
PS 1 VCO Fault   | 0x00              | ok
PS 2 VCO Fault   | 0x00              | ok
Power Unit       | 0x00              | ok
Cooling Zone 1   | 0x00              | ok
Cooling Zone 2   | 0x00              | ok
Cooling Zone 3   | 0x00              | ok
Drive 0          | 0x00              | ok
Drive 1          | 0x00              | ok
Drive 2          | 0x00              | ok
Drive 3          | 0x00              | ok
Drive 4          | 0x00              | ok
Drive 5          | 0x00              | ok
Drive 6          | 0x00              | ok
Drive 7          | 0x00              | ok
Drive 8          | 0x00              | ok
Drive 9          | 0x00              | ok
Drive 10         | 0x00              | ok
Drive 11         | 0x00              | ok
Drive 12         | 0x00              | ok
Drive 13         | 0x00              | ok
Drive 14         | 0x00              | ok
Drive 15         | 0x00              | ok
All DIMMS        | 0x00              | ok
One of the DIMMs | 0x00              | ok
DIMM 1           | 0x00              | ok
DIMM 2           | 0x00              | ok
DIMM 3           | 0x00              | ok
DIMM 4           | 0x00              | ok
DIMM 5           | 0x00              | ok
DIMM 6           | 0x00              | ok
DIMM 7           | 0x00              | ok
DIMM 8           | 0x00              | ok
DIMM 9           | 0x00              | ok
DIMM 10          | 0x00              | ok
DIMM 11          | 0x00              | ok
DIMM 12          | 0x00              | ok
DIMM 13          | 0x00              | ok
DIMM 14          | 0x00              | ok
DIMM 15          | 0x00              | ok
DIMM 16          | 0x00              | ok
DIMM 17          | 0x00              | ok
DIMM 18          | 0x00              | ok
DIMM 1 Temp      | 0x00              | ok
DIMM 2 Temp      | 0x00              | ok
DIMM 3 Temp      | 0x00              | ok
DIMM 4 Temp      | 0x00              | ok
DIMM 5 Temp      | 0x00              | ok
DIMM 6 Temp      | 0x00              | ok
DIMM 7 Temp      | 0x00              | ok
DIMM 8 Temp      | 0x00              | ok
DIMM 9 Temp      | 0x00              | ok
DIMM 10 Temp     | 0x00              | ok
DIMM 11 Temp     | 0x00              | ok
DIMM 12 Temp     | 0x00              | ok
DIMM 13 Temp     | 0x00              | ok
DIMM 14 Temp     | 0x00              | ok
DIMM 15 Temp     | 0x00              | ok
DIMM 16 Temp     | 0x00              | ok
DIMM 17 Temp     | 0x00              | ok
DIMM 18 Temp     | 0x00              | ok
PCI 1            | 0x00              | ok
PCI 2            | 0x00              | ok
PCI 3            | 0x00              | ok
PCI 4            | 0x00              | ok
All PCI Error    | 0x00              | ok
One of PCI Error | 0x00              | ok
IPMI Watchdog    | 0x00              | ok
Host Power       | 0x00              | ok
DASD Backplane 2 | 0x00              | ok
DASD Backplane 3 | Not Readable      | ns
DASD Backplane 4 | Not Readable      | ns
Backup Memory    | 0x00              | ok
Progress         | 0x00              | ok
Planar Fault     | 0x00              | ok
SEL Fullness     | 0x00              | ok
PCI 5            | 0x00              | ok
OS RealTime Mod  | 0x00              | ok
`

type runnerMock struct {
	out string
	err error
}

func newRunnerMock(out string, err error) Runner {
	return &runnerMock{
		out: out,
		err: err,
	}
}

func (r runnerMock) Run(conn *Connection, args ...string) (out string, err error) {
	if r.err != nil {
		return out, r.err
	}
	return r.out, nil
}

func TestIpmi(t *testing.T) {
	i := &Ipmi{
		Servers: []string{"USERID:PASSW0RD@lan(192.168.1.1)"},
		runner:  newRunnerMock(cmdReturn, nil),
	}

	var acc testutil.Accumulator

	err := i.Gather(&acc)

	require.NoError(t, err)

	assert.Equal(t, acc.NFields(), 2, "non-numeric measurements should be ignored")
}

func TestIpmiConnection(t *testing.T) {
	conn := NewConnection(serv)
	assert.Equal(t, "USERID", conn.Username)
	assert.Equal(t, "lan", conn.Interface)

}
