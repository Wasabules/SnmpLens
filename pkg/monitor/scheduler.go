package monitor

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"time"
)

// Reading is one target's answer to one GET.
type Reading struct {
	Target string `json:"target"`
	// OID says which of the requested OIDs this reading answers. It exists
	// because one Fetch now returns every OID for every target in one slice,
	// and a reading that cannot say what it measured cannot be paired with the
	// previous sample it derives a rate from.
	OID            string   `json:"oid"`
	Value          *float64 `json:"value"`
	SnmpType       string   `json:"snmpType"`
	ResponseTimeMs int      `json:"responseTimeMs"`
	Error          string   `json:"error,omitempty"`
}

// FetchFunc performs one GET of oid against every target.
//
// The scheduler deliberately knows nothing about SNMP versions, communities or
// v3 passphrases: the caller closes over them when it builds this function.
// That keeps credentials out of this package entirely and makes the whole
// scheduler testable without a network.
// FetchFunc reads EVERY oid from EVERY target, in as few round trips as the
// transport allows, and returns one Reading per (target, oid) pair — including
// for failures, because a pair with no reading at all is indistinguishable
// from a session nobody started.
//
// It used to take one OID. The scheduler then walked the OIDs serially while
// only the targets ran concurrently, so a session with ten OIDs against an
// unreachable device spent ten full timeouts in a row on the poll clock — the
// one that runs with the window closed.
type FetchFunc func(ctx context.Context, oids []string, targets []string) []Reading

// Point is one stored sample, with the derived values the charts need.
type Point struct {
	SessionID      string   `json:"sessionId"`
	Target         string   `json:"target"`
	OID            string   `json:"oid"`
	Timestamp      string   `json:"timestamp"`
	Value          *float64 `json:"value"`
	Delta          *float64 `json:"delta"`
	Rate           *float64 `json:"rate"`
	ResponseTimeMs int      `json:"responseTimeMs"`
	Error          string   `json:"error,omitempty"`
	SnmpType       string   `json:"snmpType,omitempty"`
}

// SessionSpec is everything the scheduler needs to run one monitoring.
type SessionSpec struct {
	ID         string
	Name       string
	OIDs       []string
	Targets    []string
	Interval   time.Duration
	Thresholds map[string]*Threshold
	Fetch      FetchFunc
	// AcceptSlow is the operator having been shown that this session cannot
	// poll as fast as it asks, and having chosen to keep the cadence anyway.
	// It suppresses the back-off, never the report: see overrun.go.
	AcceptSlow bool
}

// minInterval floors the poll period. A zero or negative interval from a
// corrupt row would otherwise spin a goroutine flat out against a device.
const minInterval = 250 * time.Millisecond

// Scheduler owns the poll clock.
//
// This is what makes "service mode" real. While the clock lived in the
// renderer's setInterval, closing the window stopped every monitoring, which
// silently disabled the thresholds, the event journal and every notification
// route that depends on them — the alerting stack was only as available as the
// window. Here the loop is a goroutine that neither knows nor cares whether a
// webview exists.
type Scheduler struct {
	mu      sync.Mutex
	running map[string]*handle

	// Persist stores samples. Required.
	Persist func(points []Point)
	// Evaluate feeds the threshold engine. Optional.
	Evaluate func(sessionID, sessionName string, samples []Sample, thresholds map[string]*Threshold) error
	// Emit pushes samples to the UI when one is listening. Optional: with no
	// window open this is simply a no-op, and nothing else changes.
	Emit func(sessionID string, points []Point)
	// OnStateChange fires when a session starts or stops, for the tray read-out.
	OnStateChange func()
	// OnPanic reports a poll round that panicked, after the loop has recovered
	// from it. Optional: with nothing wired the panic is still contained and
	// still logged, and only the journal entry is missing.
	OnPanic func(sessionID, name string, recovered string, stack string)
	// OnOverrun reports that a session cannot poll as fast as it promised, and
	// what the scheduler did about it. Edge-triggered: once when a session
	// stops keeping up, once when it starts again. Called on the poll
	// goroutine, so a handler that blocks delays that session's next round —
	// which is why it fires once per episode rather than once per tick.
	OnOverrun func(o Overrun)
	// Now is injectable so tests are not at the mercy of the wall clock.
	Now func() time.Time
}

type handle struct {
	cancel context.CancelFunc
	done   chan struct{}
	// accept carries the operator's answer to an overrun report into the loop
	// goroutine. Buffered, and sent to without blocking, because the caller is
	// a bound method on the Wails thread and must never wait on a poll.
	accept chan struct{}
}

// NewScheduler returns an idle scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{running: map[string]*handle{}, Now: time.Now}
}

// lastSample remembers the previous reading of one series, for delta and rate.
type lastSample struct {
	value float64
	at    time.Time
	typ   string
}

