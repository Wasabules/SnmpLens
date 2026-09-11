package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// mikrotikRB4011 is a MikroTik RB4011 on RouterOS 7: ten gigabit ports, the
// first the uplink and the last four unused; an SFP+ cage with nothing in it;
// the bridge the LAN ports are in. MIKROTIK-MIB's health group gives the board's
// voltage, temperatures, power and current, and RouterOS answers
// HOST-RESOURCES-MIB for its memory, its flash and its four cores — so it feeds
// the "Server" preset as well as the interface ones.
var mikrotikRB4011 = model{
	ModelInfo:  ModelInfo{ID: "mikrotik-rb4011", Category: "network"},
	enterprise: 14988, // MikroTik
	build:      buildMikrotikRB4011,
	notifications: []Notification{
		linkNotification(false, 11), // sfp-sfpplus1, empty
		linkNotification(true, 1),   // ether1, the uplink
		// MIKROTIK-MIB gives it no objects.
		{Name: "mtxrTemperatureException", OID: ".1.3.6.1.4.1.14988.1.1.9.0.2"},
	},
}

func buildMikrotikRB4011(id Identity) []Object {
	var o objects
	const version = "7.14.3"
	addSystem(&o, id, "RouterOS RB4011iGS+", ".1.3.6.1.4.1.14988.1", "noc@example.com",
		"Branch office, network room", 78)
	ifs := make([]iface, 0, 12)
	for p := 1; p <= 10; p++ {
		f := iface{index: p, descr: fmt.Sprintf("ether%d", p), ifType: ifTypeEthernet, mtu: 1500,
			speed: 1_000_000_000, mac: deviceMAC(id.Seed, uint64(p))}
		switch {
		case p == 1:
			f.alias, f.up, f.inRate, f.outRate, f.errorRate = "uplink", true, 5_600_000, 1_700_000, 0.001
		case p <= 6:
			f.up = true
			f.inRate = 20_000 + 900_000*unit(id.Seed+8100+uint64(p))
			f.outRate = 60_000 + 2_400_000*unit(id.Seed+8200+uint64(p))
		}
		ifs = append(ifs, f)
	}
	ifs = append(ifs,
		iface{index: 11, descr: "sfp-sfpplus1", ifType: ifTypeEthernet, mtu: 1500, speed: 10_000_000_000,
			mac: deviceMAC(id.Seed, 11)},
		// The bridge carries the router's own address, and its base MAC — the
		// one its engine ID carries too.
		iface{index: 12, descr: "bridge", alias: "LAN", ifType: ifTypeBridge, mtu: 1500,
			mac: deviceMAC(id.Seed, 0), up: true, inRate: 1_800_000, outRate: 5_000_000},
	)
	addInterfaces(&o, id.Seed, ifs)

	// mtxrHealth, in the units RouterOS reports: tenths of a volt, of a degree
	// and of a watt, and milliamps.
	const health = "1.3.6.1.4.1.14988.1.1.3."
	swing := func(n uint64, period time.Duration) Swing { return Swing{Period: period, Seed: id.Seed + 40000 + n} }
	o.add(health+"8.0", gosnmp.Integer, IntegerGauge(238, 242, swing(0, 30*time.Minute)))  // mtxrHlVoltage
	o.add(health+"10.0", gosnmp.Integer, IntegerGauge(390, 440, swing(1, 50*time.Minute))) // mtxrHlTemperature
	o.add(health+"11.0", gosnmp.Integer, IntegerGauge(470, 560, swing(2, 12*time.Minute))) // mtxrHlProcessorTemperature
	// The current follows the power: one swing for both.
	o.add(health+"12.0", gosnmp.Integer, IntegerGauge(142, 188, swing(3, 9*time.Minute))) // mtxrHlPower
	o.add(health+"13.0", gosnmp.Integer, IntegerGauge(590, 780, swing(3, 9*time.Minute))) // mtxrHlCurrent
	o.add("1.3.6.1.4.1.14988.1.1.4.4.0", gosnmp.OctetString, Const(version))              // mtxrLicVersion
	o.add("1.3.6.1.4.1.14988.1.1.7.3.0", gosnmp.OctetString, Const(fmt.Sprintf("%012X", mix(id.Seed+3)&0xFFFFFFFFFFFF)))
	o.add("1.3.6.1.4.1.14988.1.1.7.4.0", gosnmp.OctetString, Const(version)) // mtxrFirmwareVersion

	addHostResources(&o, id.Seed, hostResources{
		memoryKiB: 1048576, // 1 GiB
		users:     [2]float64{0, 1},
		processes: [2]float64{38, 44},
		storage: []storageArea{
			{65536, hrStorageRam, "main memory", 1024, 1048576, 0.17, 0.23, 20 * time.Minute},
			{131072, hrStorageFixedDisk, "system disk", 1024, 524288, 0.09, 0.10, 12 * time.Hour},
		},
		processors: []int{1, 2, 3, 4},
		cpuLo:      1,
		cpuHi:      24,
	})
	return o
}
