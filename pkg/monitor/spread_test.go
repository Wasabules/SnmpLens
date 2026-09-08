package monitor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Sessions resumed together must not all poll together.
//
// resumeActiveSessions starts every active session in a tight loop, and loop
// used to tick immediately. Measured before this existed: twenty sessions,
// first-tick spread of 0 ms — every device asked at once, every insert landing
// at once on a database with one writer and a four-connection pool.
func TestResumedSessionsDoNotAllPollAtOnce(t *testing.T) {
	var mu sync.Mutex
	firsts := map[string]time.Duration{}
	origin := time.Now()

	s := NewScheduler()
	s.Persist = func([]Point) {}

	const sessions = 20
	for i := 0; i < sessions; i++ {
		id := fmt.Sprintf("session-%d", i)
		s.Start(SessionSpec{
			ID: id, OIDs: []string{"1.1"}, Targets: []string{"a"},
			Interval: 30 * time.Second,
			Fetch: func(_ context.Context, oids []string, targets []string) []Reading {
				mu.Lock()
				if _, seen := firsts[id]; !seen {
					firsts[id] = time.Since(origin)
				}
				mu.Unlock()
				return []Reading{{Target: targets[0], OID: oids[0], Value: f64(1), SnmpType: "Counter32"}}
			},
		})
	}
	// Long enough for every offset in the window to have fired.
	time.Sleep(startSpreadWindow + 500*time.Millisecond)
	s.StopAll()

	mu.Lock()
	defer mu.Unlock()
	if len(firsts) != sessions {
		t.Fatalf("%d of %d sessions polled", len(firsts), sessions)
	}

	// How many landed in the same 100 ms slot? Before the spread, all of them.
	buckets := map[int]int{}
	worst := 0
	for _, d := range firsts {
		b := int(d / (100 * time.Millisecond))
		buckets[b]++
		if buckets[b] > worst {
			worst = buckets[b]
		}
	}
	if worst > sessions/2 {
		t.Errorf("%d of %d sessions polled inside the same 100 ms; the herd is not spread", worst, sessions)
	}
	if len(buckets) < 3 {
		t.Errorf("every session landed in %d slot(s); the offsets are not distributed", len(buckets))
	}
}

// The offset is derived, not drawn. A session lands in the same slot on every
// restart — which is what makes the spread stable rather than reshuffled, and
// what makes the test above an assertion rather than a hope.
func TestTheSpreadIsDeterministic(t *testing.T) {
	const iv = 30 * time.Second
	for _, id := range []string{"a", "session-1", "0195f3c2-1c4a-7e1b-9f3d-2a6b5c8d7e90"} {
		first := startSpread(id, iv)
		for i := 0; i < 5; i++ {
			if got := startSpread(id, iv); got != first {
				t.Errorf("%s: %v then %v; the offset must not move between calls", id, first, got)
			}
		}
		if first < 0 || first >= startSpreadWindow {
			t.Errorf("%s: offset %v is outside the window %v", id, first, startSpreadWindow)
		}
	}

	// Different ids land in different places. Not a hash-quality test — just
	// enough to catch an offset that ignores its input.
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		seen[startSpread(fmt.Sprintf("session-%d", i), iv)] = true
	}
	if len(seen) < 25 {
		t.Errorf("50 ids produced %d distinct offsets; the spread is barely spreading", len(seen))
	}
}

// A fast session must not have its first point pushed past its own period, or
// the spread reads as a missed poll rather than as a stagger.
func TestTheSpreadNeverExceedsTheInterval(t *testing.T) {
	for _, iv := range []time.Duration{minInterval, 300 * time.Millisecond, time.Second, time.Hour} {
		for i := 0; i < 30; i++ {
			d := startSpread(fmt.Sprintf("s%d", i), iv)
			if d >= iv {
				t.Errorf("interval %v: offset %v is a whole period or more", iv, d)
			}
			if d >= startSpreadWindow {
				t.Errorf("interval %v: offset %v exceeds the window", iv, d)
			}
		}
	}
}

// Stopping during the spread must return at once, not after the offset.
func TestStoppingDuringTheSpreadIsImmediate(t *testing.T) {
	s := NewScheduler()
	s.Persist = func([]Point) {}
	polled := make(chan struct{}, 1)

	s.Start(SessionSpec{
		ID: "slow-to-start", OIDs: []string{"1.1"}, Targets: []string{"a"},
		Interval: 30 * time.Second,
		Fetch: func(_ context.Context, oids []string, targets []string) []Reading {
			select {
			case polled <- struct{}{}:
			default:
			}
			return nil
		},
	})

	start := time.Now()
	s.StopAll()
	if d := time.Since(start); d > startSpreadWindow {
		t.Errorf("stopping took %v; it waited out the spread instead of cancelling it", d.Round(time.Millisecond))
	}
}
