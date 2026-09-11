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
	ifTypePPP              = 23
	ifTypeSoftwareLoopback = 24
	ifTypePropVirtual      = 53
	ifTypeIEEE80211        = 71
	ifTypeTunnel           = 131
	ifTypeBridge           = 209
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

// ifName is the interface's short name: its description when it has none of
// its own.
func (f iface) ifName() string {
	if f.name != "" {
		return f.name
	}
	return f.descr
}

// addInterfaces adds IF-MIB whole: ifNumber, every column of ifTable and of
// ifXTable, ifStackTable and the last-change scalars. An interface that is down
// keeps the counts it made while it was up, and adds nothing to them.
//
// A 32-bit counter and its 64-bit twin are built from the same arguments, so
// they agree — ifInUcastPkts is ifHCInUcastPkts modulo 2^32 — and the packets
// follow the octets: about 900 octets a packet in, 700 out, a few in a hundred
// multicast or broadcast.
func addInterfaces(o *objects, seed uint64, ifs []iface) {
	swing := func(n uint64, depth float64, period time.Duration) Swing {
		return Swing{Depth: depth, Period: period, Seed: seed + n}
	}
	// A starting count drawn from the seed, so two devices' counters differ
	// and a busy 32-bit one wraps within minutes of the device starting.
	start := func(n uint64, base, spread float64) uint64 {
		return uint64(base + spread*unit(seed+1000+n))
	}
	zero := Const(uint32(0))
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
		inPkts, outPkts := in/900, out/700
		type counts func() (float64, uint64, Swing)
		inOctets := counts(func() (float64, uint64, Swing) {
			return in, start(n, 3.0e9, 1.0e9), swing(n, 0.6, 5*time.Minute)
		})
		outOctets := counts(func() (float64, uint64, Swing) {
			return out, start(n+1, 1.0e9, 2.0e9), swing(n+1, 0.5, 7*time.Minute)
		})
		packets := func(rate float64, k uint64, base, spread float64, towards uint64, depth float64, period time.Duration) counts {
			return func() (float64, uint64, Swing) {
				return rate, start(n+k, base, spread), swing(n+towards, depth, period)
			}
		}
		inUcast := packets(inPkts, 2, 1e6, 1e7, 0, 0.6, 5*time.Minute)
		outUcast := packets(outPkts, 4, 1e6, 1e7, 1, 0.5, 7*time.Minute)
		inMcast := packets(inPkts*0.02, 5, 1e4, 1e5, 0, 0.6, 5*time.Minute)
		inBcast := packets(inPkts*0.01, 6, 1e4, 1e5, 0, 0.6, 5*time.Minute)
		outMcast := packets(outPkts*0.005, 7, 1e4, 1e5, 1, 0.5, 7*time.Minute)
		outBcast := packets(outPkts*0.002, 8, 1e4, 1e5, 1, 0.5, 7*time.Minute)
		physical := f.ifType == ifTypeEthernet || f.ifType == ifTypeIEEE80211

		o.add(col(1), gosnmp.Integer, Const(f.index))
		o.add(col(2), gosnmp.OctetString, Const(f.descr))
		o.add(col(3), gosnmp.Integer, Const(f.ifType))
		o.add(col(4), gosnmp.Integer, Const(f.mtu))
		// ifSpeed stops at 2^32 - 1, which is why ifHighSpeed exists.
		o.add(col(5), gosnmp.Gauge32, Const(uint32(min(f.speed, math.MaxUint32))))
		o.add(col(6), gosnmp.OctetString, Const([]byte(f.mac)))
		o.add(col(7), gosnmp.Integer, Const(admin))
		o.add(col(8), gosnmp.Integer, Const(oper))
		o.add(col(9), gosnmp.TimeTicks, zero)
		o.add(col(10), gosnmp.Counter32, Counter(inOctets()))
		o.add(col(11), gosnmp.Counter32, Counter(inUcast()))
		o.add(col(12), gosnmp.Counter32, Counter(inPkts*0.03, start(n+9, 2e4, 2e5), swing(n, 0.6, 5*time.Minute)))
		o.add(col(13), gosnmp.Counter32, zero)
		o.add(col(14), gosnmp.Counter32, Counter(errs, start(n+3, 0, 40), swing(n+3, 0.9, 11*time.Minute)))
		o.add(col(15), gosnmp.Counter32, Counter(inPkts*5e-4, start(n+10, 0, 1000), swing(n+3, 0.9, 11*time.Minute)))
		o.add(col(16), gosnmp.Counter32, Counter(outOctets()))
		o.add(col(17), gosnmp.Counter32, Counter(outUcast()))
		o.add(col(18), gosnmp.Counter32, Counter(outPkts*0.007, start(n+11, 2e4, 2e5), swing(n+1, 0.5, 7*time.Minute)))
		o.add(col(19), gosnmp.Counter32, zero)
		o.add(col(20), gosnmp.Counter32, zero)
		o.add(col(21), gosnmp.Gauge32, zero)
		o.add(col(22), gosnmp.ObjectIdentifier, Const(".0.0"))

		connector := 2
		if physical {
			connector = 1
		}
		o.add(ext(1), gosnmp.OctetString, Const(f.ifName()))
		o.add(ext(2), gosnmp.Counter32, Counter(inMcast()))
		o.add(ext(3), gosnmp.Counter32, Counter(inBcast()))
		o.add(ext(4), gosnmp.Counter32, Counter(outMcast()))
		o.add(ext(5), gosnmp.Counter32, Counter(outBcast()))
		o.add(ext(6), gosnmp.Counter64, Counter64(inOctets()))
		o.add(ext(7), gosnmp.Counter64, Counter64(inUcast()))
		o.add(ext(8), gosnmp.Counter64, Counter64(inMcast()))
		o.add(ext(9), gosnmp.Counter64, Counter64(inBcast()))
		o.add(ext(10), gosnmp.Counter64, Counter64(outOctets()))
		o.add(ext(11), gosnmp.Counter64, Counter64(outUcast()))
		o.add(ext(12), gosnmp.Counter64, Counter64(outMcast()))
		o.add(ext(13), gosnmp.Counter64, Counter64(outBcast()))
		o.add(ext(14), gosnmp.Integer, Const(1)) // linkUp and linkDown enabled
		o.add(ext(15), gosnmp.Gauge32, Const(uint32(min(f.speed/1_000_000, math.MaxUint32))))
		o.add(ext(16), gosnmp.Integer, Const(2)) // not promiscuous
		o.add(ext(17), gosnmp.Integer, Const(connector))
		o.add(ext(18), gosnmp.OctetString, Const(f.alias))
		o.add(ext(19), gosnmp.TimeTicks, zero)

		// ifStackTable: every interface directly on the wire, with nothing
		// stacked on it.
		o.add(fmt.Sprintf("1.3.6.1.2.1.31.1.2.1.3.0.%d", f.index), gosnmp.Integer, Const(1))
		o.add(fmt.Sprintf("1.3.6.1.2.1.31.1.2.1.3.%d.0", f.index), gosnmp.Integer, Const(1))
	}
	o.add("1.3.6.1.2.1.31.1.5.0", gosnmp.TimeTicks, zero) // ifTableLastChange
	o.add("1.3.6.1.2.1.31.1.6.0", gosnmp.TimeTicks, zero) // ifStackLastChange
}

// deviceMAC is a MAC address for a device's n-th port, drawn from its seed:
// locally administered and unicast (02:…), so it can never be a vendor's.
func deviceMAC(seed uint64, n uint64) net.HardwareAddr {
	h := mix(seed ^ (n * 0x100000001B3))
	return net.HardwareAddr{0x02, byte(h >> 32), byte(h >> 24), byte(h >> 16), byte(h >> 8), byte(h)}
}
