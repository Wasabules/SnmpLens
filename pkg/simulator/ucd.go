package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// UCD-SNMP-MIB is net-snmp's own: memory, disks, load averages and processor
// time as Linux counts them, which is what most tools read from a Linux host
// before HOST-RESOURCES-MIB. Every figure it gives twice agrees: the memory
// available is the total less what is in use, a disk's percentage is its used
// space over its size, and the idle processor time is what the rest leaves.
type ucdInfo struct {
	memKB, swapKB int
	// memUsed is the share of memory in use, which swings between the two.
	memUsed [2]float64
	cpus    int
	// load is the one-minute load average's range; the five- and fifteen-
	// minute ones move within it, more slowly.
	load [2]float64
	// user and system are the processor time in percent.
	user, system [2]float64
	disks        []ucdDisk
}

type ucdDisk struct {
	path, device string
	totalKB      float64
	used         [2]float64 // share of the disk in use
}

func addUCD(o *objects, seed uint64, u ucdInfo) {
	const ucd = "1.3.6.1.4.1.2021."
	swing := func(n uint64, period time.Duration) Swing { return Swing{Period: period, Seed: seed + 90000 + n} }
	widen := func(r Reading) Reading {
		return derived{typ: gosnmp.Counter64, f: func(c clock) any { return uint64(number(r, c)) }}
	}

	// memory, in kilobytes
	total, swap := float64(u.memKB), float64(u.swapKB)
	used := IntegerGauge(u.memUsed[0]*total, u.memUsed[1]*total, swing(0, 4*time.Minute))
	swapUsed := IntegerGauge(0, swap*0.02, swing(1, time.Hour))
	availReal := remainder(gosnmp.Integer, total, used)
	availSwap := remainder(gosnmp.Integer, swap, swapUsed)
	free := remainder(gosnmp.Integer, total+swap, used, swapUsed)
	mem := func(n int) string { return fmt.Sprintf(ucd+"4.%d.0", n) }
	o.add(mem(1), gosnmp.Integer, Const(0))
	o.add(mem(2), gosnmp.OctetString, Const("swap"))
	o.add(mem(3), gosnmp.Integer, Const(u.swapKB))
	o.add(mem(4), gosnmp.Integer, availSwap)
	o.add(mem(5), gosnmp.Integer, Const(u.memKB))
	o.add(mem(6), gosnmp.Integer, availReal)
	o.add(mem(11), gosnmp.Integer, free)
	o.add(mem(12), gosnmp.Integer, Const(16000))
	o.add(mem(13), gosnmp.Integer, IntegerGauge(total*0.01, total*0.015, swing(2, 30*time.Minute)))
	o.add(mem(14), gosnmp.Integer, IntegerGauge(total*0.02, total*0.03, swing(3, 30*time.Minute)))
	o.add(mem(15), gosnmp.Integer, IntegerGauge(total*0.18, total*0.24, swing(4, 25*time.Minute)))
	o.add(mem(18), gosnmp.Counter64, Const(uint64(u.swapKB)))
	o.add(mem(19), gosnmp.Counter64, widen(availSwap))
	o.add(mem(20), gosnmp.Counter64, Const(uint64(u.memKB)))
	o.add(mem(21), gosnmp.Counter64, widen(availReal))
	o.add(mem(22), gosnmp.Counter64, widen(free))
	o.add(mem(100), gosnmp.Integer, Const(0)) // no error
	o.add(mem(101), gosnmp.OctetString, Const(""))

	// dskTable: each file system, its size in kilobytes also as two 32-bit
	// halves, since an Integer32 stops at 2 TB.
	for i, d := range u.disks {
		n := i + 1
		col := func(c int) string { return fmt.Sprintf(ucd+"9.1.%d.%d", c, n) }
		usedKB := Gauge64(d.used[0]*d.totalKB, d.used[1]*d.totalKB, swing(10+uint64(n), 12*time.Hour))
		availKB := remainder(gosnmp.Counter64, d.totalKB, usedKB)
		totalKB := Const(uint64(d.totalKB))
		capped := func(r Reading) Reading {
			return derived{typ: gosnmp.Integer, f: func(c clock) any { return as(gosnmp.Integer, number(r, c)) }}
		}
		o.add(col(1), gosnmp.Integer, Const(n))
		o.add(col(2), gosnmp.OctetString, Const(d.path))
		o.add(col(3), gosnmp.OctetString, Const(d.device))
		o.add(col(4), gosnmp.Integer, Const(-1)) // a minimum in percent, not in kilobytes
		o.add(col(5), gosnmp.Integer, Const(10))
		o.add(col(6), gosnmp.Integer, capped(totalKB))
		o.add(col(7), gosnmp.Integer, capped(availKB))
		o.add(col(8), gosnmp.Integer, capped(usedKB))
		o.add(col(9), gosnmp.Integer, percentOf(gosnmp.Integer, usedKB, d.totalKB))
		o.add(col(10), gosnmp.Integer, IntegerGauge(2, 6, swing(20+uint64(n), 6*time.Hour)))
		o.add(col(11), gosnmp.Gauge32, word(totalKB, false))
		o.add(col(12), gosnmp.Gauge32, word(totalKB, true))
		o.add(col(13), gosnmp.Gauge32, word(availKB, false))
		o.add(col(14), gosnmp.Gauge32, word(availKB, true))
		o.add(col(15), gosnmp.Gauge32, word(usedKB, false))
		o.add(col(16), gosnmp.Gauge32, word(usedKB, true))
		o.add(col(100), gosnmp.Integer, Const(0))
		o.add(col(101), gosnmp.OctetString, Const(""))
	}

	// laTable: the load averages, written out and in hundredths, from one swing
	// each so that the two agree.
	for i, l := range []struct {
		name, config string
		narrow       float64
		period       time.Duration
	}{
		{"Load-1", "12.00", 1, 3 * time.Minute},
		{"Load-5", "14.00", 0.7, 12 * time.Minute},
		{"Load-15", "14.00", 0.5, 35 * time.Minute},
	} {
		n := i + 1
		mid := (u.load[0] + u.load[1]) / 2
		half := (u.load[1] - u.load[0]) / 2 * l.narrow
		lo, hi := (mid-half)*100, (mid+half)*100
		s := swing(30+uint64(n), l.period)
		col := func(c int) string { return fmt.Sprintf(ucd+"10.1.%d.%d", c, n) }
		o.add(col(1), gosnmp.Integer, Const(n))
		o.add(col(2), gosnmp.OctetString, Const(l.name))
		o.add(col(3), gosnmp.OctetString, LoadText(lo, hi, s))
		o.add(col(4), gosnmp.OctetString, Const(l.config))
		o.add(col(5), gosnmp.Integer, IntegerGauge(lo, hi, s))
		o.add(col(100), gosnmp.Integer, Const(0))
		o.add(col(101), gosnmp.OctetString, Const(""))
	}

	// systemStats: the processor in percent, and its raw time in ticks of a
	// hundredth of a second per processor.
	user := IntegerGauge(u.user[0], u.user[1], swing(40, 90*time.Second))
	system := IntegerGauge(u.system[0], u.system[1], swing(41, 2*time.Minute))
	ss := func(n int) string { return fmt.Sprintf(ucd+"11.%d.0", n) }
	o.add(ss(1), gosnmp.Integer, Const(1))
	o.add(ss(2), gosnmp.OctetString, Const("systemStats"))
	o.add(ss(3), gosnmp.Integer, Const(0))
	o.add(ss(4), gosnmp.Integer, Const(0))
	o.add(ss(5), gosnmp.Integer, IntegerGauge(20, 400, swing(42, 4*time.Minute)))
	o.add(ss(6), gosnmp.Integer, IntegerGauge(5, 90, swing(43, 5*time.Minute)))
	o.add(ss(7), gosnmp.Integer, IntegerGauge(400, 1400, swing(44, 3*time.Minute)))
	o.add(ss(8), gosnmp.Integer, IntegerGauge(900, 4000, swing(45, 3*time.Minute)))
	o.add(ss(9), gosnmp.Integer, user)
	o.add(ss(10), gosnmp.Integer, system)
	o.add(ss(11), gosnmp.Integer, remainder(gosnmp.Integer, 100, user, system))
	ticks := float64(100 * u.cpus)
	avgUser := (u.user[0] + u.user[1]) / 200
	avgSystem := (u.system[0] + u.system[1]) / 200
	raw := func(n uint64, share float64) Reading {
		return Counter(ticks*share, uint64(1e6+4e7*unit(seed+91000+n)), Swing{Depth: 0.5, Period: 5 * time.Minute, Seed: seed + 91000 + n})
	}
	o.add(ss(50), gosnmp.Counter32, raw(50, avgUser))
	o.add(ss(51), gosnmp.Counter32, raw(51, avgUser*0.02))
	o.add(ss(52), gosnmp.Counter32, raw(52, avgSystem))
	o.add(ss(53), gosnmp.Counter32, raw(53, 1-avgUser-avgSystem))
	o.add(ss(54), gosnmp.Counter32, raw(54, 0.004))
	o.add(ss(55), gosnmp.Counter32, raw(52, avgSystem*0.9))
	o.add(ss(56), gosnmp.Counter32, raw(56, 0.001))
	o.add(ss(57), gosnmp.Counter32, raw(57, 1.6))
	o.add(ss(58), gosnmp.Counter32, raw(58, 0.4))
	o.add(ss(59), gosnmp.Counter32, raw(59, 9))
	o.add(ss(60), gosnmp.Counter32, raw(60, 24))
	o.add(ss(61), gosnmp.Counter32, raw(61, 0.003))
	for n := 62; n <= 66; n++ {
		o.add(ss(n), gosnmp.Counter32, Const(uint32(0)))
	}
	o.add(ss(67), gosnmp.Integer, Const(u.cpus))
}
