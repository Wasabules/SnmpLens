package simulator

import (
	"fmt"
	"math"
	"time"

	"github.com/gosnmp/gosnmp"
)

// Swing is how a moving value moves: how far (Depth, a fraction of a counter's
// mean rate), how slowly (Period, that of its slowest component) and out of
// step with which others (Seed).
//
// The motion is three sines of unrelated periods, so it does not visibly repeat
// over a demonstration, and it is a pure function of time: a reading asked for
// twice at the same instant answers the same thing, and nothing ticks while
// nobody asks.
type Swing struct {
	Depth  float64
	Period time.Duration
	Seed   uint64
}

// components are the three sines: each one's share of the motion, and how much
// faster than the slowest it turns. 1, 2.7 and 7.3 share no small common
// multiple, which is what keeps their sum from repeating.
var components = [...]struct{ weight, speed float64 }{
	{0.6, 1}, {0.3, 2.7}, {0.1, 7.3},
}

// mix is splitmix64's finaliser: consecutive seeds come out unrelated.
func mix(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// unit is a number in [0, 1) drawn from x.
func unit(x uint64) float64 { return float64(mix(x)>>11) / (1 << 53) }

func (s Swing) phase(k int) float64 { return 2 * math.Pi * unit(s.Seed*4+uint64(k)) }

func (s Swing) omega(k int) float64 {
	period := s.Period.Seconds()
	if period <= 0 {
		period = 300
	}
	return 2 * math.Pi * components[k].speed / period
}

// wave is the motion t seconds in, between -1 and 1.
func (s Swing) wave(t float64) float64 {
	var v float64
	for k, c := range components {
		v += c.weight * math.Sin(s.omega(k)*t+s.phase(k))
	}
	return v
}

// area is the wave's integral from 0 to t, which is what a counter adds up.
func (s Swing) area(t float64) float64 {
	var v float64
	for k, c := range components {
		w := s.omega(k)
		v += c.weight * (math.Cos(s.phase(k)) - math.Cos(w*t+s.phase(k))) / w
	}
	return v
}

func elapsed(c clock) float64 { return c.now.Sub(c.started).Seconds() }

// counter is a count that grows by perSecond on average and swings around that
// rate without ever falling below (1 - Depth) of it, so it only ever goes up, as
// a counter must.
type counter struct {
	perSecond float64
	start     uint64
	swing     Swing
	wide      bool
	// integer is an INTEGER that counts all the same: hrSWRunPerfCPU.
	integer bool
}

// Counter answers a Counter32 that grows by perSecond on average from start.
// It wraps at 2^32 as a real interface's does: SnmpLens corrects the wrap
// (pkg/monitor/counters.go), and a device that never wrapped would never
// exercise that.
func Counter(perSecond float64, start uint64, s Swing) Reading {
	return counter{perSecond: perSecond, start: start, swing: s}
}

// Counter64 is Counter at 64 bits. Built from the same arguments the two agree,
// the way ifInOctets is ifHCInOctets modulo 2^32.
func Counter64(perSecond float64, start uint64, s Swing) Reading {
	return counter{perSecond: perSecond, start: start, swing: s, wide: true}
}

// IntegerCounter is Counter for an INTEGER that counts — hrSWRunPerfCPU, the
// centi-seconds of processor a process has had — kept below 2^31 as an Integer32
// must be.
func IntegerCounter(perSecond float64, start uint64, s Swing) Reading {
	return counter{perSecond: perSecond, start: start, swing: s, integer: true}
}

func (c counter) count(t float64) uint64 {
	depth := math.Min(math.Max(c.swing.Depth, 0), 0.95)
	return c.start + uint64(c.perSecond*(t+depth*c.swing.area(t)))
}

func (c counter) read(k clock) any {
	n := c.count(elapsed(k))
	switch {
	case c.wide:
		return n
	case c.integer:
		return int(n % (1 << 31))
	}
	return uint32(n) // the wrap
}

func (c counter) check(t gosnmp.Asn1BER) error {
	want := gosnmp.Counter32
	switch {
	case c.wide:
		want = gosnmp.Counter64
	case c.integer:
		want = gosnmp.Integer
	}
	if t != want {
		return fmt.Errorf("this counter is a %v, not a %v", want, t)
	}
	if c.perSecond < 0 || math.IsNaN(c.perSecond) || math.IsInf(c.perSecond, 0) {
		return fmt.Errorf("a counter grows by a finite, positive rate, not %v", c.perSecond)
	}
	return nil
}

// gauge is a level swinging between lo and hi: a load, a memory use, a count
// of processes. A gauge spans its whole range, so Swing.Depth plays no part.
type gauge struct {
	lo, hi  float64
	swing   Swing
	integer bool
	// wide is a level carried in 64 bits: a CounterBasedGauge64, which is a
	// Counter64 on the wire.
	wide bool
}

// Gauge answers a Gauge32 swinging between lo and hi.
func Gauge(lo, hi float64, s Swing) Reading { return gauge{lo: lo, hi: hi, swing: s} }

// IntegerGauge is Gauge for the many MIB objects that are an INTEGER even
// though they measure a level — hrProcessorLoad, hrStorageUsed.
func IntegerGauge(lo, hi float64, s Swing) Reading {
	return gauge{lo: lo, hi: hi, swing: s, integer: true}
}

// Gauge64 is Gauge for a level too large for 32 bits — UCD-SNMP-MIB's memory in
// kilobytes, as a CounterBasedGauge64.
func Gauge64(lo, hi float64, s Swing) Reading { return gauge{lo: lo, hi: hi, swing: s, wide: true} }

func (g gauge) value(k clock) float64 {
	return math.Round(g.lo + (g.hi-g.lo)*(0.5+0.5*g.swing.wave(elapsed(k))))
}

func (g gauge) read(k clock) any {
	v := g.value(k)
	switch {
	case g.integer:
		return int(v)
	case g.wide:
		return uint64(v)
	}
	return uint32(v)
}

func (g gauge) check(t gosnmp.Asn1BER) error {
	switch {
	case g.integer && t != gosnmp.Integer:
		return fmt.Errorf("this gauge is an Integer, not a %v", t)
	case g.wide && t != gosnmp.Counter64:
		return fmt.Errorf("this gauge is a Counter64, not a %v", t)
	case !g.integer && !g.wide && t != gosnmp.Gauge32:
		return fmt.Errorf("this gauge is a Gauge32, not a %v", t)
	case math.IsNaN(g.lo) || math.IsNaN(g.hi) || g.hi < g.lo:
		return fmt.Errorf("a gauge swings from a low to a high, not from %v to %v", g.lo, g.hi)
	case g.integer && (g.lo < math.MinInt32 || g.hi > math.MaxInt32):
		return fmt.Errorf("%v to %v does not fit an Integer32", g.lo, g.hi)
	case g.wide && (g.lo < 0 || g.hi > 1<<62):
		return fmt.Errorf("%v to %v does not fit a Counter64", g.lo, g.hi)
	case !g.integer && !g.wide && (g.lo < 0 || g.hi > math.MaxUint32):
		return fmt.Errorf("%v to %v does not fit a Gauge32", g.lo, g.hi)
	}
	return nil
}

// LoadText is a level written as a decimal string, as UCD-SNMP-MIB's laLoad
// writes a load average: hundredths from lo to hi, read as "0.42". Built from
// the arguments of an IntegerGauge in hundredths, the two agree.
func LoadText(lo, hi float64, s Swing) Reading {
	return loadText{gauge{lo: lo, hi: hi, swing: s, integer: true}}
}

type loadText struct{ g gauge }

func (l loadText) read(k clock) any { return fmt.Sprintf("%.2f", l.g.value(k)/100) }

func (l loadText) check(t gosnmp.Asn1BER) error {
	if t != gosnmp.OctetString {
		return fmt.Errorf("a load written out is an OCTET STRING, not a %v", t)
	}
	return l.g.check(gosnmp.Integer)
}

// DateAndTime answers the time now as SNMPv2-TC's DateAndTime, in the machine's
// own zone: hrSystemDate.
func DateAndTime() Reading { return dateAndTime{} }

type dateAndTime struct{}

func (dateAndTime) read(c clock) any { return dateAndTimeOf(c.now) }

func (dateAndTime) check(t gosnmp.Asn1BER) error {
	if t != gosnmp.OctetString {
		return fmt.Errorf("a DateAndTime is an OCTET STRING, not a %v", t)
	}
	return nil
}

// dateAndTimeOf is t in the eleven octets of SNMPv2-TC's DateAndTime: the year
// in two, month, day, hour, minutes, seconds, tenths, and the offset from UTC.
func dateAndTimeOf(t time.Time) []byte {
	_, offset := t.Zone()
	sign := byte('+')
	if offset < 0 {
		sign, offset = '-', -offset
	}
	year := t.Year()
	return []byte{byte(year >> 8), byte(year), byte(t.Month()), byte(t.Day()), byte(t.Hour()), byte(t.Minute()),
		byte(t.Second()), byte(t.Nanosecond() / 100_000_000), sign, byte(offset / 3600), byte(offset % 3600 / 60)}
}

// derived is a figure computed from others at the same instant, so that what a
// MIB states twice agrees when it is read: the memory used and the memory
// available, a percentage and its parts, the two halves of a 64-bit count.
// Unexported like the rest: a device file cannot describe one.
type derived struct {
	typ gosnmp.Asn1BER
	f   func(c clock) any
}

func (d derived) read(c clock) any { return d.f(c) }

func (d derived) check(t gosnmp.Asn1BER) error {
	if t != d.typ {
		return fmt.Errorf("this figure is a %v, not a %v", d.typ, t)
	}
	return nil
}

// number is what r reads, as a number whatever Go type it is read in.
func number(r Reading, c clock) float64 {
	switch v := r.read(c).(type) {
	case int:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	}
	return 0
}

// remainder is total less what the parts read: the processor time left idle,
// the space left free.
func remainder(t gosnmp.Asn1BER, total float64, parts ...Reading) Reading {
	return derived{typ: t, f: func(c clock) any {
		v := total
		for _, p := range parts {
			v -= number(p, c)
		}
		return as(t, max(v, 0))
	}}
}

// percentOf is what part reads as a whole percentage of total.
func percentOf(t gosnmp.Asn1BER, part Reading, total float64) Reading {
	return derived{typ: t, f: func(c clock) any {
		if total <= 0 {
			return as(t, 0)
		}
		return as(t, math.Round(100*number(part, c)/total))
	}}
}

// word is one 32-bit half of what r reads: UCD-SNMP-MIB gives a disk's size in
// kilobytes as a low and a high Unsigned32.
func word(r Reading, high bool) Reading {
	return derived{typ: gosnmp.Gauge32, f: func(c clock) any {
		v := uint64(number(r, c))
		if high {
			return uint32(v >> 32)
		}
		return uint32(v)
	}}
}

// as is v in the Go type Const holds for t.
func as(t gosnmp.Asn1BER, v float64) any {
	switch t {
	case gosnmp.Integer:
		return int(min(v, math.MaxInt32))
	case gosnmp.Counter64:
		return uint64(v)
	}
	return uint32(min(v, math.MaxUint32))
}

// SecondsUp answers the seconds since the agent started as a Gauge32: how long
// something the device brought up at boot, a BGP session, has been up.
func SecondsUp() Reading { return secondsUp{} }

type secondsUp struct{}

func (secondsUp) read(c clock) any { return uint32(c.now.Sub(c.started) / time.Second) }

func (secondsUp) check(t gosnmp.Asn1BER) error {
	if t != gosnmp.Gauge32 {
		return fmt.Errorf("seconds up are a Gauge32, not a %v", t)
	}
	return nil
}

// engineSeconds is snmpEngineTime: the seconds since the agent started.
type engineSeconds struct{}

func (engineSeconds) read(c clock) any { return int(c.now.Sub(c.started) / time.Second) }

func (engineSeconds) check(t gosnmp.Asn1BER) error {
	if t != gosnmp.Integer {
		return fmt.Errorf("snmpEngineTime is an Integer, not a %v", t)
	}
	return nil
}
