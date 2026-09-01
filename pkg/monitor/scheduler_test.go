package monitor

import (
	"context"
	"sync"
	"testing"
	"time"
)

type collector struct {
	mu      sync.Mutex
	points  []Point
	samples int
	emits   int
}

func (c *collector) persist(p []Point) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.points = append(c.points, p...)
}

func (c *collector) evaluate(_, _ string, s []Sample, _ map[string]*Threshold) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.samples += len(s)
	return nil
}

func (c *collector) emit(string, []Point) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emits++
}

func (c *collector) snapshot() ([]Point, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Point(nil), c.points...), c.samples, c.emits
}

func f64(v float64) *float64 { return &v }

// counterFetch returns a value that grows by step on every call, per target.
func counterFetch(step float64) FetchFunc {
	var mu sync.Mutex
	state := map[string]float64{}
	return func(_ context.Context, oid string, targets []string) []Reading {
		mu.Lock()
		defer mu.Unlock()
		out := make([]Reading, 0, len(targets))
		for _, t := range targets {
			key := t + "|" + oid
			state[key] += step
			out = append(out, Reading{Target: t, Value: f64(state[key]), SnmpType: "Counter32", ResponseTimeMs: 3})
		}
		return out
	}
}

func waitFor(t *testing.T, cond func() bool, why string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", why)
}

func TestSchedulerPollsEveryOidAndTarget(t *testing.T) {
	c := &collector{}
	s := NewScheduler()
	s.Persist, s.Evaluate, s.Emit = c.persist, c.evaluate, c.emit

	s.Start(SessionSpec{
		ID: "s1", OIDs: []string{"1.1", "1.2"}, Targets: []string{"a", "b"},
		Interval: 20 * time.Millisecond, Fetch: counterFetch(10),
	})
	defer s.StopAll()

	waitFor(t, func() bool { p, _, _ := c.snapshot(); return len(p) >= 8 }, "two rounds of 4 points")

	points, samples, emits := c.snapshot()
	seen := map[string]bool{}
	for _, p := range points {
		seen[p.Target+"|"+p.OID] = true
	}
	if len(seen) != 4 {
		t.Errorf("got %d distinct series, want 4 (2 targets x 2 OIDs): %v", len(seen), seen)
	}
	if samples == 0 || emits == 0 {
		t.Errorf("evaluate/emit were not called (samples=%d emits=%d)", samples, emits)
	}
}

// The first point must not wait a whole interval: a 5-minute monitoring would
// otherwise look broken for five minutes.
func TestSchedulerPollsImmediately(t *testing.T) {
	c := &collector{}
	s := NewScheduler()
	s.Persist = c.persist

	s.Start(SessionSpec{
		ID: "s1", OIDs: []string{"1.1"}, Targets: []string{"a"},
		Interval: time.Hour, Fetch: counterFetch(1),
	})
	defer s.StopAll()

	waitFor(t, func() bool { p, _, _ := c.snapshot(); return len(p) >= 1 }, "an immediate first poll")
}

func TestSchedulerDerivesDeltaAndRate(t *testing.T) {
	c := &collector{}
	s := NewScheduler()
	s.Persist = c.persist

	s.Start(SessionSpec{
		ID: "s1", OIDs: []string{"1.1"}, Targets: []string{"a"},
		Interval: 20 * time.Millisecond, Fetch: counterFetch(100),
	})
	defer s.StopAll()

	waitFor(t, func() bool { p, _, _ := c.snapshot(); return len(p) >= 2 }, "a second sample")
	points, _, _ := c.snapshot()

	if points[0].Delta != nil {
		t.Error("the very first sample has nothing to compare against; delta must be nil")
	}
	if points[1].Delta == nil || *points[1].Delta != 100 {
		t.Errorf("delta = %v, want 100", points[1].Delta)
	}
	if points[1].Rate == nil || *points[1].Rate <= 0 {
		t.Errorf("rate = %v, want a positive value", points[1].Rate)
	}
}

