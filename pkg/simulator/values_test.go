package simulator

import (
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func at(d time.Duration) clock {
	t0 := time.Unix(1_700_000_000, 0)
	return clock{started: t0, now: t0.Add(d)}
}

// A counter only ever goes up, however its rate swings.
func TestACounterOnlyGoesUp(t *testing.T) {
	c := Counter64(1_250_000, 3e9, Swing{Depth: 0.95, Period: 5 * time.Minute, Seed: 7})
	prev := c.read(at(0)).(uint64)
	for s := 1; s <= 3600; s++ {
		v := c.read(at(time.Duration(s) * time.Second)).(uint64)
		if v < prev {
			t.Fatalf("at %ds the count fell from %d to %d", s, prev, v)
		}
		prev = v
	}
}

// ifInOctets is ifHCInOctets modulo 2^32, and it wraps as a real one does.
func TestThe32BitCounterIsThe64BitOneWrapped(t *testing.T) {
	s := Swing{Depth: 0.6, Period: 5 * time.Minute, Seed: 7}
	c32, c64 := Counter(1_250_000, 4_000_000_000, s), Counter64(1_250_000, 4_000_000_000, s)
	wrapped := false
	var prev uint32
	for sec := 0; sec <= 900; sec += 5 {
		k := at(time.Duration(sec) * time.Second)
		hi, lo := c64.read(k).(uint64), c32.read(k).(uint32)
		if uint32(hi) != lo {
			t.Fatalf("at %ds: %d at 32 bits, %d at 64", sec, lo, hi)
		}
		if sec > 0 && lo < prev {
			wrapped = true
		}
		prev = lo
	}
	if !wrapped {
		t.Error("starting 295 MB short of 2^32 at 1.25 MB/s, the 32-bit counter did not wrap in 15 minutes")
	}
}

func TestAGaugeStaysInItsRangeAndMoves(t *testing.T) {
	g := IntegerGauge(4, 55, Swing{Period: 90 * time.Second, Seed: 3})
	seen := map[int]bool{}
	for s := 0; s < 600; s++ {
		v := g.read(at(time.Duration(s) * time.Second)).(int)
		if v < 4 || v > 55 {
			t.Fatalf("at %ds a gauge from 4 to 55 read %d", s, v)
		}
		seen[v] = true
	}
	if len(seen) < 20 {
		t.Errorf("a CPU load took %d distinct values in ten minutes; it hardly moves", len(seen))
	}
}

// A reading is a function of the instant and of its seed, and of nothing else:
// asked twice it answers the same, and two seeds do not move in step — two
// simulated servers side by side must not draw the same curve.
func TestAReadingIsAFunctionOfTimeAndSeed(t *testing.T) {
	a := Gauge(0, 1000, Swing{Period: time.Minute, Seed: 1})
	b := Gauge(0, 1000, Swing{Period: time.Minute, Seed: 2})
	if k := at(37 * time.Second); a.read(k) != a.read(k) {
		t.Error("one reading, asked twice at one instant, answered two things")
	}
	same := 0
	for s := 0; s < 60; s++ {
		if k := at(time.Duration(s) * time.Second); a.read(k) == b.read(k) {
			same++
		}
	}
	if same > 10 {
		t.Errorf("two seeds agreed %d times in a minute", same)
	}
}

func TestAMovingValueRefusesTheWrongType(t *testing.T) {
	s := Swing{Period: time.Minute}
	for name, err := range map[string]error{
		"a Counter32 as a Counter64":  Counter(1, 0, s).check(gosnmp.Counter64),
		"a Counter64 as a Counter32":  Counter64(1, 0, s).check(gosnmp.Counter32),
		"a negative rate":             Counter(-1, 0, s).check(gosnmp.Counter32),
		"a Gauge32 as an Integer":     Gauge(0, 1, s).check(gosnmp.Integer),
		"a range upside down":         Gauge(10, 1, s).check(gosnmp.Gauge32),
		"a Gauge32 below zero":        Gauge(-1, 1, s).check(gosnmp.Gauge32),
		"an Integer gauge as Gauge32": IntegerGauge(0, 1, s).check(gosnmp.Gauge32),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
