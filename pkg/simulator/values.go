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

func (c counter) count(t float64) uint64 {
	depth := math.Min(math.Max(c.swing.Depth, 0), 0.95)
	return c.start + uint64(c.perSecond*(t+depth*c.swing.area(t)))
}

func (c counter) read(k clock) any {
	n := c.count(elapsed(k))
	if c.wide {
		return n
	}
	return uint32(n) // the wrap
}

func (c counter) check(t gosnmp.Asn1BER) error {
	want := gosnmp.Counter32
	if c.wide {
		want = gosnmp.Counter64
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
}

// Gauge answers a Gauge32 swinging between lo and hi.
func Gauge(lo, hi float64, s Swing) Reading { return gauge{lo: lo, hi: hi, swing: s} }

// IntegerGauge is Gauge for the many MIB objects that are an INTEGER even
// though they measure a level — hrProcessorLoad, hrStorageUsed.
func IntegerGauge(lo, hi float64, s Swing) Reading {
	return gauge{lo: lo, hi: hi, swing: s, integer: true}
}

func (g gauge) read(k clock) any {
	v := math.Round(g.lo + (g.hi-g.lo)*(0.5+0.5*g.swing.wave(elapsed(k))))
	if g.integer {
		return int(v)
	}
	return uint32(v)
}

func (g gauge) check(t gosnmp.Asn1BER) error {
	switch {
	case g.integer && t != gosnmp.Integer:
		return fmt.Errorf("this gauge is an Integer, not a %v", t)
	case !g.integer && t != gosnmp.Gauge32:
		return fmt.Errorf("this gauge is a Gauge32, not a %v", t)
	case math.IsNaN(g.lo) || math.IsNaN(g.hi) || g.hi < g.lo:
		return fmt.Errorf("a gauge swings from a low to a high, not from %v to %v", g.lo, g.hi)
	case g.integer && (g.lo < math.MinInt32 || g.hi > math.MaxInt32):
		return fmt.Errorf("%v to %v does not fit an Integer32", g.lo, g.hi)
	case !g.integer && (g.lo < 0 || g.hi > math.MaxUint32):
		return fmt.Errorf("%v to %v does not fit a Gauge32", g.lo, g.hi)
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
