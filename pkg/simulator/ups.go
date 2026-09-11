package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// apcSmartUPS is a three-phase APC Smart-UPS on its network management card,
// answering the standard UPS-MIB (RFC 1628): the MIB the "UPS (RFC 1628)" preset
// polls, three output lines included — which is why it is the three-phase model
// rather than a single-phase one. It runs on mains, charged.
var apcSmartUPS = model{
	ModelInfo:  ModelInfo{ID: "apc-smart-ups", Category: "power"},
	enterprise: 318, // APC
	build:      buildAPCSmartUPS,
	notifications: []Notification{{
		Name: "upsTrapOnBattery",
		OID:  ".1.3.6.1.2.1.33.2.1",
		// upsEstimatedMinutesRemaining, upsSecondsOnBattery, upsConfigLowBattTime
		Objects: []string{".1.3.6.1.2.1.33.1.2.3.0", ".1.3.6.1.2.1.33.1.2.2.0", ".1.3.6.1.2.1.33.1.9.7.0"},
	}},
}

func buildAPCSmartUPS(id Identity) []Object {
	var o objects
	serial := fmt.Sprintf("5A%04dT%05d", 2100+int(unit(id.Seed+1)*4), int(unit(id.Seed+2)*100000))
	addSystem(&o, id, "APC Web/SNMP Management Card (MB:v4.1.0 PF:v6.9.6 PN:apc_hw05_aos_696.bin AF1:v6.9.6 "+
		"AN1:apc_hw05_sumx_696.bin MN:SUVTP10KH3B4S HR:05 SN: "+serial+" MD:03/14/2021) "+
		"(Embedded PowerNet SNMP Agent SW v2.2 compatible)",
		".1.3.6.1.4.1.318.1.3.17.1", // smartUPS3Phase10kVA
		"facilities@example.com", "Server room, UPS bay", 72)
	addInterfaces(&o, id.Seed, []iface{
		{index: 1, descr: "eth0", ifType: ifTypeEthernet, mtu: 1500, speed: 100_000_000,
			mac: deviceMAC(id.Seed, 1), up: true, inRate: 400, outRate: 700},
	})

	const ups = "1.3.6.1.2.1.33.1."
	swing := func(n uint64, period time.Duration) Swing { return Swing{Period: period, Seed: id.Seed + 20000 + n} }
	// upsIdent
	o.add(ups+"1.1.0", gosnmp.OctetString, Const("APC"))
	o.add(ups+"1.2.0", gosnmp.OctetString, Const("Smart-UPS VT 10kVA"))
	o.add(ups+"1.3.0", gosnmp.OctetString, Const("UPS 09.1 / ID=1015"))
	o.add(ups+"1.4.0", gosnmp.OctetString, Const("v6.9.6"))
	o.add(ups+"1.5.0", gosnmp.OctetString, Const(id.Name))
	o.add(ups+"1.6.0", gosnmp.OctetString, Const("Racks A1 to A4"))
	// upsBattery
	o.add(ups+"2.1.0", gosnmp.Integer, Const(2))                                           // upsBatteryStatus: normal
	o.add(ups+"2.2.0", gosnmp.Integer, Const(0))                                           // upsSecondsOnBattery
	o.add(ups+"2.3.0", gosnmp.Integer, IntegerGauge(38, 44, swing(0, 20*time.Minute)))     // minutes remaining
	o.add(ups+"2.4.0", gosnmp.Integer, IntegerGauge(99, 100, swing(1, 40*time.Minute)))    // % charge remaining
	o.add(ups+"2.5.0", gosnmp.Integer, IntegerGauge(2180, 2200, swing(2, 15*time.Minute))) // 0.1 V DC
	o.add(ups+"2.6.0", gosnmp.Integer, Const(0))                                           // 0.1 A: not discharging
	o.add(ups+"2.7.0", gosnmp.Integer, IntegerGauge(24, 27, swing(3, 3*time.Hour)))        // °C
	// upsInput, one line per phase
	o.add(ups+"3.1.0", gosnmp.Counter32, Const(uint32(0))) // upsInputLineBads
	o.add(ups+"3.2.0", gosnmp.Integer, Const(3))
	for line := 1; line <= 3; line++ {
		n := 10 + uint64(line)*8
		col := func(c int) string { return fmt.Sprintf(ups+"3.3.1.%d.%d", c, line) }
		o.add(col(1), gosnmp.Integer, Const(line))
		o.add(col(2), gosnmp.Integer, IntegerGauge(499, 501, swing(n, 3*time.Minute)))     // 0.1 Hz
		o.add(col(3), gosnmp.Integer, IntegerGauge(228, 234, swing(n+1, 9*time.Minute)))   // V RMS
		o.add(col(4), gosnmp.Integer, IntegerGauge(95, 140, swing(n+2, 7*time.Minute)))    // 0.1 A RMS
		o.add(col(5), gosnmp.Integer, IntegerGauge(2100, 3100, swing(n+3, 7*time.Minute))) // W
	}
	// upsOutput, one line per phase, each loaded differently
	o.add(ups+"4.1.0", gosnmp.Integer, Const(3)) // upsOutputSource: normal, which is mains
	o.add(ups+"4.2.0", gosnmp.Integer, Const(500))
	o.add(ups+"4.3.0", gosnmp.Integer, Const(3))
	for i, load := range [][2]float64{{34, 42}, {41, 48}, {28, 37}} {
		line := i + 1
		n := 50 + uint64(line)*8
		col := func(c int) string { return fmt.Sprintf(ups+"4.4.1.%d.%d", c, line) }
		o.add(col(1), gosnmp.Integer, Const(line))
		o.add(col(2), gosnmp.Integer, Const(230))
		o.add(col(3), gosnmp.Integer, IntegerGauge(80+load[0], 80+load[1], swing(n, 6*time.Minute)))   // 0.1 A RMS
		o.add(col(4), gosnmp.Integer, IntegerGauge(load[0]*55, load[1]*55, swing(n+1, 6*time.Minute))) // W
		o.add(col(5), gosnmp.Integer, IntegerGauge(load[0], load[1], swing(n+2, 6*time.Minute)))       // %
	}
	o.add(ups+"6.1.0", gosnmp.Gauge32, Const(uint32(0))) // upsAlarmsPresent
	// upsConfig: 230 V and 50 Hz in and out, 10 kVA, 8 kW, two minutes' warning
	// of a low battery, the alarm on, and the transfer points.
	for i, v := range []int{230, 50, 230, 50, 10000, 8000, 2, 2, 160, 282} {
		o.add(fmt.Sprintf(ups+"9.%d.0", i+1), gosnmp.Integer, Const(v))
	}
	return o
}