// Start begins polling a session. Starting one already running is a no-op, so
// a double click in the UI cannot produce two clocks on the same device.
func (s *Scheduler) Start(spec SessionSpec) {
	if spec.ID == "" || spec.Fetch == nil || len(spec.OIDs) == 0 || len(spec.Targets) == 0 {
		return
	}
	if spec.Interval < minInterval {
		spec.Interval = minInterval
	}

	s.mu.Lock()
	if _, exists := s.running[spec.ID]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &handle{cancel: cancel, done: make(chan struct{}), accept: make(chan struct{}, 1)}
	s.running[spec.ID] = h
	s.mu.Unlock()

	go s.loop(ctx, h, spec)
	s.notifyStateChange()
}

// startSpreadWindow bounds how late a first poll can be.
//
// Two seconds is enough to break the simultaneity that matters — the burst of
// inserts against SQLite's single writer, and the burst of sockets — while
// staying under what anyone notices as a delay when they press Start.
const startSpreadWindow = 2 * time.Second

// startSpread is where in the window this session's first poll falls.
//
// FNV-1a over the id: cheap, dependency-free, and deterministic, which is the
// property that matters. A random offset would spread just as well and could
// not be tested, and would reshuffle every restart so a session's slot would
// never settle.
func startSpread(id string, interval time.Duration) time.Duration {
	window := startSpreadWindow
	if interval < window {
		// A fast session must not have its first point pushed past its own
		// period, or the spread would look like a missed poll.
		window = interval
	}
	if window <= 0 {
		return 0
	}
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	for i := 0; i < len(id); i++ {
		h ^= uint64(id[i])
		h *= prime64
	}
	return time.Duration(h % uint64(window))
}

func (s *Scheduler) loop(ctx context.Context, h *handle, spec SessionSpec) {
	defer close(h.done)

	last := map[string]lastSample{}
	ticker := time.NewTicker(spec.Interval)
	defer ticker.Stop()

	// The guardrail. It reads the CYCLE TIME rather than anything declared,
	// widens the period when a session cannot keep up, and reports the edge so
	// the operator can accept the slowdown instead of having it chosen for
	// them. All of the policy is in overrun.go, which holds no clock.
	guard := newOverrunGuard(spec.Interval, spec.AcceptSlow)
	effective := spec.Interval
	recadence := func() {
		if guard.effective != effective {
			effective = guard.effective
			ticker.Reset(effective)
		}
	}
	report := func(o *Overrun) {
		if o == nil {
			return
		}
		o.SessionID, o.Name = spec.ID, spec.Name
		o.OIDs, o.Targets = len(spec.OIDs), len(spec.Targets)
		if s.OnOverrun != nil {
			s.OnOverrun(*o)
		}
	}

	// Spread the first poll, then keep the promise of a prompt first point.
	//
	// Measured before this existed: twenty sessions started back to back — which
	// is exactly what resumeActiveSessions does after a reboot — took their
	// first tick with a spread of 0 ms. Every device asked at once, every insert
	// arriving at once on a database with one writer and a four-connection pool.
	//
	// The offset is DERIVED FROM THE SESSION ID, not drawn at random: a session
	// lands in the same slot on every restart, so the spread is stable and can
	// be asserted rather than hoped for. Two sessions can still collide; twenty
	// cannot all collide.
	//
	// Bounded by startSpreadWindow rather than by the interval, because the
	// comment this replaces was right: waiting a full interval for the first
	// point makes a slow monitoring look broken, and an hourly session must not
	// take an hour to show anything.
	if d := startSpread(spec.ID, spec.Interval); d > 0 {
		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	report(guard.observe(s.safeTick(ctx, spec, last)))
	recadence()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.accept:
			// Answered while backed off, so the cadence is restored now rather
			// than at the next round — which can be eight periods away.
			guard.accept()
			recadence()
		case <-ticker.C:
			// A poll slower than the interval simply delays the next tick.
			// Go coalesces the missed ticks, so a struggling agent can never
			// pile up overlapping rounds against itself — and that coalescing
			// is exactly why the guardrail is needed: the loop stops being
			// idle and nothing about it looks wrong.
			report(guard.observe(s.safeTick(ctx, spec, last)))
			recadence()
		}
	}
}

// safeTick is tick, with the process's life not depending on it.
//
// A poll round runs on its own goroutine and calls out to a Fetch the scheduler
// did not write, then to Persist, Evaluate and Emit — four callbacks, each of
// which is somebody else's code. An unrecovered panic in any of them does not
// end the session: it ends the PROCESS, taking every other session, the trap
// listener and the notification outbox with it. pkg/notify recovers around
// each delivery for exactly this reason, and this loop has the same shape.
//
// Recovered per ROUND rather than per session, so a session that panics once —
// a nil map from a half-configured target, say — keeps polling on the next
// tick instead of stopping silently.
func (s *Scheduler) safeTick(ctx context.Context, spec SessionSpec, last map[string]lastSample) (stat cycleStat) {
	defer func() {
		if r := recover(); r != nil {
			trace := string(debug.Stack())
			log.Printf("monitor: session %s panicked during a poll and was recovered: %v; %s",
				spec.ID, r, trace)
			if s.OnPanic != nil {
				s.OnPanic(spec.ID, spec.Name, fmt.Sprint(r), trace)
			}
			// A round that panicked measured nothing, so the guardrail must not
			// read it as a fast cycle and conclude the session is keeping up.
			stat = cycleStat{}
		}
	}()
	return s.tick(ctx, spec, last)
}

