package storage

import (
	"fmt"
	"testing"
	"time"

	"SnmpLens/pkg/events"
)

// deadRows counts the given-up deliveries left in the outbox.
func deadRows(t *testing.T, st *Storage) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM notify_outbox WHERE state = 'dead'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// queueDead puts n given-up deliveries in the outbox, oldest first.
func queueDead(t *testing.T, st *Storage, sinkID string, n int) []int64 {
	t.Helper()
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		e := events.Event{
			ID: fmt.Sprintf("evt-%s-%d", sinkID, i), Category: events.CategoryTrap,
			Kind: events.KindTrapReceived, Severity: "minor",
			Ts: time.Now().UTC().Format(time.RFC3339), Summary: "trap",
		}
		if err := st.EnqueueDeliveries(e, []string{sinkID}, "s", "b"); err != nil {
			t.Fatal(err)
		}
		due, err := st.DueDeliveries(1)
		if err != nil {
			t.Fatal(err)
		}
		if len(due) == 0 {
			t.Fatalf("nothing due after queueing %d", i)
		}
		ids = append(ids, due[0].ID)
		if err := st.MarkFailed(due[0].ID, "unreachable", time.Now().Add(time.Hour), true); err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

// The outbox was the one table in the database with no ceiling at all.
//
// Retention deliberately does not cover dead rows — a dead letter is the only
// record that a notification never arrived — and nothing else bounded them. A
// trap flood routed to an unreachable collector produces one row per event per
// sink, each holding the complete event JSON plus the rendered subject and
// body, and every one of them ends up dead.
func TestTheDeadLetterListHasACeiling(t *testing.T) {
	st := newTestStorage(t)

	queueDead(t, st, "sink-a", 12)
	if got := deadRows(t, st); got != 12 {
		t.Fatalf("%d dead rows before trimming, want 12", got)
	}

	removed, err := st.TrimDeadLetters(5)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 7 {
		t.Errorf("removed %d rows, want 7", removed)
	}
	if got := deadRows(t, st); got != 5 {
		t.Errorf("%d dead rows kept, want 5", got)
	}

	// The NEWEST are the ones kept: an operator reading the delivery log wants
	// what just failed, not what failed first.
	rows, err := st.ListDeliveries("dead", 100)
	if err != nil {
		t.Fatal(err)
	}
	// Compared as NUMBERS: "evt-sink-a-10" sorts before "evt-sink-a-7" as a
	// string, which is how the first version of this assertion managed to fail
	// on correct behaviour.
	kept := map[int]bool{}
	for _, r := range rows {
		var i int
		if _, err := fmt.Sscanf(r.EventID, "evt-sink-a-%d", &i); err != nil {
			t.Fatalf("unexpected event id %q", r.EventID)
		}
		kept[i] = true
	}
	for i := 7; i < 12; i++ {
		if !kept[i] {
			t.Errorf("dead letter %d was discarded although it is among the newest five", i)
		}
	}
	for i := 0; i < 7; i++ {
		if kept[i] {
			t.Errorf("dead letter %d survived although seven newer ones exist", i)
		}
	}

	// Idempotent: running it again with nothing to do removes nothing.
	if removed, err := st.TrimDeadLetters(5); err != nil || removed != 0 {
		t.Errorf("a second trim removed %d rows (err %v)", removed, err)
	}
	// And a ceiling above the row count is a no-op, not a truncation.
	if removed, err := st.TrimDeadLetters(1000); err != nil || removed != 0 {
		t.Errorf("a ceiling above the count removed %d rows (err %v)", removed, err)
	}
}

// A pending delivery is an alert still owed, and a delivered one is covered by
// its own retention window. Neither may be caught by the dead-letter ceiling.
func TestTheCeilingTouchesOnlyDeadRows(t *testing.T) {
	st := newTestStorage(t)

	// One pending, one sent, several dead.
	e := events.Event{ID: "evt-pending", Category: events.CategoryTrap,
		Kind: events.KindTrapReceived, Severity: "minor",
		Ts: time.Now().UTC().Format(time.RFC3339), Summary: "trap"}
	if err := st.EnqueueDeliveries(e, []string{"sink-b"}, "s", "b"); err != nil {
		t.Fatal(err)
	}
	due, err := st.DueDeliveries(1)
	if err != nil || len(due) == 0 {
		t.Fatalf("nothing due: %v", err)
	}
	sent := due[0].ID
	if err := st.MarkSent(sent); err != nil {
		t.Fatal(err)
	}

	e2 := e
	e2.ID = "evt-still-owed"
	if err := st.EnqueueDeliveries(e2, []string{"sink-b"}, "s", "b"); err != nil {
		t.Fatal(err)
	}
	queueDead(t, st, "sink-c", 6)

	if _, err := st.TrimDeadLetters(2); err != nil {
		t.Fatal(err)
	}

	var pending, sentLeft int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM notify_outbox WHERE state='pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM notify_outbox WHERE state='sent'`).Scan(&sentLeft); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Errorf("%d pending rows after the trim, want 1: an alert still owed was discarded", pending)
	}
	if sentLeft != 1 {
		t.Errorf("%d sent rows after the trim, want 1", sentLeft)
	}
	if got := deadRows(t, st); got != 2 {
		t.Errorf("%d dead rows, want 2", got)
	}
}

// A failure is a write like any other, and it was the one write path that did
// not say so — so the trimmer stopped exactly when the outbox was growing
// fastest. A destination nothing can reach produces failures and no MarkSent
// at all, so nothing ever ran retention again.
func TestAFailedDeliveryDrivesTheTrimmer(t *testing.T) {
	st := newTestStorage(t)

	before := st.outboxWriteCount()
	queueDead(t, st, "sink-d", 3)
	after := st.outboxWriteCount()

	if after-before != 3 {
		t.Errorf("MarkFailed recorded %d outbox writes for 3 failures; the retention "+
			"cadence never advances while a sink is down", after-before)
	}
}

// outboxWriteCount reads the cadence counter under its lock.
func (s *Storage) outboxWriteCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outboxWrites
}
