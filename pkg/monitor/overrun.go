package monitor

import "time"

// The guardrail: what happens when a session cannot poll as fast as it says.
//
// A preset is a file somebody else wrote, and it declares a cadence. Bind three
// of them to a switch over a WAN link and the honest outcome is not an error —
// it is a session whose poll round takes longer than its own period. Go's
// ticker COALESCES the ticks it missed, so the loop is never idle: it finishes
// a round and the next one is already due. The device is asked continuously,
// the inserts never stop arriving on a database with a single writer, and
// nothing anywhere says a word. The session simply stops meaning what it says —
// "every 30 s" becomes "as fast as it can", which is a different product.
//
// Three rules carry this, and each is a defect in the version without it.
//
// THE MEASUREMENT IS THE CYCLE, NOT THE OID COUNT. Sixty OIDs against a chassis
// on the same switch cost less than five over a satellite link, so no count
// drawn on a preset predicts what it costs here — a bound on OIDs would refuse
// legitimate presets while admitting expensive ones. That is why pkg/preset's
// limits are sanity bounds and say so: the policy is this file.
//
// A CYCLE SPENT WAITING FOR A DEVICE THAT IS DOWN IS NOT EVIDENCE OF LOAD.
// An unreachable target costs wall-clock and almost nothing else, and it is
// precisely the moment monitoring matters: backing off there would slow down
// the reachability detection during the outage it exists to report. So a round
// where half the readings or more came back as errors counts neither way.
//
// RECOVERY IS JUDGED AGAINST THE REQUESTED INTERVAL, NEVER THE EFFECTIVE ONE.
// Once backed off, every cycle fits the widened period trivially — judging
// against it would recover on the next round, overrun again, and flap forever,
// one report per tick. The hysteresis is the other half: a cycle has to fit in
// HALF the requested interval to count as recovered, so a session sitting on
// the boundary settles instead of oscillating.

const (
	// One slow round is a retransmit, not a condition. Three in a row is a
	// condition.
	overrunStreak = 3
	recoverStreak = 3

	// Recovered means comfortably inside, not barely inside.
	recoverDivisor = 2

	// The back-off targets a 50% duty cycle: half the time on the wire and in
	// the database, half the time idle. Slowing to exactly the cycle time would
	// leave the loop as busy as it was.
	backoffHeadroom = 2

	// And it is capped, so a monitoring can never quietly become hourly — and
	// so that recovery, which is measured in cycles, stays timely: at eight
	// times the interval, three good rounds are 24 periods away at worst, not
	// an afternoon.
	maxBackoffFactor = 8
)

// Overrun says a session cannot poll as fast as it promised, and what the
// scheduler did about it. Milliseconds rather than time.Duration because this
// struct exists to be REPORTED — it crosses the bridge and lands in the event
// journal, where a nanosecond count is not a number anyone reads.
type Overrun struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	// What the session asked for.
	IntervalMs int `json:"intervalMs"`
	// What one round actually costs, measured end to end: the fetch, the
	// persist, the threshold evaluation and the emit.
	CycleMs int `json:"cycleMs"`
	// What it polls at now. Equal to IntervalMs when the operator has accepted
	// the slowdown, or when this edge is the recovery.
	EffectiveMs int `json:"effectiveMs"`
	OIDs        int `json:"oids"`
	Targets     int `json:"targets"`
	// Accepted: the operator was shown this and chose to keep the cadence.
	Accepted bool `json:"accepted"`
	// Recovered: this edge is the return to normal, not the departure from it.
	Recovered bool `json:"recovered"`
}

// cycleStat is one poll round, measured.
type cycleStat struct {
	dur      time.Duration
	readings int
	failed   int
}

// evidence reports whether this round says anything about LOAD.
//
// A round with no readings at all measured nothing. A round where half the
// readings or more are errors measured a timeout, and a timeout is the device
// being unreachable rather than this application being overloaded.
func (c cycleStat) evidence() bool {
	return c.readings > 0 && c.failed*2 < c.readings
}

// overrunGuard is the state of one session's guardrail. Pure: it holds no
// clock, takes no lock and touches nothing outside itself, so the whole policy
// is testable without waiting for anything.
type overrunGuard struct {
	interval    time.Duration
	accepted    bool
	over, under int
	engaged     bool
	cycle       time.Duration
	effective   time.Duration
}

func newOverrunGuard(interval time.Duration, accepted bool) *overrunGuard {
	return &overrunGuard{interval: interval, accepted: accepted, effective: interval}
}

// observe feeds one round in and returns the edge to report, or nil.
//
// One report per EPISODE, not per tick: an operator who left a preset running
// overnight must find one line saying it could not keep up, not four thousand.
// The scheduler still widens further, silently, if a session gets worse while
// already engaged — the number in the first report is what it cost then, and
// re-reporting every degradation is the flapping this avoids.
func (g *overrunGuard) observe(c cycleStat) *Overrun {
	if !c.evidence() {
		return nil
	}

	if c.dur > g.interval {
		g.under = 0
		g.over++
		if !g.engaged && g.over >= overrunStreak {
			g.engaged = true
			g.cycle = c.dur
			g.effective = g.backoff(c.dur)
			return g.edge(false)
		}
		if g.engaged && c.dur > g.cycle {
			g.cycle = c.dur
			g.effective = g.backoff(c.dur)
		}
		return nil
	}

	g.over = 0
	// Against the REQUESTED interval. See the note at the top of this file.
	if c.dur*recoverDivisor > g.interval {
		return nil
	}
	g.under++
	if g.engaged && g.under >= recoverStreak {
		g.engaged = false
		g.cycle = c.dur
		g.effective = g.interval
		return g.edge(true)
	}
	return nil
}

// accept is the operator saying "yes, I know, keep the cadence I chose".
//
// It undoes the back-off immediately rather than at the next round, because the
// next round can be eight intervals away — waiting it out would make the button
// look broken.
func (g *overrunGuard) accept() {
	g.accepted = true
	g.effective = g.interval
}

func (g *overrunGuard) backoff(cycle time.Duration) time.Duration {
	if g.accepted {
		return g.interval
	}
	d := cycle * backoffHeadroom
	if capped := g.interval * maxBackoffFactor; d > capped {
		d = capped
	}
	if d < g.interval {
		d = g.interval
	}
	return d
}

func (g *overrunGuard) edge(recovered bool) *Overrun {
	return &Overrun{
		IntervalMs:  int(g.interval / time.Millisecond),
		CycleMs:     int(g.cycle / time.Millisecond),
		EffectiveMs: int(g.effective / time.Millisecond),
		Accepted:    g.accepted,
		Recovered:   recovered,
	}
}
