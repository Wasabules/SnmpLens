package simulator

import (
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

// A fault is checked before it is applied: a share without its error, a speed
// the counters cannot run at or a latency past any timeout is refused.
func TestAFaultIsChecked(t *testing.T) {
	for _, ok := range []Faults{{}, {LatencyMs: 500, JitterMs: 100}, {LossPercent: 100}, {Mute: true},
		{Error: FaultGenErr, ErrorPercent: 30}, {Error: FaultTooBig, ErrorPercent: 100}, {CounterSpeed: 1000}} {
		if err := ok.Check(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	for _, bad := range []Faults{{LatencyMs: -1}, {LatencyMs: MaxLatencyMs + 1}, {JitterMs: MaxLatencyMs + 1},
		{LossPercent: 101}, {Error: "noSuchName", ErrorPercent: 10}, {Error: FaultGenErr}, {ErrorPercent: 10},
		{CounterSpeed: 7}} {
		if bad.Check() == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}

// Whether a request is lost, or answered with an error, is a share of the
// rolls; a mute device loses them all.
func TestAFaultIsAShareOfTheRequests(t *testing.T) {
	loss := Faults{LossPercent: 30}
	errs := Faults{Error: FaultGenErr, ErrorPercent: 30}
	for roll := 0; roll < 100; roll++ {
		if loss.drops(roll) != (roll < 30) || errs.errs(roll) != (roll < 30) {
			t.Fatalf("roll %d", roll)
		}
		if !(Faults{Mute: true}).drops(roll) || (Faults{}).drops(roll) || (Faults{}).errs(roll) {
			t.Fatalf("roll %d: a mute device, or one doing nothing wrong", roll)
		}
	}
}

// Counters made to run faster turn faster from where they are, and slowed down
// again they run on from there: never a jump and never a step back, either of
// which would read as a wrap.
func TestCountersChangeSpeedWithoutJumping(t *testing.T) {
	a := &Agent{}
	t0 := time.Now()
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	seconds := func(s float64) float64 {
		c := clock{started: t0, now: at(s)}
		if fs := a.faults.Load(); fs != nil {
			c.warp = fs.warp
		}
		return counterSeconds(c)
	}
	a.setFaultsLocked(Faults{}, t0, t0)
	if got := seconds(10); got != 10 {
		t.Fatalf("at 10 s the counters count %v", got)
	}
	a.setFaultsLocked(Faults{CounterSpeed: 100}, at(10), t0)
	if got := seconds(10); got != 10 {
		t.Errorf("sped up, the counters jumped to %v", got)
	}
	if got := seconds(11); got != 110 {
		t.Errorf("a second at ×100 counts %v", got)
	}
	a.setFaultsLocked(Faults{LossPercent: 5, CounterSpeed: 100}, at(11), t0)
	if got := seconds(12); got != 210 {
		t.Errorf("another fault moved the counters: %v", got)
	}
	a.setFaultsLocked(Faults{}, at(12), t0)
	if got := seconds(12); got != 210 {
		t.Errorf("slowed down, the counters went to %v", got)
	}
	if got := seconds(13); got != 211 {
		t.Errorf("a second at ×1 counts %v", got)
	}
}

// A running device's faults change from its next message on: a mute device
// answers nothing, one made to err answers with the error, a slow one late,
// and one cleared answers again — none of it restarting it.
func TestFaultsChangeWhileTheDeviceRuns(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public"})
	g := manager(a, gosnmp.Version2c, "public")
	g.Timeout = silent
	connect(t, g)
	set := func(f Faults) {
		t.Helper()
		if err := a.SetFaults(f); err != nil {
			t.Fatal(err)
		}
	}

	set(Faults{Mute: true})
	if _, err := g.Get([]string{sysDescrOID}); err == nil {
		t.Error("a mute device answered")
	}
	set(Faults{Error: FaultGenErr, ErrorPercent: 100})
	if res, err := g.Get([]string{sysDescrOID}); err != nil || res.Error != gosnmp.GenErr {
		t.Errorf("made to answer genErr: %v, %v", res, err)
	}
	set(Faults{Error: FaultTooBig, ErrorPercent: 100})
	if res, err := g.Get([]string{sysDescrOID}); err != nil || res.Error != gosnmp.TooBig || len(res.Variables) != 0 {
		t.Errorf("made to answer tooBig: %v, %v", res, err)
	}
	set(Faults{LatencyMs: 300})
	g.Timeout = 3 * time.Second
	start := time.Now()
	if res, err := g.Get([]string{sysDescrOID}); err != nil || res.Error != gosnmp.NoError {
		t.Errorf("a slow device: %v, %v", res, err)
	}
	if took := time.Since(start); took < 300*time.Millisecond {
		t.Errorf("a device made to wait 300 ms answered in %v", took)
	}
	set(Faults{})
	if res, err := g.Get([]string{sysDescrOID}); err != nil || res.Error != gosnmp.NoError {
		t.Errorf("cleared: %v, %v", res, err)
	}
	if err := a.SetFaults(Faults{LossPercent: 101}); err == nil {
		t.Error("a loss of 101 % was set")
	}
}
