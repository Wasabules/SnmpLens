package simulator

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/gosnmp/gosnmp"
)

// Faults are what a simulated device is made to do wrong, so that SnmpLens can
// be tested against it: a reachability alert against a device that stops
// answering, the overload guardrail against one that answers slowly, the rate
// derived across a counter's wrap against one whose counters run fast. They are
// changed while the device runs (Agent.SetFaults) — restarting it would throw
// away the uptime and the counters a test is watching — and kept with it.
type Faults struct {
	// LatencyMs delays every answer, and JitterMs adds up to as much again at
	// random.
	LatencyMs int `json:"latencyMs"`
	JitterMs  int `json:"jitterMs"`
	// LossPercent of the requests are lost before the agent sees them, as a
	// lossy link loses them.
	LossPercent int `json:"lossPercent"`
	// Mute answers nothing: a device that has stopped answering. What it sends
	// — its notifications — it still sends.
	Mute bool `json:"mute"`
	// Error answers ErrorPercent of the requests with that error instead of
	// their answer: FaultTooBig or FaultGenErr.
	Error        string `json:"error"`
	ErrorPercent int    `json:"errorPercent"`
	// CounterSpeed runs the counters that many times faster — 1 (or 0), 10, 100
	// or 1000 — so that a Counter32 wraps in minutes rather than in days.
	CounterSpeed int `json:"counterSpeed"`
}

// MaxLatencyMs bounds a latency and its jitter: past it, a manager has long
// since given up.
const MaxLatencyMs = 10000

// The errors a device can be made to answer with.
const (
	FaultTooBig = "tooBig"
	FaultGenErr = "genErr"
)

// counterSpeeds are the speeds a device's counters can run at; 0 is 1.
var counterSpeeds = []int{0, 1, 10, 100, 1000}

// Check reports the first thing wrong with f.
func (f Faults) Check() error {
	switch {
	case f.LatencyMs < 0 || f.LatencyMs > MaxLatencyMs || f.JitterMs < 0 || f.JitterMs > MaxLatencyMs:
		return fmt.Errorf("a latency and its jitter are 0 to %d ms", MaxLatencyMs)
	case f.LossPercent < 0 || f.LossPercent > 100:
		return errors.New("a loss is 0 to 100 %")
	case f.Error != "" && f.Error != FaultTooBig && f.Error != FaultGenErr:
		return fmt.Errorf("%q is not an error a device can be made to answer with: %s or %s", f.Error, FaultTooBig, FaultGenErr)
	case f.Error != "" && (f.ErrorPercent < 1 || f.ErrorPercent > 100):
		return errors.New("an error answers 1 to 100 % of the requests")
	case f.Error == "" && f.ErrorPercent != 0:
		return errors.New("a share of errors needs an error")
	case !slices.Contains(counterSpeeds, f.CounterSpeed):
		return fmt.Errorf("counters run at 1, 10, 100 or 1000 times their speed, not %d", f.CounterSpeed)
	}
	return nil
}

// speed is how many times faster the counters run.
func (f Faults) speed() float64 { return float64(max(f.CounterSpeed, 1)) }

// drops reports whether a request is lost, roll being drawn from [0, 100).
func (f Faults) drops(roll int) bool { return f.Mute || roll < f.LossPercent }

// errs reports whether a request is answered with the error, roll being drawn
// from [0, 100).
func (f Faults) errs(roll int) bool { return f.Error != "" && roll < f.ErrorPercent }

// delay is how long an answer waits, share being drawn from [0, 1).
func (f Faults) delay(share float64) time.Duration {
	return time.Duration((float64(f.LatencyMs) + share*float64(f.JitterMs)) * float64(time.Millisecond))
}

// roll and draw are the chances a fault is decided by. Not a secret, so
// math/rand's.
func roll() int     { return rand.IntN(100) }
func draw() float64 { return rand.Float64() }

// warp is how far the counters had run, in the seconds they count, when their
// speed last changed: they run on from there at the new speed. So a counter
// made to run faster turns faster and never jumps, and one slowed down again
// never goes back — a counter that went back would read as a wrap.
type warp struct {
	at    time.Time
	base  float64 // the counters' seconds at `at`
	speed float64
}

// counterSeconds is how far the counters have run at c: the time since the
// agent started, or where its warp has taken them.
func counterSeconds(c clock) float64 {
	if c.warp == nil {
		return elapsed(c)
	}
	return c.warp.base + c.now.Sub(c.warp.at).Seconds()*c.warp.speed
}

// faultState is the faults an agent runs with, and where its counters are.
type faultState struct {
	faults Faults
	warp   *warp
}

// SetFaults changes what a running agent does wrong, from its next message on,
// without restarting it.
func (a *Agent) SetFaults(f Faults) error {
	if err := f.Check(); err != nil {
		return err
	}
	a.mu.Lock()
	started := a.started
	a.mu.Unlock()
	a.faultMu.Lock()
	defer a.faultMu.Unlock()
	if started.IsZero() {
		// Start runs the counters from where the faults say.
		a.faults.Store(&faultState{faults: f})
		return nil
	}
	a.setFaultsLocked(f, time.Now(), started)
	return nil
}

// setFaultsLocked keeps f, and moves the counters' warp on when their speed
// changes, so that they run on from where they are at now. faultMu is held.
func (a *Agent) setFaultsLocked(f Faults, now, started time.Time) {
	next := &faultState{faults: f}
	if prev := a.faults.Load(); prev != nil {
		next.warp = prev.warp
	}
	switch speed := f.speed(); {
	case next.warp == nil && speed == 1:
	case next.warp == nil:
		next.warp = &warp{at: now, base: now.Sub(started).Seconds(), speed: speed}
	case next.warp.speed != speed:
		next.warp = &warp{at: now, base: counterSeconds(clock{started: started, now: now, warp: next.warp}), speed: speed}
	}
	a.faults.Store(next)
}

// injectedError is the error the agent's faults answer a request with instead
// of its answer, if they answer this one with one.
func (a *Agent) injectedError(ver gosnmp.SnmpVersion, req *gosnmp.SnmpPacket) (answer, bool) {
	fs := a.faults.Load()
	if fs == nil || !fs.faults.errs(roll()) {
		return answer{}, false
	}
	if fs.faults.Error == FaultTooBig {
		return tooBig(ver, req), true
	}
	return answer{vars: req.Variables, status: gosnmp.GenErr, index: errIndex(0, len(req.Variables))}, true
}
