package monitor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func fits(d time.Duration) cycleStat { return cycleStat{dur: d, readings: 4} }

func timeouts(d time.Duration) cycleStat {
	return cycleStat{dur: d, readings: 4, failed: 4}
}

// Three rounds that do not fit is a condition. One is a retransmit.
func TestOneSlowRoundIsNotAnOverrun(t *testing.T) {
	g := newOverrunGuard(time.Second, false)
	if o := g.observe(fits(2 * time.Second)); o != nil {
		t.Fatal("one slow round engaged the guardrail")
	}
	if o := g.observe(fits(100 * time.Millisecond)); o != nil {
		t.Fatal("a fast round reported something")
	}
	// The streak restarts: two slow rounds either side of a fast one is not
	// three in a row.
	g.observe(fits(2 * time.Second))
	if o := g.observe(fits(2 * time.Second)); o != nil {
		t.Fatal("the streak did not restart after a round that fitted")
	}
	if o := g.observe(fits(2 * time.Second)); o == nil {
		t.Fatal("three rounds in a row did not engage the guardrail")
	}
}

// What it does about it: widen the period so the loop is idle half the time.
func TestEngagingWidensThePeriodAndSaysSo(t *testing.T) {
	g := newOverrunGuard(time.Second, false)
	var edge *Overrun
	for i := 0; i < overrunStreak; i++ {
		edge = g.observe(fits(1500 * time.Millisecond))
	}
	if edge == nil {
		t.Fatal("no edge was reported")
	}
	if edge.IntervalMs != 1000 || edge.CycleMs != 1500 {
		t.Errorf("the report does not say what happened: %+v", edge)
	}
	if edge.EffectiveMs != 3000 {
		t.Errorf("EffectiveMs = %d, want 3000 (twice the cycle: half the time idle)", edge.EffectiveMs)
	}
	if edge.Recovered || edge.Accepted {
		t.Errorf("a departure was reported as a recovery or as accepted: %+v", edge)
	}
	if g.effective != 3*time.Second {
		t.Errorf("the guard polls at %v, not the period it reported", g.effective)
	}
}

// One line in the journal, not four thousand.
func TestOneReportPerEpisode(t *testing.T) {
	g := newOverrunGuard(time.Second, false)
	reports := 0
	for i := 0; i < 50; i++ {
		if g.observe(fits(1500*time.Millisecond)) != nil {
			reports++
		}
	}
	if reports != 1 {
		t.Fatalf("%d reports for one episode", reports)
	}
	// It still widens further, silently, if the session gets worse.
	g.observe(fits(4 * time.Second))
	if g.effective != 8*time.Second {
		t.Errorf("effective = %v; a worse cycle did not widen the period", g.effective)
	}
}

// The back-off is capped, so a monitoring can never quietly become hourly and
// recovery, which is counted in rounds, stays reachable.
func TestTheBackoffIsCapped(t *testing.T) {
	g := newOverrunGuard(10*time.Second, false)
	for i := 0; i < overrunStreak; i++ {
		g.observe(fits(10 * time.Minute))
	}
	if want := 10 * time.Second * maxBackoffFactor; g.effective != want {
		t.Errorf("effective = %v, want the cap %v", g.effective, want)
	}
}

