package simulator

import (
	"fmt"
	"math"
	"net"
	"time"

	"github.com/gosnmp/gosnmp"
)

// The IANAifType values the models use.
const (
	ifTypeOther            = 1
	ifTypeEthernet         = 6
	ifTypeSoftwareLoopback = 24
	ifTypePropVirtual      = 53
)

// iface is one interface of a model: what IF-MIB says of it, and how busy it is.
type iface struct {
	index int
	// descr is ifDescr and name ifName: the long and the short form a Cisco
	// gives, "FastEthernet0/1" and "Fa0/1". A name not given is the descr.
	descr, name, alias string
	ifType, mtu        int
	speed              uint64 // bits per second
	mac                net.HardwareAddr
	adminDown, up      bool
	inRate, outRate    float64 // octets per second, while up
	errorRate          float64 // input errors per second, while up
}

// addInterfaces adds IF-MIB's ifNumber, ifTable and ifXTable. An interface that
// is down keeps the counts it made while it was up, and adds nothing to them.
func addInterfaces(o *objects, seed uint64, ifs []iface) {
	swing := func(n uint64, depth float64, period time.Duration) Swing {
		return Swing{Depth: depth, Period: period, Seed: seed + n}
	}
	// A starting count drawn from the seed, so two devices' counters differ
	// and a busy 32-bit one wraps within minutes of the device starting.
	start := func(n uint64, base, spread float64) uint64 {
		return uint64(base + spread*unit(seed+1000+n))
	}
	o.add("1.3.6.1.2.1.2.1.0", gosnmp.Integer, Const(len(ifs)))
	for _, f := range ifs {
		col := func(n int) string { return fmt.Sprintf("1.3.6.1.2.1.2.2.1.%d.%d", n, f.index) }
		ext := func(n int) string { return fmt.Sprintf("1.3.6.1.2.1.31.1.1.1.%d.%d", n, f.index) }
		n := uint64(f.index) * 16
		admin, oper := 1, 2
		if f.adminDown {
			admin = 2
		}
		in, out, errs := 0.0, 0.0, 0.0
		if f.up && !f.adminDown {
			oper, in, out, errs = 1, f.inRate, f.outRate, f.errorRate
		}
		name := f.name
		if name == "" {
			name = f.descr
		}
		inOctets := func() (float64, uint64, Swing) {
			return in, start(n, 3.0e9, 1.0e9), swing(n, 0.6, 5*time.Minute)
		}
		outOctets := func() (float64, uint64, Swing) {
			return out, start(n+1, 1.0e9, 2.0e9), swing(n+1, 0.5, 7*time.Minute)
		}

		o.add(col(1), gosnmp.Integer, Const(f.index))
		o.add(col(2), gosnmp.OctetString, Const(f.descr))
		o.add(col(3), gosnmp.Integer, Const(f.ifType))
		o.add(col(4), gosnmp.Integer, Const(f.mtu))
		// ifSpeed stops at 2^32 - 1, which is why ifHighSpeed exists.
		o.add(col(5), gosnmp.Gauge32, Const(uint32(min(f.speed, math.MaxUint32))))
		o.add(col(6), gosnmp.OctetString, Const([]byte(f.mac)))
		o.add(col(7), gosnmp.Integer, Const(admin))
		o.add(col(8), gosnmp.Integer, Const(oper))
		o.add(col(9), gosnmp.TimeTicks, Const(uint32(0)))
		o.add(col(10), gosnmp.Counter32, Counter(inOctets()))
		o.add(col(11), gosnmp.Counter32, Counter(in/900, start(n+2, 1e6, 1e7), swing(n, 0.6, 5*time.Minute)))
		o.add(col(13), gosnmp.Counter32, Const(uint32(0)))
		o.add(col(14), gosnmp.Counter32, Counter(errs, start(n+3, 0, 40), swing(n+3, 0.9, 11*time.Minute)))
		o.add(col(16), gosnmp.Counter32, Counter(outOctets()))
		o.add(col(17), gosnmp.Counter32, Counter(out/700, start(n+4, 1e6, 1e7), swing(n+1, 0.5, 7*time.Minute)))
		o.add(col(19), gosnmp.Counter32, Const(uint32(0)))
		o.add(col(20), gosnmp.Counter32, Const(uint32(0)))

		o.add(ext(1), gosnmp.OctetString, Const(name))
		o.add(ext(6), gosnmp.Counter64, Counter64(inOctets()))
		o.add(ext(10), gosnmp.Counter64, Counter64(outOctets()))
		o.add(ext(15), gosnmp.Gauge32, Const(uint32(min(f.speed/1_000_000, math.MaxUint32))))
		o.add(ext(18), gosnmp.OctetString, Const(f.alias))
	}
}

// deviceMAC is a MAC address for a device's n-th port, drawn from its seed:
// locally administered and unicast (02:…), so it can never be a vendor's.
func deviceMAC(seed uint64, n uint64) net.HardwareAddr {
	h := mix(seed ^ (n * 0x100000001B3))
	return net.HardwareAddr{0x02, byte(h >> 32), byte(h >> 24), byte(h >> 16), byte(h >> 8), byte(h)}
}
