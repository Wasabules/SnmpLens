package simulator

import (
	"fmt"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// dellIDRAC9 is the iDRAC9 of a Dell PowerEdge R750: the controller a server is
// watched through out of band, whatever its operating system is doing.
// IDRAC-MIB gives the server's global and storage status, its power state and
// how long it has been on, its temperatures, fans and power consumption. Its
// alert is one of the iDRAC's, carrying the eleven objects every iDRAC alert
// carries — accessible-for-notify, so no request reads them.
var dellIDRAC9 = model{
	ModelInfo:  ModelInfo{ID: "dell-idrac9", Category: "server"},
	enterprise: 674, // Dell
	build:      buildDellIDRAC9,
	notifications: []Notification{{
		Name:    "alertTemperatureProbeWarning",
		OID:     "." + idrac + "3.2.1.0.2162",
		Objects: idracAlertObjects(),
	}},
}

// idrac is IDRAC-MIB's outOfBandGroup.
const idrac = "1.3.6.1.4.1.674.10892.5."

// idracAlertObjects are alertVariablesGroup, in the MIB's order: message ID,
// message, status, service tag, FQDN, FQDD, display name, arguments, chassis
// service tag, chassis name, and the controller's own FQDN.
func idracAlertObjects() []string {
	out := make([]string, 11)
	for i := range out {
		out[i] = fmt.Sprintf(".%s3.1.%d.0", idrac, i+1)
	}
	return out
}

// serviceTag is a Dell service tag drawn from a seed: seven characters, with
// no vowel to spell anything.
func serviceTag(seed uint64) string {
	const alphabet = "0123456789BCDFGHJKLMNPQRSTVWXYZ"
	b := make([]byte, 7)
	h := mix(seed)
	for i := range b {
		b[i] = alphabet[h%uint64(len(alphabet))]
		h /= uint64(len(alphabet))
	}
	return string(b)
}

func buildDellIDRAC9(id Identity) []Object {
	var o objects
	tag := serviceTag(id.Seed + 5)
	// The server's own name, which is not the controller's.
	fqdn := "r750-" + strings.ToLower(tag) + ".example.com"
	addSystem(&o, id, "Dell Out-of-band SNMP Agent for Remote Access Controller", ".1.3.6.1.4.1.674.10892.5",
		"it@example.com", "Server room, rack B3", 72)
	addInterfaces(&o, id.Seed, []iface{
		{index: 1, descr: "eth0", alias: "iDRAC dedicated port", ifType: ifTypeEthernet, mtu: 1500,
			speed: 1_000_000_000, mac: deviceMAC(id.Seed, 1), up: true, inRate: 6_000, outRate: 9_000},
	})

	// racInfoGroup and systemInfoGroup
	o.add(idrac+"1.1.1.0", gosnmp.OctetString, Const("Integrated Dell Remote Access Controller"))
	o.add(idrac+"1.1.2.0", gosnmp.OctetString, Const("iDRAC"))
	o.add(idrac+"1.1.7.0", gosnmp.Integer, Const(48)) // racType: idrac9Monolithic
	o.add(idrac+"1.1.8.0", gosnmp.OctetString, Const("7.00.60.00"))
	o.add(idrac+"1.3.1.0", gosnmp.OctetString, Const(fqdn))
	o.add(idrac+"1.3.2.0", gosnmp.OctetString, Const(tag))
	o.add(idrac+"1.3.12.0", gosnmp.OctetString, Const("PowerEdge R750"))
	// statusGroup: everything ok, the server on — since the device started.
	o.add(idrac+"2.1.0", gosnmp.Integer, Const(3))    // globalSystemStatus: ok
	o.add(idrac+"2.2.0", gosnmp.Integer, Const(3))    // systemLCDStatus: ok
	o.add(idrac+"2.3.0", gosnmp.Integer, Const(3))    // globalStorageStatus: ok
	o.add(idrac+"2.4.0", gosnmp.Integer, Const(4))    // systemPowerState: on
	o.add(idrac+"2.5.0", gosnmp.Gauge32, SecondsUp()) // systemPowerUpTime: an Unsigned32, seconds

	swing := func(n uint64, period time.Duration) Swing { return Swing{Period: period, Seed: id.Seed + 80000 + n} }
	// A probe table row is indexed by the chassis, then the probe, and says
	// where the probe is; the status is ok(3) throughout.
	probe := func(table string, n int, name string, status int, reading Reading, kind int) {
		col := func(c int) string { return fmt.Sprintf(idrac+"4.%s.1.%d.1.%d", table, c, n) }
		o.add(col(1), gosnmp.Integer, Const(1))
		o.add(col(2), gosnmp.Integer, Const(n))
		o.add(col(5), gosnmp.Integer, Const(status))
		o.add(col(6), gosnmp.Integer, reading)
		if kind != 0 {
			o.add(col(7), gosnmp.Integer, Const(kind))
		}
		o.add(col(8), gosnmp.OctetString, Const(name))
	}
	// temperatureProbeTable, in tenths of a degree
	for i, p := range []struct {
		name   string
		lo, hi float64
	}{
		{"System Board Inlet Temp", 210, 245},
		{"System Board Exhaust Temp", 330, 385},
		{"CPU1 Temp", 480, 620},
		{"CPU2 Temp", 470, 610},
	} {
		probe("700.20", i+1, p.name, 3, IntegerGauge(p.lo, p.hi, swing(uint64(i), 20*time.Minute)), 0)
	}
	// coolingDeviceTable: six fans, in RPM
	for n := 1; n <= 6; n++ {
		probe("700.12", n, fmt.Sprintf("System Board Fan%d", n), 3,
			IntegerGauge(5280, 6960, swing(10+uint64(n), 15*time.Minute)), 0)
	}
	// amperageProbeTable: each supply's current in tenths of an amp
	// (amperageProbeTypeIsPowerSupplyAmps), and what the server draws in watts
	// (amperageProbeTypeIsSystemWatts).
	probe("600.30", 1, "PS1 Current 1", 3, IntegerGauge(8, 14, swing(20, 10*time.Minute)), 23)
	probe("600.30", 2, "PS2 Current 2", 3, IntegerGauge(8, 13, swing(20, 10*time.Minute)), 23)
	probe("600.30", 3, "System Board Pwr Consumption", 3, IntegerGauge(294, 462, swing(20, 10*time.Minute)), 26)

	// alertVariablesGroup: what alertTemperatureProbeWarning says.
	for i, v := range []struct {
		t gosnmp.Asn1BER
		r Reading
	}{
		{gosnmp.OctetString, Const("TMP0120")},
		{gosnmp.OctetString, Const("The system board inlet temperature is greater than the upper warning threshold.")},
		{gosnmp.Integer, Const(4)}, // nonCritical
		{gosnmp.OctetString, Const(tag)},
		{gosnmp.OctetString, Const(fqdn)},
		{gosnmp.OctetString, Const("iDRAC.Embedded.1#SystemBoardInletTemp")},
		{gosnmp.OctetString, Const("System Board Inlet Temp")},
		{gosnmp.OctetString, Const("System Board Inlet Temp")},
		{gosnmp.OctetString, Const(tag)},
		{gosnmp.OctetString, Const("Main System Chassis")},
		{gosnmp.OctetString, Const(id.Name)},
	} {
		o.addForNotify(fmt.Sprintf(idrac+"3.1.%d.0", i+1), v.t, v.r)
	}
	return o
}