// After a failed poll the series restarts: the next delta must not silently
// span the outage as though the counter had advanced smoothly through it.
func TestFailedPollBreaksTheSeries(t *testing.T) {
	c := &collector{}
	s := NewScheduler()
	s.Persist = c.persist

	var n int
	var mu sync.Mutex
	fetch := func(context.Context, string, []string) []Reading {
		mu.Lock()
		defer mu.Unlock()
		n++
		switch n {
		case 1:
			return []Reading{{Target: "a", Value: f64(100), SnmpType: "Counter32"}}
		case 2:
			return []Reading{{Target: "a", Value: nil, Error: "timeout"}}
		default:
			return []Reading{{Target: "a", Value: f64(500), SnmpType: "Counter32"}}
		}
	}

	s.Start(SessionSpec{
		ID: "s1", OIDs: []string{"1.1"}, Targets: []string{"a"},
		Interval: 15 * time.Millisecond, Fetch: fetch,
	})
	defer s.StopAll()

	waitFor(t, func() bool { p, _, _ := c.snapshot(); return len(p) >= 3 }, "three polls")
	points, _, _ := c.snapshot()

	if points[1].Error == "" {
		t.Fatal("the failed poll was not recorded")
	}
	if points[2].Delta != nil {
		t.Errorf("delta = %v across an outage; the series must restart instead", *points[2].Delta)
	}
}

// A double click in the UI must not produce two clocks hammering one device.
func TestStartingTwiceIsANoOp(t *testing.T) {
	s := NewScheduler()
	s.Persist = func([]Point) {}
	spec := SessionSpec{
		ID: "s1", OIDs: []string{"1.1"}, Targets: []string{"a"},
		Interval: time.Hour, Fetch: counterFetch(1),
	}
	s.Start(spec)
	s.Start(spec)
	defer s.StopAll()

	if got := len(s.Running()); got != 1 {
		t.Errorf("Running() = %d, want 1", got)
	}
}

// Stop must not return until the goroutine is done, or shutdown would race the
// database close.
func TestStopWaitsForTheGoroutine(t *testing.T) {
	var inFlight sync.WaitGroup
	released := make(chan struct{})
	var once sync.Once

	s := NewScheduler()
	s.Persist = func([]Point) {
		once.Do(func() { close(released) })
		<-released
	}
	inFlight.Add(1)
	s.Start(SessionSpec{
		ID: "s1", OIDs: []string{"1.1"}, Targets: []string{"a"},
		Interval: 10 * time.Millisecond, Fetch: counterFetch(1),
	})
	go func() { defer inFlight.Done(); <-released }()
	inFlight.Wait()

	s.Stop("s1")
	if s.IsRunning("s1") {
		t.Error("session still marked running after Stop returned")
	}
}

func TestStateChangeFires(t *testing.T) {
	var mu sync.Mutex
	var n int
	s := NewScheduler()
	s.Persist = func([]Point) {}
	s.OnStateChange = func() { mu.Lock(); n++; mu.Unlock() }

	s.Start(SessionSpec{ID: "s1", OIDs: []string{"1.1"}, Targets: []string{"a"}, Interval: time.Hour, Fetch: counterFetch(1)})
	s.Stop("s1")

	mu.Lock()
	defer mu.Unlock()
	if n < 2 {
		t.Errorf("OnStateChange fired %d times, want at least 2 (start + stop)", n)
	}
}

// A corrupt or hand-edited row must not spin a goroutine flat out.
func TestZeroIntervalIsFloored(t *testing.T) {
	c := &collector{}
	s := NewScheduler()
	s.Persist = c.persist
	s.Start(SessionSpec{ID: "s1", OIDs: []string{"1.1"}, Targets: []string{"a"}, Interval: 0, Fetch: counterFetch(1)})
	defer s.StopAll()

	time.Sleep(120 * time.Millisecond)
	points, _, _ := c.snapshot()
	if len(points) > 3 {
		t.Errorf("%d polls in 120ms; the interval floor is not being applied", len(points))
	}
}

func TestIncompleteSpecIsRejected(t *testing.T) {
	s := NewScheduler()
	s.Start(SessionSpec{ID: "", OIDs: []string{"1.1"}, Targets: []string{"a"}, Fetch: counterFetch(1)})
	s.Start(SessionSpec{ID: "s1", Targets: []string{"a"}, Fetch: counterFetch(1)})
	s.Start(SessionSpec{ID: "s2", OIDs: []string{"1.1"}, Fetch: counterFetch(1)})
	s.Start(SessionSpec{ID: "s3", OIDs: []string{"1.1"}, Targets: []string{"a"}})
	if got := len(s.Running()); got != 0 {
		t.Errorf("Running() = %d, want 0", got)
	}
}
