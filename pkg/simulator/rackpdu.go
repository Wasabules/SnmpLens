package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// apcRackPDU is an APC AP7921B switched rack PDU: eight outlets on one 16 A
// phase, the last of them switched off. PowerNet-MIB's rPDU group gives the
// phase load, the power the PDU draws and each outlet's name and state. Its
// notifications are PowerNet's SNMPv1 traps under apc, as RFC 3584 translates
// them: enterprises.318.0.<specific-trap>.
var apcRackPDU = model{
	ModelInfo:  ModelInfo{ID: "apc-rack-pdu", Category: "power"},
	enterprise: 318, // APC
	build:      buildAPCRackPDU,
	notifications: []Notification{
		{Name: "rPDUOutletOff", OID: ".1.3.6.1.4.1.318.0.269", Objects: []string{
			"." + rPDU + "1.6.0", "." + rPDU + "1.1.0", // rPDUIdentSerialNumber, rPDUIdentName
			"." + rPDU + "3.3.1.1.1.8", "." + rPDU + "3.3.1.1.2.8", // the outlet's index and name
			"." + mtrapargsString,
		}},
		{Name: "rPDUNearOverload", OID: ".1.3.6.1.4.1.318.0.274", Objects: []string{
			"." + rPDU + "1.6.0", "." + rPDU + "1.1.0",
			"." + rPDU + "2.3.1.1.4.1", // rPDULoadStatusPhaseNumber
		}},
	},
}

const (
	rPDU = "1.3.6.1.4.1.318.1.1.12."
	// mtrapargsString is the text PowerNet's traps carry.
	mtrapargsString = "1.3.6.1.4.1.318.2.3.3.0"
)

// rackOutlets are the PDU's outlets, named after what they feed.
var rackOutlets = []string{
	"srv-web-01 PSU1", "srv-web-02 PSU1", "srv-db-01 PSU1", "sw-top-of-rack",
	"fw-01", "nas-01", "kvm", "spare",
}

func buildAPCRackPDU(id Identity) []Object {
	var o objects
	serial := fmt.Sprintf("5A%04dE%05d", 1900+int(unit(id.Seed+1)*4), int(unit(id.Seed+2)*100000))
	addSystem(&o, id, "APC Web/SNMP Management Card (MB:v4.1.0 PF:v6.4.6 PN:apc_hw05_aos_646.bin AF1:v6.4.6 "+
		"AN1:apc_hw05_rpdu_646.bin MN:AP7921B HR:B2 SN: "+serial+" MD:06/12/2019) "+
		"(Embedded PowerNet SNMP Agent SW v2.2 compatible)",
		".1.3.6.1.4.1.318.1.3.4.5", // masterSwitchrPDU
		"facilities@example.com", "Rack A2, rear", 72)
	addInterfaces(&o, id.Seed, []iface{
		{index: 1, descr: "eth0", ifType: ifTypeEthernet, mtu: 1500, speed: 100_000_000,
			mac: deviceMAC(id.Seed, 1), up: true, inRate: 300, outRate: 600},
	})

	// The load and the power drawn move together: 230 V times 4.2 to 6.8 A.
	load := Swing{Period: 9 * time.Minute, Seed: id.Seed + 70000}
	// rPDUIdent
	o.add(rPDU+"1.1.0", gosnmp.OctetString, Const(id.Name))
	o.add(rPDU+"1.2.0", gosnmp.OctetString, Const("B2"))
	o.add(rPDU+"1.3.0", gosnmp.OctetString, Const("v6.4.6"))
	o.add(rPDU+"1.5.0", gosnmp.OctetString, Const("AP7921B"))
	o.add(rPDU+"1.6.0", gosnmp.OctetString, Const(serial))
	o.add(rPDU+"1.7.0", gosnmp.Integer, Const(16)) // rated amps
	o.add(rPDU+"1.8.0", gosnmp.Integer, Const(len(rackOutlets)))
	o.add(rPDU+"1.9.0", gosnmp.Integer, Const(1))
	o.add(rPDU+"1.16.0", gosnmp.Integer, IntegerGauge(966, 1564, load)) // watts
	// rPDULoadDevice, and rPDULoadStatusTable's one phase
	o.add(rPDU+"2.1.1.0", gosnmp.Integer, Const(16))
	o.add(rPDU+"2.1.2.0", gosnmp.Integer, Const(1))
	o.add(rPDU+"2.3.1.1.1.1", gosnmp.Integer, Const(1))
	o.add(rPDU+"2.3.1.1.2.1", gosnmp.Gauge32, Gauge(42, 68, load)) // tenths of an amp
	o.add(rPDU+"2.3.1.1.3.1", gosnmp.Integer, Const(1))            // phaseLoadNormal
	o.add(rPDU+"2.3.1.1.4.1", gosnmp.Integer, Const(1))
	// rPDUOutletControlTable, which the notifications name an outlet from, and
	// rPDUOutletStatusTable.
	for i, name := range rackOutlets {
		n := i + 1
		state := 1 // outletStatusOn
		if name == "spare" {
			state = 2
		}
		o.add(fmt.Sprintf(rPDU+"3.3.1.1.1.%d", n), gosnmp.Integer, Const(n))
		o.add(fmt.Sprintf(rPDU+"3.3.1.1.2.%d", n), gosnmp.OctetString, Const(name))
		o.add(fmt.Sprintf(rPDU+"3.5.1.1.1.%d", n), gosnmp.Integer, Const(n))
		o.add(fmt.Sprintf(rPDU+"3.5.1.1.2.%d", n), gosnmp.OctetString, Const(name))
		o.add(fmt.Sprintf(rPDU+"3.5.1.1.3.%d", n), gosnmp.Integer, Const(1)) // phase 1
		o.add(fmt.Sprintf(rPDU+"3.5.1.1.4.%d", n), gosnmp.Integer, Const(state))
	}
	o.addForNotify(mtrapargsString, gosnmp.OctetString, Const("Outlet 8, spare, turned off."))
	return o
}