// A cycle spent waiting for a device that is not answering measures the
// network, not this application. Backing off there would slow the reachability
// detection down at the exact moment monitoring matters.
//
// Measured, so the shape is not hypothetical: 30 OIDs against a silent agent
// cost 4.0 s at the default two-second timeout with one retry, and 60 OIDs cost
// 8.0 s — comfortably past any interval a preset may declare.
func TestTimeoutsAreNotEvidenceOfLoad(t *testing.T) {
	g := newOverrunGuard(time.Second, false)
	for i := 0; i < 20; i++ {
		if o := g.observe(timeouts(8 * time.Second)); o != nil {
			t.Fatalf("round %d: an unreachable device engaged the load guardrail", i)
		}
	}
	if g.engaged {
		t.Error("the guardrail engaged on timeouts")
	}
	// Half failing is still not evidence; a majority answering is.
	g2 := newOverrunGuard(time.Second, false)
	for i := 0; i < overrunStreak; i++ {
		g2.observe(cycleStat{dur: 8 * time.Second, readings: 4, failed: 2})
	}
	if g2.engaged {
		t.Error("half the readings failing was read as load")
	}
	g3 := newOverrunGuard(time.Second, false)
	for i := 0; i < overrunStreak; i++ {
		g3.observe(cycleStat{dur: 2 * time.Second, readings: 4, failed: 1})
	}
	if !g3.engaged {
		t.Error("a mostly-answering round was not counted as load")
	}
	// A round that produced no readings at all measured nothing.
	g4 := newOverrunGuard(time.Second, false)
	for i := 0; i < overrunStreak; i++ {
		g4.observe(cycleStat{dur: 8 * time.Second})
	}
	if g4.engaged {
		t.Error("a round with no readings was read as load")
	}
}

// Recovery is judged against the REQUESTED interval. Judged against the widened
// one, every round fits trivially and the guard flaps: recover, overrun,
// recover, one report per tick, forever.
func TestRecoveryIsJudgedAgainstTheRequestedInterval(t *testing.T) {
	g := newOverrunGuard(time.Second, false)
	for i := 0; i < overrunStreak; i++ {
		g.observe(fits(1500 * time.Millisecond))
	}
	if !g.engaged {
		t.Fatal("setup: the guard did not engage")
	}

	// 1.2 s fits comfortably inside the 3 s effective period and is still an
	// overrun of the second the session promised.
	for i := 0; i < 10; i++ {
		if o := g.observe(fits(1200 * time.Millisecond)); o != nil {
			t.Fatalf("round %d recovered against the widened period: %+v", i, o)
		}
	}

	// Barely inside the requested interval is not recovered either: hysteresis
	// is what stops a session on the boundary oscillating.
	for i := 0; i < 10; i++ {
		if o := g.observe(fits(900 * time.Millisecond)); o != nil {
			t.Fatalf("round %d recovered on a cycle that barely fits: %+v", i, o)
		}
	}

	var edge *Overrun
	for i := 0; i < recoverStreak; i++ {
		edge = g.observe(fits(200 * time.Millisecond))
	}
	if edge == nil {
		t.Fatal("comfortably fast rounds never recovered")
	}
	if !edge.Recovered || edge.EffectiveMs != 1000 {
		t.Errorf("the recovery does not restore the requested cadence: %+v", edge)
	}
	if g.effective != time.Second {
		t.Errorf("the guard still polls at %v", g.effective)
	}
}

// Accepting is the operator's decision, and it is about the CADENCE, not about
// silence: the report still fires, because that is what asks them.
func TestAcceptingKeepsTheCadenceAndStillReports(t *testing.T) {
	g := newOverrunGuard(time.Second, true)
	var edge *Overrun
	for i := 0; i < overrunStreak; i++ {
		edge = g.observe(fits(5 * time.Second))
	}
	if edge == nil {
		t.Fatal("an accepted session reported nothing at all")
	}
	if !edge.Accepted || edge.EffectiveMs != 1000 {
		t.Errorf("an accepted session was slowed down anyway: %+v", edge)
	}
	if g.effective != time.Second {
		t.Errorf("effective = %v; acceptance did not keep the cadence", g.effective)
	}
}

