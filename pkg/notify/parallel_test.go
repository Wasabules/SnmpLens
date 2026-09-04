package notify

import (
	"sync/atomic"
	"testing"
	"time"

	"SnmpLens/pkg/events"
)

// One unreachable destination must not hold up the others.
//
// Drain sent a batch of 20 in a single goroutine, so the worst case behind one
// unreachable SMTP relay was 20 x the 20-second default timeout — around seven
// minutes of blocked outbox, during which a critical trap routed to a perfectly
// healthy webhook waits. Worse, the Wake() that trap triggered is dropped by
// TryLock, so it then waits for the 15-second ticker on top.
//
// Measured before the fix, with two deliveries and a sink that takes 1.5s: the
// healthy sink's delivery went out 1.5s late and the drain took 1.5s.
func TestASlowSinkDoesNotHoldUpAnother(t *testing.T) {
	const slowFor = 400 * time.Millisecond

	var fastAt atomic.Int64
	start := time.Now()

	slow := sinkFunc(func(events.Event, string, string) error {
		time.Sleep(slowFor)
		return nil
	})
	fast := sinkFunc(func(events.Event, string, string) error {
		fastAt.Store(int64(time.Since(start)))
		return nil
	})

	q := &scriptedQueue{batch: []Queued{
		// The slow one is FIRST, which is the case that used to block.
		{ID: 1, SinkID: "slow", Event: events.Event{ID: "e1"}},
		{ID: 2, SinkID: "fast", Event: events.Event{ID: "e2"}},
	}}

	d := NewDispatcher(q, func(id string) (Sink, bool) {
		if id == "slow" {
			return slow, true
		}
		return fast, true
	}, time.Hour)

	d.Drain()

	waited := time.Duration(fastAt.Load())
	if waited >= slowFor {
		t.Errorf("the healthy destination waited %v behind the slow one; with the "+
			"default 20s email timeout and a full batch that is minutes", waited)
	}
	if len(q.sent) != 2 {
		t.Errorf("%d of 2 deliveries were sent", len(q.sent))
	}
}

// Deliveries to the SAME destination stay serial: twenty parallel SMTP
// conversations with one relay is how a sender gets rate-limited or blocked.
func TestDeliveriesToOneSinkStaySerial(t *testing.T) {
	var concurrent, peak atomic.Int32

	sink := sinkFunc(func(events.Event, string, string) error {
		n := concurrent.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		concurrent.Add(-1)
		return nil
	})

	batch := make([]Queued, 6)
	for i := range batch {
		batch[i] = Queued{ID: int64(i + 1), SinkID: "relay", Event: events.Event{ID: "e"}}
	}
	q := &scriptedQueue{batch: batch}
	d := NewDispatcher(q, func(string) (Sink, bool) { return sink, true }, time.Hour)

	d.Drain()

	if p := peak.Load(); p != 1 {
		t.Errorf("%d deliveries to one relay ran at once; they must be serial", p)
	}
	if len(q.sent) != len(batch) {
		t.Errorf("%d of %d deliveries were sent", len(q.sent), len(batch))
	}
}

// Every delivery in the batch is accounted for, whichever way it went, and the
// bookkeeping is safe to do from several goroutines.
func TestEveryDeliveryInAParallelBatchIsRecorded(t *testing.T) {
	var failing = sinkFunc(func(events.Event, string, string) error {
		return &HTTPStatusError{Code: 500, Msg: "webhook returned 500"}
	})
	ok := sinkFunc(func(events.Event, string, string) error { return nil })

	batch := make([]Queued, 12)
	for i := range batch {
		sinkID := "ok"
		if i%2 == 0 {
			sinkID = "bad"
		}
		batch[i] = Queued{ID: int64(i + 1), SinkID: sinkID, Event: events.Event{ID: "e"}}
	}
	q := &scriptedQueue{batch: batch}

	var results atomic.Int32
	d := NewDispatcher(q, func(id string) (Sink, bool) {
		if id == "bad" {
			return failing, true
		}
		return ok, true
	}, time.Hour)
	d.OnResult = func(Queued, error, bool) { results.Add(1) }

	d.Drain()

	if got := len(q.sent) + len(q.failed); got != len(batch) {
		t.Errorf("%d of %d deliveries were accounted for (%d sent, %d failed)",
			got, len(batch), len(q.sent), len(q.failed))
	}
	if len(q.sent) != 6 || len(q.failed) != 6 {
		t.Errorf("sent %d, failed %d; want 6 and 6", len(q.sent), len(q.failed))
	}
}

// A panic in one sink must not take the drain — or the app — down with it. This
// runs on a background goroutine, where an unrecovered panic ends the process.
func TestAPanickingSinkDoesNotKillTheDrain(t *testing.T) {
	var delivered atomic.Int32

	boom := sinkFunc(func(events.Event, string, string) error { panic("sink exploded") })
	ok := sinkFunc(func(events.Event, string, string) error {
		delivered.Add(1)
		return nil
	})

	q := &scriptedQueue{batch: []Queued{
		{ID: 1, SinkID: "boom", Event: events.Event{ID: "e1"}},
		{ID: 2, SinkID: "ok", Event: events.Event{ID: "e2"}},
	}}
	d := NewDispatcher(q, func(id string) (Sink, bool) {
		if id == "boom" {
			return boom, true
		}
		return ok, true
	}, time.Hour)

	d.Drain()

	if delivered.Load() != 1 {
		t.Error("a panic in one destination stopped the others being delivered")
	}
	if len(q.failed) != 1 {
		t.Errorf("the panicking delivery was not recorded as failed: %+v", q.failed)
	}
}

// scriptedQueue records what the drain did, from any goroutine.
type recordedFailure struct {
	id   int64
	msg  string
	dead bool
}
