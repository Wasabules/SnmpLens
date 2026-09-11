package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// unifiU6Pro is a UniFi U6 Pro access point: its uplink, two radios and three
// SSIDs — the office network on both bands and a guest one on 5 GHz.
// UBNT-UniFi-MIB gives what a wireless dashboard polls: each radio's channel use
// and each SSID's clients and traffic. A U6 runs Linux and answers with
// net-snmp's sysObjectID, a sysDescr naming the model, and host resources.
var unifiU6Pro = model{
	ModelInfo:     ModelInfo{ID: "unifi-u6-pro", Category: "wireless"},
	enterprise:    41112, // Ubiquiti
	build:         buildUniFiU6Pro,
	notifications: []Notification{linkNotification(true, 2)}, // eth0, the uplink
}

// The access point's SSIDs, each on an interface of its own, as a UniFi names
// them.
var unifiVAPs = []struct {
	essid, iface, radio string
	channel, txPower    int
	stations            [2]float64
	rx, tx              float64 // octets per second
}{
	{"Office", "ath0", "ng", 6, 20, [2]float64{3, 9}, 90_000, 260_000},
	{"Office", "ath1", "na", 44, 23, [2]float64{8, 22}, 700_000, 2_600_000},
	{"Guest", "ath2", "na", 44, 23, [2]float64{0, 6}, 60_000, 380_000},
}

func buildUniFiU6Pro(id Identity) []Object {
	var o objects
	const version = "6.6.77.15402"
	addSystem(&o, id, "U6-Pro "+version, ".1.3.6.1.4.1.8072.3.2.10", "noc@example.com", "Open space, ceiling", 66)
	ifs := []iface{
		{index: 1, descr: "lo", ifType: ifTypeSoftwareLoopback, mtu: 65536, speed: 10_000_000,
			up: true, inRate: 3_000, outRate: 3_000},
		{index: 2, descr: "eth0", alias: "uplink", ifType: ifTypeEthernet, mtu: 1500, speed: 1_000_000_000,
			mac: deviceMAC(id.Seed, 1), up: true, inRate: 2_900_000, outRate: 900_000, errorRate: 0.0005},
		{index: 3, descr: "wifi0", ifType: ifTypeIEEE80211, mtu: 1500, speed: 573_500_000,
			mac: deviceMAC(id.Seed, 2), up: true, inRate: 90_000, outRate: 260_000},
		{index: 4, descr: "wifi1", ifType: ifTypeIEEE80211, mtu: 1500, speed: 4_800_000_000,
			mac: deviceMAC(id.Seed, 3), up: true, inRate: 760_000, outRate: 2_980_000},
	}
	for i, v := range unifiVAPs {
		ifs = append(ifs, iface{index: 5 + i, descr: v.iface, alias: v.essid, ifType: ifTypeIEEE80211, mtu: 1500,
			speed: ifs[2+min(i, 1)].speed, mac: deviceMAC(id.Seed, uint64(4+i)), up: true, inRate: v.rx, outRate: v.tx})
	}
	ifs = append(ifs, iface{index: 8, descr: "br0", ifType: ifTypeBridge, mtu: 1500,
		mac: deviceMAC(id.Seed, 0), up: true, inRate: 2_800_000, outRate: 850_000})
	addInterfaces(&o, id.Seed, ifs)

	const unifi = "1.3.6.1.4.1.41112.1.6."
	swing := func(n uint64, depth float64, period time.Duration) Swing {
		return Swing{Depth: depth, Period: period, Seed: id.Seed + 45000 + n}
	}
	// unifiRadioTable: how much of each radio's airtime is in use, and how much
	// of that is this access point's own.
	for i, r := range []struct {
		name, radio string
		lo, hi      float64
	}{{"wifi0", "ng", 28, 61}, {"wifi1", "na", 9, 34}} {
		row := i + 1
		col := func(c int) string { return fmt.Sprintf(unifi+"1.1.1.%d.%d", c, row) }
		n := uint64(i) * 4
		o.add(col(1), gosnmp.Integer, Const(row))
		o.add(col(2), gosnmp.OctetString, Const(r.name))
		o.add(col(3), gosnmp.OctetString, Const(r.radio))
		o.add(col(6), gosnmp.Integer, IntegerGauge(r.lo, r.hi, swing(n, 0, 4*time.Minute)))
		o.add(col(7), gosnmp.Integer, IntegerGauge(r.lo*0.3, r.hi*0.3, swing(n+1, 0, 4*time.Minute)))
		o.add(col(8), gosnmp.Integer, IntegerGauge(r.lo*0.2, r.hi*0.25, swing(n+2, 0, 4*time.Minute)))
	}
	// unifiVapTable: one row per SSID and band.
	for i, v := range unifiVAPs {
		row := i + 1
		col := func(c int) string { return fmt.Sprintf(unifi+"1.2.1.%d.%d", c, row) }
		n := 20 + uint64(i)*4
		o.add(col(1), gosnmp.Integer, Const(row))
		o.add(col(4), gosnmp.Integer, Const(v.channel))
		o.add(col(6), gosnmp.OctetString, Const(v.essid))
		o.add(col(7), gosnmp.OctetString, Const(v.iface))
		o.add(col(8), gosnmp.Integer, IntegerGauge(v.stations[0], v.stations[1], swing(n, 0, 30*time.Minute)))
		o.add(col(9), gosnmp.OctetString, Const(v.radio))
		o.add(col(10), gosnmp.Counter32, Counter(v.rx, 0, swing(n+1, 0.6, 5*time.Minute)))
		o.add(col(16), gosnmp.Counter32, Counter(v.tx, 0, swing(n+2, 0.6, 7*time.Minute)))
		o.add(col(21), gosnmp.Integer, Const(v.txPower)) // dBm
		o.add(col(22), gosnmp.Integer, Const(1))         // up: true
	}
	// unifiApSystem
	o.add(unifi+"3.1.0", gosnmp.IPAddress, Const("10.20.0.25"))
	o.add(unifi+"3.2.0", gosnmp.Integer, Const(2)) // isolated: false
	o.add(unifi+"3.3.0", gosnmp.OctetString, Const("U6-Pro"))
	o.add(unifi+"3.5.0", gosnmp.Counter32, Counter(1, 0, Swing{})) // seconds up
	o.add(unifi+"3.6.0", gosnmp.OctetString, Const(version))

	addHostResources(&o, id.Seed, hostResources{
		memoryKiB: 1_015_808,
		users:     [2]float64{0, 1},
		processes: [2]float64{92, 118},
		storage: []storageArea{
			{1, hrStorageRam, "Physical memory", 1024, 1_015_808, 0.41, 0.52, 25 * time.Minute},
			{31, hrStorageFixedDisk, "/", 4096, 7_680, 0.61, 0.62, 12 * time.Hour},
		},
		processors: []int{196608, 196609, 196610, 196611},
		cpuLo:      2,
		cpuHi:      31,
	})
	return o
}
