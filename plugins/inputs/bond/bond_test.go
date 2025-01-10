package bond

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

const sampleTestAB = `
Ethernet Channel Bonding Driver: v3.6.0 (September 26, 2009)

Bonding Mode: fault-tolerance (active-backup)
Primary Slave: eth2 (primary_reselect always)
Currently Active Slave: eth2
MII Status: up
MII Polling Interval (ms): 100
Up Delay (ms): 0
Down Delay (ms): 0

Slave Interface: eth3
MII Status: down
Speed: 1000 Mbps
Duplex: full
Link Failure Count: 2
Permanent HW addr:
Slave queue ID: 0

Slave Interface: eth2
MII Status: up
Speed: 100 Mbps
Duplex: full
Link Failure Count: 0
Permanent HW addr:
`

const sampleTestLACP = `
Ethernet Channel Bonding Driver: v3.7.1 (April 27, 2011)

Bonding Mode: IEEE 802.3ad Dynamic link aggregation
Transmit Hash Policy: layer2 (0)
MII Status: up
MII Polling Interval (ms): 100
Up Delay (ms): 0
Down Delay (ms): 0

802.3ad info
LACP rate: fast
Min links: 0
Aggregator selection policy (ad_select): stable

Slave Interface: eth0
MII Status: up
Speed: 10000 Mbps
Duplex: full
Link Failure Count: 2
Permanent HW addr: 3c:ec:ef:5e:71:58
Slave queue ID: 0
Aggregator ID: 2
Actor Churn State: none
Partner Churn State: none
Actor Churned Count: 2
Partner Churned Count: 0

Slave Interface: eth1
MII Status: up
Speed: 10000 Mbps
Duplex: full
Link Failure Count: 1
Permanent HW addr: 3c:ec:ef:5e:71:59
Slave queue ID: 0
Aggregator ID: 2
Actor Churn State: none
Partner Churn State: none
Actor Churned Count: 0
Partner Churned Count: 0
`

const sampleTestLACPFirstUpSecondDown = `
Ethernet Channel Bonding Driver: v3.7.1 (April 27, 2011)

Bonding Mode: IEEE 802.3ad Dynamic link aggregation
Transmit Hash Policy: layer2 (0)
MII Status: up
MII Polling Interval (ms): 100
Up Delay (ms): 0
Down Delay (ms): 0

802.3ad info
LACP rate: fast
Min links: 0
Aggregator selection policy (ad_select): stable

Slave Interface: eth0
MII Status: up
Speed: 10000 Mbps
Duplex: full
Link Failure Count: 2
Permanent HW addr: 3c:ec:ef:5e:71:58
Slave queue ID: 0
Aggregator ID: 2
Actor Churn State: none
Partner Churn State: none
Actor Churned Count: 2
Partner Churned Count: 0

Slave Interface: eth1
MII Status: down
Speed: Unknown
Duplex: Unknown
Link Failure Count: 1
Permanent HW addr: 3c:ec:ef:5e:71:59
Slave queue ID: 0
Aggregator ID: 2
Actor Churn State: none
Partner Churn State: none
Actor Churned Count: 0
Partner Churned Count: 0
`

const sampleSysMode = "802.3ad 5"
const sampleSysSlaves = "eth0 eth1 "
const sampleSysAdPorts = " 2 "

func TestGatherBondInterface(t *testing.T) {
	var acc testutil.Accumulator
	bond := &Bond{}

	require.NoError(t, bond.gatherBondInterface("bondAB", sampleTestAB, &acc))
	acc.AssertContainsTaggedFields(t, "bond", map[string]interface{}{"active_slave": "eth2", "status": 1}, map[string]string{"bond": "bondAB"})
	acc.AssertContainsTaggedFields(
		t,
		"bond_slave",
		map[string]interface{}{"failures": 2, "status": 0},
		map[string]string{"bond": "bondAB", "interface": "eth3"},
	)
	acc.AssertContainsTaggedFields(
		t,
		"bond_slave",
		map[string]interface{}{"failures": 0, "status": 1},
		map[string]string{"bond": "bondAB", "interface": "eth2"},
	)
	acc.AssertContainsTaggedFields(t, "bond_slave", map[string]interface{}{"count": 2}, map[string]string{"bond": "bondAB"})

	acc = testutil.Accumulator{}
	require.NoError(t, bond.gatherBondInterface("bondLACP", sampleTestLACP, &acc))
	gatherSysDetails("bondLACP", sysFiles{ModeFile: sampleSysMode, SlaveFile: sampleSysSlaves, ADPortsFile: sampleSysAdPorts}, &acc)
	acc.AssertContainsTaggedFields(t, "bond", map[string]interface{}{"status": 1}, map[string]string{"bond": "bondLACP"})
	acc.AssertContainsTaggedFields(
		t,
		"bond_slave",
		map[string]interface{}{"failures": 2, "status": 1, "actor_churned": 2, "partner_churned": 0, "total_churned": 2},
		map[string]string{"bond": "bondLACP", "interface": "eth0"},
	)
	acc.AssertContainsTaggedFields(
		t,
		"bond_slave",
		map[string]interface{}{"failures": 1, "status": 1, "actor_churned": 0, "partner_churned": 0, "total_churned": 0},
		map[string]string{"bond": "bondLACP", "interface": "eth1"},
	)
	acc.AssertContainsTaggedFields(t, "bond_slave", map[string]interface{}{"count": 2}, map[string]string{"bond": "bondLACP"})
	acc.AssertContainsTaggedFields(
		t,
		"bond_sys",
		map[string]interface{}{"slave_count": 2, "ad_port_count": 2},
		map[string]string{"bond": "bondLACP", "mode": "802.3ad"},
	)

	acc = testutil.Accumulator{}
	require.NoError(t, bond.gatherBondInterface("bondLACPUpDown", sampleTestLACPFirstUpSecondDown, &acc))
	gatherSysDetails("bondLACPUpDown", sysFiles{ModeFile: sampleSysMode, SlaveFile: sampleSysSlaves, ADPortsFile: sampleSysAdPorts}, &acc)
	acc.AssertContainsTaggedFields(t, "bond", map[string]interface{}{"status": 1}, map[string]string{"bond": "bondLACPUpDown"})
	acc.AssertContainsTaggedFields(
		t,
		"bond_slave",
		map[string]interface{}{"failures": 2, "status": 1, "actor_churned": 2, "partner_churned": 0, "total_churned": 2},
		map[string]string{"bond": "bondLACPUpDown", "interface": "eth0"},
	)
	acc.AssertContainsTaggedFields(
		t,
		"bond_slave",
		map[string]interface{}{"failures": 1, "status": 0, "actor_churned": 0, "partner_churned": 0, "total_churned": 0},
		map[string]string{"bond": "bondLACPUpDown", "interface": "eth1"},
	)
	acc.AssertContainsTaggedFields(t, "bond_slave", map[string]interface{}{"count": 2}, map[string]string{"bond": "bondLACPUpDown"})
	acc.AssertContainsTaggedFields(
		t,
		"bond_sys",
		map[string]interface{}{"slave_count": 2, "ad_port_count": 2},
		map[string]string{"bond": "bondLACPUpDown", "mode": "802.3ad"},
	)
}
