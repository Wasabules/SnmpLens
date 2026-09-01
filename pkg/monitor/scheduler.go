package monitor

import (
	"context"
	"sync"
	"time"
)

// Reading is one target's answer to one GET.
type Reading struct {
	Target         string   `json:"target"`
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
type FetchFunc func(ctx context.Context, oid string, targets []string) []Reading

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
	// Now is injectable so tests are not at the mercy of the wall clock.
	Now func() time.Time
}

type handle struct {
	cancel context.CancelFunc
	done   chan struct{}
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
	h := &handle{cancel: cancel, done: make(chan struct{})}
	s.running[spec.ID] = h
	s.mu.Unlock()

	go s.loop(ctx, h, spec)
	s.notifyStateChange()
}

func (s *Scheduler) loop(ctx context.Context, h *handle, spec SessionSpec) {
	defer close(h.done)

	last := map[string]lastSample{}
	ticker := time.NewTicker(spec.Interval)
	defer ticker.Stop()

	// Poll immediately: waiting a full interval for the first point makes a
	// slow monitoring look broken.
	s.tick(ctx, spec, last)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A poll slower than the interval simply delays the next tick.
			// Go coalesces the missed ticks, so a struggling agent can never
			// pile up overlapping rounds against itself.
			s.tick(ctx, spec, last)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, spec SessionSpec, last map[string]lastSample) {
	now := s.now()
	stamp := now.UTC().Format(time.RFC3339Nano)

	var points []Point
	var samples []Sample

	for _, oid := range spec.OIDs {
		if ctx.Err() != nil {
			return
		}
		for _, r := range spec.Fetch(ctx, oid, spec.Targets) {
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
	}

	if len(points) == 0 {
		return
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
