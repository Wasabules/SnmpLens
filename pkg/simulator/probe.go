package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// environmentProbe is a temperature and humidity probe answering the standard
// ENTITY-SENSOR-MIB (RFC 3433) rather than a vendor's: two sensors in
// ENTITY-MIB's physical table, and their readings, as a server room's cold aisle
// reads. The MIB defines no notification, and the probe sends only the generic
// ones.
var environmentProbe = model{
	ModelInfo:  ModelInfo{ID: "environment-probe", Category: "environment"},
	enterprise: 8072, // an embedded net-snmp
	build:      buildEnvironmentProbe,
}

func buildEnvironmentProbe(id Identity) []Object {
	var o objects
	addSystem(&o, id, "Environment monitor, 2 sensors (temperature, humidity), firmware 2.4.1",
		".1.3.6.1.4.1.8072.3.2.10", "facilities@example.com", "Server room, cold aisle", 72)
	addInterfaces(&o, id.Seed, []iface{
		{index: 1, descr: "eth0", ifType: ifTypeEthernet, mtu: 1500, speed: 100_000_000,
			mac: deviceMAC(id.Seed, 1), up: true, inRate: 300, outRate: 500},
	})

	// ENTITY-MIB's physical table: the probe, and the two sensors in it.
	const physical = "1.3.6.1.2.1.47.1.1.1.1."
	for _, e := range []struct {
		index            int
		descr, name      string
		class, container int
	}{
		{1, "Environment monitor", "chassis", 3, 0}, // chassis
		{2, "Temperature sensor", "temperature", 8, 1},
		{3, "Humidity sensor", "humidity", 8, 1}, // sensor
	} {
		o.add(fmt.Sprintf(physical+"2.%d", e.index), gosnmp.OctetString, Const(e.descr))
		o.add(fmt.Sprintf(physical+"4.%d", e.index), gosnmp.Integer, Const(e.container))
		o.add(fmt.Sprintf(physical+"5.%d", e.index), gosnmp.Integer, Const(e.class))
		o.add(fmt.Sprintf(physical+"7.%d", e.index), gosnmp.OctetString, Const(e.name))
	}

	// ENTITY-SENSOR-MIB: the readings, in tenths.
	const sensor = "1.3.6.1.2.1.99.1.1.1."
	for i, s := range []struct {
		index, kind int
		lo, hi      float64
		units       string
		period      time.Duration
	}{
		{2, 8, 215, 245, "degrees Celsius", 50 * time.Minute}, // celsius: 21.5 to 24.5
		{3, 9, 380, 460, "percent RH", 70 * time.Minute},      // percentRH: 38 to 46
	} {
		col := func(c int) string { return fmt.Sprintf(sensor+"%d.%d", c, s.index) }
		o.add(col(1), gosnmp.Integer, Const(s.kind))
		o.add(col(2), gosnmp.Integer, Const(9)) // scale: units
		o.add(col(3), gosnmp.Integer, Const(1)) // precision: one decimal
		o.add(col(4), gosnmp.Integer, IntegerGauge(s.lo, s.hi, Swing{Period: s.period, Seed: id.Seed + 50000 + uint64(i)}))
		o.add(col(5), gosnmp.Integer, Const(1)) // operational status: ok
		o.add(col(6), gosnmp.OctetString, Const(s.units))
		o.add(col(7), gosnmp.TimeTicks, Uptime())           // read continuously: the last reading is now
		o.add(col(8), gosnmp.Gauge32, Const(uint32(10000))) // update rate, in milliseconds
	}
	return o
}