// tick runs one poll round and returns what it cost.
//
// The measurement covers the WHOLE round — the fetch, the persist, the
// threshold evaluation and the emit — because all four are the load, and the
// question the guardrail answers is whether a round fits inside the period the
// session promised.
func (s *Scheduler) tick(ctx context.Context, spec SessionSpec, last map[string]lastSample) cycleStat {
	now := s.now()
	stamp := now.UTC().Format(time.RFC3339Nano)
	started := time.Now()
	stat := cycleStat{}

	var points []Point
	var samples []Sample

	if ctx.Err() != nil {
		return stat
	}
	readings := spec.Fetch(ctx, spec.OIDs, spec.Targets)
	if ctx.Err() != nil {
		// A CANCELLED round is not a measurement, and must not be stored.
		//
		// This became load-bearing the moment the fetch started honouring the
		// context: what a cancelled poll returns is one error per OID, and
		// persisting those breaks every series while the evaluator reads them
		// as a device that stopped answering. Pressing Stop would have raised
		// an unreachability alert on a healthy switch.
		return stat
	}
	for _, r := range readings {
		stat.readings++
		if r.Error != "" {
			// An ERROR, not a nil value: a string OID answers with no numeric
			// value and no error at all, and counting that as a failure would
			// leave a session polling sysDescr with a guardrail that can never
			// engage.
			stat.failed++
		}
		oid := r.OID
		key := r.Target + "|" + oid
		p := Point{
			SessionID: spec.ID, Target: r.Target, OID: oid, Timestamp: stamp,
			Value: r.Value, ResponseTimeMs: r.ResponseTimeMs, Error: r.Error, SnmpType: r.SnmpType,
		}

		if r.Value != nil {
			if prev, ok := last[key]; ok {
				typ := r.SnmpType
				if typ == "" {
					typ = prev.typ
				}
				if d, ok := CorrectedDelta(prev.value, *r.Value, typ); ok {
					delta := d
					p.Delta = &delta
					if dt, ok := ElapsedSeconds(prev.at, now); ok {
						rate := d / dt
						p.Rate = &rate
					}
				}
			}
			last[key] = lastSample{value: *r.Value, at: now, typ: r.SnmpType}
		} else {
			// A failed poll breaks the series: the next delta must not
			// span the outage as though nothing happened.
			delete(last, key)
		}

		points = append(points, p)
		samples = append(samples, Sample{
			Target: r.Target, OID: oid, Timestamp: stamp, Value: r.Value, Error: r.Error,
		})
	}

	if len(points) == 0 {
		return stat
	}
	if s.Persist != nil {
		s.Persist(points)
	}
	if s.Evaluate != nil {
		// A detection failure must never stop the clock.
		_ = s.Evaluate(spec.ID, spec.Name, samples, spec.Thresholds)
	}
	if s.Emit != nil {
		s.Emit(spec.ID, points)
	}
	// Wall clock deliberately, not s.Now: this measures how long the round
	// actually took, and s.Now is injected by tests that move time in jumps.
	stat.dur = time.Since(started)
	return stat
}

// AcceptSlow is the operator answering an overrun report: keep the cadence I
// chose, I accept that it slows things down. It applies to the running session
// and takes effect at once.
//
// Not persisted here on purpose — the scheduler holds no storage. A session
// resumed after a restart asks again, which is the right default for a decision
// about this machine's load.
func (s *Scheduler) AcceptSlow(sessionID string) {
	s.mu.Lock()
	h := s.running[sessionID]
	s.mu.Unlock()
	if h == nil {
		return
	}
	select {
	case h.accept <- struct{}{}:
	default:
	}
}

// Stop ends one session and waits for its goroutine, so a caller that stops
// then closes the database cannot race the final write.
func (s *Scheduler) Stop(sessionID string) {
	s.mu.Lock()
	h := s.running[sessionID]
	delete(s.running, sessionID)
	s.mu.Unlock()

	if h == nil {
		return
	}
	h.cancel()
	<-h.done
	s.notifyStateChange()
}

// StopAll ends every session.
func (s *Scheduler) StopAll() {
	s.mu.Lock()
	handles := make([]*handle, 0, len(s.running))
	for _, h := range s.running {
		handles = append(handles, h)
	}
	s.running = map[string]*handle{}
	s.mu.Unlock()

	for _, h := range handles {
		h.cancel()
	}
	for _, h := range handles {
		<-h.done
	}
	if len(handles) > 0 {
		s.notifyStateChange()
	}
}

// IsRunning reports whether a session is polling.
func (s *Scheduler) IsRunning(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[sessionID]
	return ok
}

// Running lists the polling session ids.
func (s *Scheduler) Running() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.running))
	for id := range s.running {
		ids = append(ids, id)
	}
	return ids
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Scheduler) notifyStateChange() {
	if s.OnStateChange != nil {
		s.OnStateChange()
	}
}
