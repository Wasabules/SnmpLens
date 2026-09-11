package simulator

import (
	"fmt"
	"net"
	"time"

	"github.com/gosnmp/gosnmp"
)

// linuxServer is an Ubuntu host running net-snmp: the system group, three
// interfaces — the loopback, one busy, one cabled and down, which is what an
// interface wall looks like on a real server — and the host resources.
//
// It answers everything the bundled "Interfaces" and "Host resources" presets
// poll, and TestTheLinuxModelFeedsItsPresets holds it to that: binding a preset
// to a simulated server and getting empty widgets would be a demonstration of
// the wrong thing.
var linuxServer = model{
	ModelInfo:  ModelInfo{ID: "linux-server", Category: "server"},
	enterprise: 8072, // net-snmp
	build:      buildLinuxServer,
}

type linuxInterface struct {
	index           int
	name, alias     string
	ifType, mtu     int
	speed           uint32 // bits per second
	mac             net.HardwareAddr
	up              bool
	inRate, outRate float64 // octets per second
	errorRate       float64 // input errors per second
}

func buildLinuxServer(id Identity) []Object {
	var o objects
	swing := func(n uint64, depth float64, period time.Duration) Swing {
		return Swing{Depth: depth, Period: period, Seed: id.Seed + n}
	}
	// A starting count drawn from the seed, so two servers' counters differ
	// and a 32-bit one wraps within minutes of the device starting.
	start := func(n uint64, base, spread float64) uint64 {
		return uint64(base + spread*unit(id.Seed+1000+n))
	}

	// system (RFC 3418)
	o.add("1.3.6.1.2.1.1.1.0", gosnmp.OctetString,
		Const(fmt.Sprintf("Linux %s 6.8.0-45-generic #45-Ubuntu SMP PREEMPT_DYNAMIC x86_64", id.Name)))
	o.add("1.3.6.1.2.1.1.2.0", gosnmp.ObjectIdentifier, Const(".1.3.6.1.4.1.8072.3.2.10"))
	o.add("1.3.6.1.2.1.1.3.0", gosnmp.TimeTicks, Uptime())
	o.add("1.3.6.1.2.1.1.4.0", gosnmp.OctetString, Const("root@"+id.Name))
	o.add("1.3.6.1.2.1.1.5.0", gosnmp.OctetString, Const(id.Name))
	o.add("1.3.6.1.2.1.1.6.0", gosnmp.OctetString, Const("Server room, rack A1"))
	o.add("1.3.6.1.2.1.1.7.0", gosnmp.Integer, Const(72))

	// interfaces (RFC 2863): ifTable, then ifXTable
	ifs := []linuxInterface{
		{1, "lo", "", 24, 65536, 10_000_000, nil, true, 20_000, 20_000, 0},
		{2, "eth0", "uplink", 6, 1500, 1_000_000_000, deviceMAC(id.Seed, 1), true, 1_250_000, 310_000, 0.004},
		{3, "eth1", "spare", 6, 1500, 1_000_000_000, deviceMAC(id.Seed, 2), false, 0, 0, 0},
	}
	o.add("1.3.6.1.2.1.2.1.0", gosnmp.Integer, Const(len(ifs)))
	for _, f := range ifs {
		col := func(n int) string { return fmt.Sprintf("1.3.6.1.2.1.2.2.1.%d.%d", n, f.index) }
		ext := func(n int) string { return fmt.Sprintf("1.3.6.1.2.1.31.1.1.1.%d.%d", n, f.index) }
		n := uint64(f.index) * 16
		oper := 2 // down
		if f.up {
			oper = 1
		}
		inOctets := func() (float64, uint64, Swing) {
			return f.inRate, start(n, 3.0e9, 1.0e9), swing(n, 0.6, 5*time.Minute)
		}
		outOctets := func() (float64, uint64, Swing) {
			return f.outRate, start(n+1, 1.0e9, 2.0e9), swing(n+1, 0.5, 7*time.Minute)
		}

		o.add(col(1), gosnmp.Integer, Const(f.index))
		o.add(col(2), gosnmp.OctetString, Const(f.name))
		o.add(col(3), gosnmp.Integer, Const(f.ifType))
		o.add(col(4), gosnmp.Integer, Const(f.mtu))
		o.add(col(5), gosnmp.Gauge32, Const(f.speed))
		o.add(col(6), gosnmp.OctetString, Const([]byte(f.mac)))
		o.add(col(7), gosnmp.Integer, Const(1)) // administratively up
		o.add(col(8), gosnmp.Integer, Const(oper))
		o.add(col(9), gosnmp.TimeTicks, Const(uint32(0)))
		o.add(col(10), gosnmp.Counter32, Counter(inOctets()))
		o.add(col(11), gosnmp.Counter32, Counter(f.inRate/900, start(n+2, 1e6, 1e7), swing(n, 0.6, 5*time.Minute)))
		o.add(col(13), gosnmp.Counter32, Const(uint32(0)))
		o.add(col(14), gosnmp.Counter32, Counter(f.errorRate, start(n+3, 0, 40), swing(n+3, 0.9, 11*time.Minute)))
		o.add(col(16), gosnmp.Counter32, Counter(outOctets()))
		o.add(col(17), gosnmp.Counter32, Counter(f.outRate/700, start(n+4, 1e6, 1e7), swing(n+1, 0.5, 7*time.Minute)))
		o.add(col(19), gosnmp.Counter32, Const(uint32(0)))
		o.add(col(20), gosnmp.Counter32, Const(uint32(0)))

		o.add(ext(1), gosnmp.OctetString, Const(f.name))
		o.add(ext(6), gosnmp.Counter64, Counter64(inOctets()))
		o.add(ext(10), gosnmp.Counter64, Counter64(outOctets()))
		o.add(ext(15), gosnmp.Gauge32, Const(f.speed/1_000_000))
		o.add(ext(18), gosnmp.OctetString, Const(f.alias))
	}

	// host resources (RFC 2790)
	o.add("1.3.6.1.2.1.25.1.1.0", gosnmp.TimeTicks, Uptime())
	o.add("1.3.6.1.2.1.25.1.5.0", gosnmp.Gauge32, Gauge(1, 3, swing(200, 0, 20*time.Minute)))
	o.add("1.3.6.1.2.1.25.1.6.0", gosnmp.Gauge32, Gauge(180, 260, swing(201, 0, 7*time.Minute)))
	o.add("1.3.6.1.2.1.25.2.2.0", gosnmp.Integer, Const(8388608)) // KiB: 8 GiB

	storage := []struct {
		index       int
		kind, descr string
		unit, size  int
		lo, hi      float64 // share of size in use
		period      time.Duration
	}{
		{1, ".1.3.6.1.2.1.25.2.1.2", "Physical memory", 1024, 8388608, 0.55, 0.75, 4 * time.Minute},
		{3, ".1.3.6.1.2.1.25.2.1.3", "Virtual memory", 1024, 10485760, 0.48, 0.62, 5 * time.Minute},
		{31, ".1.3.6.1.2.1.25.2.1.4", "/", 4096, 25600000, 0.41, 0.43, 6 * time.Hour},
		{36, ".1.3.6.1.2.1.25.2.1.4", "/home", 4096, 51200000, 0.63, 0.64, 9 * time.Hour},
	}
	for i, st := range storage {
		col := func(n int) string { return fmt.Sprintf("1.3.6.1.2.1.25.2.3.1.%d.%d", n, st.index) }
		o.add(col(1), gosnmp.Integer, Const(st.index))
		o.add(col(2), gosnmp.ObjectIdentifier, Const(st.kind))
		o.add(col(3), gosnmp.OctetString, Const(st.descr))
		o.add(col(4), gosnmp.Integer, Const(st.unit))
		o.add(col(5), gosnmp.Integer, Const(st.size))
		o.add(col(6), gosnmp.Integer, IntegerGauge(st.lo*float64(st.size), st.hi*float64(st.size),
			swing(300+uint64(i), 0, st.period)))
	}

	// One row per CPU, indexed as net-snmp indexes hrDeviceTable.
	for i, index := range []int{196608, 196609} {
		o.add(fmt.Sprintf("1.3.6.1.2.1.25.3.3.1.1.%d", index), gosnmp.ObjectIdentifier, Const(".0.0"))
		o.add(fmt.Sprintf("1.3.6.1.2.1.25.3.3.1.2.%d", index), gosnmp.Integer,
			IntegerGauge(4, 55, swing(400+uint64(i), 0, 90*time.Second)))
	}
	return o
}

// deviceMAC is a MAC address for a device's n-th port, drawn from its seed:
// locally administered and unicast (02:…), so it can never be a vendor's.
func deviceMAC(seed uint64, n uint64) net.HardwareAddr {
	h := mix(seed ^ (n * 0x100000001B3))
	return net.HardwareAddr{0x02, byte(h >> 32), byte(h >> 24), byte(h >> 16), byte(h >> 8), byte(h)}
}
