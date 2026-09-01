package notify

import (
	"errors"
	"sync"
	"testing"
	"time"

	"SnmpLens/pkg/events"
)

type fakeQueue struct {
	mu      sync.Mutex
	pending []Queued
	sent    []int64
	failed  []struct {
		id   int64
		msg  string
		dead bool
	}
}

func (q *fakeQueue) DueDeliveries(limit int) ([]Queued, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.pending
	q.pending = nil
	return out, nil
}

func (q *fakeQueue) MarkSent(id int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sent = append(q.sent, id)
	return nil
}

func (q *fakeQueue) MarkFailed(id int64, msg string, next time.Time, dead bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed = append(q.failed, struct {
		id   int64
		msg  string
		dead bool
	}{id, msg, dead})
	return nil
}

type fakeSink struct {
	err  error
	sent int
	mu   sync.Mutex
}

func (f *fakeSink) Send(events.Event, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent++
	return f.err
}

func (f *fakeSink) Describe() string { return "fake" }

func queued(id int64, attempts int) Queued {
	return Queued{ID: id, SinkID: "s1", Attempts: attempts, Subject: "s", Body: "b",
		Event: events.Event{ID: "e1", Kind: events.KindTrapReceived}}
}

func TestDrainMarksSuccessfulDeliverySent(t *testing.T) {
	q := &fakeQueue{pending: []Queued{queued(1, 0)}}
	sink := &fakeSink{}
	d := NewDispatcher(q, func(string) (Sink, bool) { return sink, true }, time.Hour)

	d.Drain()

	if len(q.sent) != 1 || q.sent[0] != 1 {
		t.Fatalf("delivery not marked sent: %v", q.sent)
	}
	if sink.sent != 1 {
		t.Errorf("sink called %d times, want 1", sink.sent)
	}
}

// A transient failure must be retried, not discarded.
func TestDrainRetriesTransientFailure(t *testing.T) {
	q := &fakeQueue{pending: []Queued{queued(2, 0)}}
	sink := &fakeSink{err: errors.New("connection refused")}
	d := NewDispatcher(q, func(string) (Sink, bool) { return sink, true }, time.Hour)

	d.Drain()

	if len(q.failed) != 1 {
		t.Fatalf("failure not recorded: %+v", q.failed)
	}
	if q.failed[0].dead {
		t.Error("a transient failure must stay pending, not become a dead letter")
	}
}

// Retrying a bad password six times can lock an account, so a permanent failure
// gives up immediately.
func TestDrainDeadLettersPermanentFailure(t *testing.T) {
	q := &fakeQueue{pending: []Queued{queued(3, 0)}}
	sink := &fakeSink{err: errors.New("smtp auth: 535 authentication failed")}
	d := NewDispatcher(q, func(string) (Sink, bool) { return sink, true }, time.Hour)

	d.Drain()

	if len(q.failed) != 1 || !q.failed[0].dead {
		t.Fatalf("permanent failure was not dead-lettered: %+v", q.failed)
	}
}

func TestDrainDeadLettersAfterMaxAttempts(t *testing.T) {
	q := &fakeQueue{pending: []Queued{queued(4, MaxAttempts-1)}}
	sink := &fakeSink{err: errors.New("connection refused")}
	d := NewDispatcher(q, func(string) (Sink, bool) { return sink, true }, time.Hour)

	d.Drain()

	if len(q.failed) != 1 || !q.failed[0].dead {
		t.Fatalf("attempt %d should exhaust the budget: %+v", MaxAttempts, q.failed)
	}
}

// A sink deleted after the event was queued must not be retried forever
// against nothing.
func TestDrainDeadLettersMissingSink(t *testing.T) {
	q := &fakeQueue{pending: []Queued{queued(5, 0)}}
	d := NewDispatcher(q, func(string) (Sink, bool) { return nil, false }, time.Hour)

	d.Drain()

	if len(q.failed) != 1 || !q.failed[0].dead {
		t.Fatalf("missing sink not dead-lettered: %+v", q.failed)
	}
	if !contains(q.failed[0].msg, "no longer exists") {
		t.Errorf("unhelpful error message: %q", q.failed[0].msg)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// Backoff must grow but stay bounded: a next attempt a day away is
// indistinguishable from a lost notification.
func TestBackoffGrowsAndIsCapped(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 1; attempt <= 12; attempt++ {
		d := Backoff(attempt)
		if d < prev {
			t.Errorf("attempt %d: backoff shrank from %v to %v", attempt, prev, d)
		}
		if d > 15*time.Minute {
			t.Errorf("attempt %d: backoff %v exceeds the cap", attempt, d)
		}
		prev = d
	}
	if Backoff(0) <= 0 {
		t.Error("backoff must be positive even for a nonsensical attempt number")
	}
}

func TestPermanentClassification(t *testing.T) {
	if Permanent(nil) {
		t.Error("nil is not a permanent failure")
	}
	if !Permanent(errors.New("webhook returned 401 Unauthorized")) {
		t.Error("401 should not be retried")
	}
	if Permanent(errors.New("webhook returned 503 Service Unavailable")) {
		t.Error("503 is transient and must be retried")
	}
	if !Permanent(errors.New("webhook URL is empty")) {
		t.Error("a misconfiguration cannot fix itself by retrying")
	}
}

// The ticker must not stack overlapping passes retrying the same rows when a
// relay is slow.
func TestDrainIsNotReentrant(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	slow := &blockingSink{started: started, release: release}

	q := &fakeQueue{pending: []Queued{queued(6, 0)}}
	d := NewDispatcher(q, func(string) (Sink, bool) { return slow, true }, time.Hour)

	go d.Drain()
	<-started

	// A second drain while the first is in flight must return immediately.
	done := make(chan struct{})
	go func() { d.Drain(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a concurrent Drain blocked instead of returning")
	}

	close(release)
}

type blockingSink struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingSink) Send(events.Event, string, string) error {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return nil
}

func (b *blockingSink) Describe() string { return "blocking" }