// Answering the report restores the cadence at once, because the next round can
// be eight periods away and a button that takes eight periods looks broken.
func TestAcceptingWhileBackedOffTakesEffectImmediately(t *testing.T) {
	g := newOverrunGuard(time.Second, false)
	for i := 0; i < overrunStreak; i++ {
		g.observe(fits(4 * time.Second))
	}
	if g.effective == time.Second {
		t.Fatal("setup: the guard did not back off")
	}
	g.accept()
	if g.effective != time.Second {
		t.Fatalf("effective = %v after accepting, want the requested second", g.effective)
	}
	// And it stays accepted for the rest of the session.
	for i := 0; i < overrunStreak; i++ {
		g.observe(fits(4 * time.Second))
	}
	if g.effective != time.Second {
		t.Errorf("effective = %v; the guard backed off again after being accepted", g.effective)
	}
}

/* --- the scheduler end ---------------------------------------------------- */

// The guardrail on the real clock: a session whose round does not fit reports
// once and starts polling at the widened period.
func TestASlowSessionIsReportedAndSlowedDown(t *testing.T) {
	// At minInterval, the floor: a fast session is where a round that does not
	// fit is easiest to produce, and the floor is what a preset at
	// pkg/preset's MinIntervalSec would land near on a slow link.
	const interval = minInterval
	const round = 300 * time.Millisecond

	var mu sync.Mutex
	var starts []time.Time
	var edges []Overrun

	s := NewScheduler()
	s.Persist = func([]Point) {}
	s.OnOverrun = func(o Overrun) {
		mu.Lock()
		edges = append(edges, o)
		mu.Unlock()
	}
	s.Start(SessionSpec{
		ID: "slow", Name: "slow", OIDs: []string{"1.1"}, Targets: []string{"a"},
		Interval: interval,
		Fetch: func(_ context.Context, oids, targets []string) []Reading {
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			time.Sleep(round)
			v := 1.0
			return []Reading{{Target: targets[0], OID: oids[0], Value: &v, SnmpType: "Counter32"}}
		},
	})
	time.Sleep(3 * time.Second)
	s.StopAll()

	mu.Lock()
	defer mu.Unlock()
	if len(edges) != 1 {
		t.Fatalf("%d reports, want exactly one: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.SessionID != "slow" || e.Name != "slow" || e.OIDs != 1 || e.Targets != 1 {
		t.Errorf("the report does not identify the session: %+v", e)
	}
	if e.EffectiveMs <= e.IntervalMs {
		t.Errorf("reported effective %d ms is not wider than the %d ms asked for", e.EffectiveMs, e.IntervalMs)
	}

	// And the widening is real, not just reported. Before it, the ticker
	// coalesces and the next round starts the moment the last one ends.
	if len(starts) < overrunStreak+1 {
		t.Fatalf("only %d rounds ran; the test is too short to see the change", len(starts))
	}
	gap := starts[len(starts)-1].Sub(starts[len(starts)-2])
	if gap < round+30*time.Millisecond {
		t.Errorf("the last gap was %v, no wider than the round itself: the loop is still saturated",
			gap.Round(time.Millisecond))
	}
}

// A cancelled round must not be stored. Once the fetch honours the context, what
// it returns on a stop is one error per OID — persisting those breaks every
// series, and the evaluator reads them as a device that stopped answering.
func TestACancelledRoundIsNotPersisted(t *testing.T) {
	entered := make(chan struct{})
	var mu sync.Mutex
	persisted := 0

	s := NewScheduler()
	s.Persist = func(p []Point) {
		mu.Lock()
		persisted += len(p)
		mu.Unlock()
	}
	s.Start(SessionSpec{
		ID: "cancelled", OIDs: []string{"1.1"}, Targets: []string{"a"},
		Interval: time.Hour,
		Fetch: func(ctx context.Context, oids, targets []string) []Reading {
			close(entered)
			<-ctx.Done()
			// What a cancelled GetMany answers: an error for every OID.
			return []Reading{{Target: targets[0], OID: oids[0], Error: "context canceled"}}
		},
	})
	<-entered
	s.Stop("cancelled")

	mu.Lock()
	defer mu.Unlock()
	if persisted != 0 {
		t.Fatalf("%d points from a cancelled round reached storage", persisted)
	}
}

// The operator's answer reaches the running loop: the cadence they chose comes
// back, without restarting the session and losing the baselines every rate is
// derived from.
func TestAcceptSlowRestoresTheCadenceOfARunningSession(t *testing.T) {
	const interval = minInterval
	const round = 300 * time.Millisecond

	var mu sync.Mutex
	var starts []time.Time
	reported := make(chan struct{}, 1)

	s := NewScheduler()
	s.Persist = func([]Point) {}
	s.OnOverrun = func(o Overrun) {
		select {
		case reported <- struct{}{}:
		default:
		}
	}
	s.Start(SessionSpec{
		ID: "answered", OIDs: []string{"1.1"}, Targets: []string{"a"},
		Interval: interval,
		Fetch: func(_ context.Context, oids, targets []string) []Reading {
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			time.Sleep(round)
			v := 1.0
			return []Reading{{Target: targets[0], OID: oids[0], Value: &v, SnmpType: "Counter32"}}
		},
	})
	defer s.StopAll()

	select {
	case <-reported:
	case <-time.After(3 * time.Second):
		t.Fatal("the session never reported that it could not keep up")
	}

	s.AcceptSlow("answered")
	s.AcceptSlow("no such session") // must not panic

	mu.Lock()
	from := len(starts)
	mu.Unlock()
	time.Sleep(time.Second)

	mu.Lock()
	defer mu.Unlock()
	after := starts[from:]
	if len(after) < 2 {
		t.Fatalf("%d rounds in the second after accepting; the back-off is still in force", len(after))
	}
	// Back to running flat out, which is what accepting the slowdown means.
	if gap := after[1].Sub(after[0]); gap > round+120*time.Millisecond {
		t.Errorf("gap after accepting is %v; the cadence was not restored", gap.Round(time.Millisecond))
	}
}

// A poll round must not be able to end the process.
//
// The round runs on its own goroutine and calls out to four callbacks the
// scheduler did not write — Fetch, Persist, Evaluate, Emit. An unrecovered
// panic in any of them does not end the session: it ends the PROCESS, taking
// every other session, the trap listener and the notification outbox with it.
//
// Found for real: a session started with no SNMP client dereferenced nil inside
// newGoSNMP, on the poll goroutine, and killed the test binary — on macOS only,
// because the other two platforms finished before the first tick landed.
func TestAPanicInAPollDoesNotEndTheProcess(t *testing.T) {
	var mu sync.Mutex
	rounds := 0
	var reported []string

	s := NewScheduler()
	s.Persist = func([]Point) {}
	s.OnPanic = func(sessionID, name, recovered, stack string) {
		mu.Lock()
		reported = append(reported, sessionID+": "+recovered)
		mu.Unlock()
	}

	s.Start(SessionSpec{
		ID: "explodes", Name: "boom", OIDs: []string{"1.1"}, Targets: []string{"a"},
		Interval: minInterval,
		Fetch: func(_ context.Context, oids, targets []string) []Reading {
			mu.Lock()
			rounds++
			n := rounds
			mu.Unlock()
			if n == 1 {
				var nilMap map[string]int
				//lint:ignore SA5000 the panic is the subject of this test
				nilMap["boom"] = 1
			}
			v := 1.0
			return []Reading{{Target: targets[0], OID: oids[0], Value: &v, SnmpType: "Counter32"}}
		},
	})

	time.Sleep(minInterval*3 + 400*time.Millisecond)
	s.StopAll()

	mu.Lock()
	defer mu.Unlock()
	if len(reported) == 0 {
		t.Fatal("the panic was swallowed with nothing said about it")
	}
	if !strings.Contains(reported[0], "explodes") {
		t.Errorf("the report does not name the session: %q", reported[0])
	}

	// Recovered per ROUND, not per session: the next tick must still poll, or a
	// single transient panic stops a monitoring for good and silently.
	if rounds < 2 {
		t.Errorf("%d round(s); the session stopped polling after the panic", rounds)
	}
}
