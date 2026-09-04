package storage

import (
	"path/filepath"
	"testing"

	"SnmpLens/pkg/events"
)

// The watermark is what makes routing safe to do off the producer's goroutine:
// anything above it was journalled but may not have had its deliveries written,
// so it is replayed at startup.

func newEvent(summary string) events.Event {
	return events.Event{
		Ts: "2026-01-01T00:00:00Z", Category: events.CategoryTrap,
		Kind: events.KindTrapReceived, Severity: events.SevMinor.String(),
		State: events.StateOneshot, TitleKey: "events.kind.trap.received",
		Summary: summary,
	}
}

// Seeded at Init, to the newest event that exists — never to zero, and never
// lazily.
//
// With lazy creation, a crash before the router's first flush leaves the row
// absent, and the "absent means MAX(seq)" rule then skips exactly the events it
// was meant to protect: the two rules combine to defeat each other.
func TestTheWatermarkExistsBeforeAnythingRuns(t *testing.T) {
	st := newTestStorage(t)

	mark, err := st.RoutedThrough()
	if err != nil {
		t.Fatalf("RoutedThrough on a fresh database: %v", err)
	}
	if mark != 0 {
		t.Errorf("a fresh database starts at %d, want 0", mark)
	}

	// The row must physically be there, not conjured by the read.
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM notify_watermark`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d watermark rows, want exactly 1", n)
	}
}

// Opening a database that already has events must not offer to replay them all.
func TestOpeningAnExistingJournalDoesNotReplayIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "monitoring.db")

	st, err := Init(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := st.InsertEvent(newEvent("before"), ""); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate the upgrade: drop the watermark, then reopen.
	if _, err := st.db.Exec(`DELETE FROM notify_watermark`); err != nil {
		t.Fatal(err)
	}
	st.Close()

	st2, err := Init(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st2.Close() })

	mark, err := st2.RoutedThrough()
	if err != nil {
		t.Fatal(err)
	}
	pending, err := st2.EventsAfter(mark, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("%d events would be re-routed on upgrade; two weeks of history "+
			"would be delivered again", len(pending))
	}
}

// A watermark above the newest event is the signature of a replaced database.
// Trust the events, not the number, or everything below it is never replayed.
func TestAWatermarkPastTheEndIsCorrected(t *testing.T) {
	st := newTestStorage(t)
	for i := 0; i < 5; i++ {
		if _, err := st.InsertEvent(newEvent("e"), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.Exec(`UPDATE notify_watermark SET routed_through_seq = 9999`); err != nil {
		t.Fatal(err)
	}

	mark, err := st.RoutedThrough()
	if err != nil {
		t.Fatal(err)
	}
	if mark != 5 {
		t.Errorf("watermark = %d, want it pulled back to the newest event (5)", mark)
	}
}

// The deliveries and the watermark must be ONE transaction. A watermark that
// commits without them names events that are never replayed and never
// delivered — the exact loss it exists to prevent.
func TestTheWatermarkMovesOnlyWithTheDeliveries(t *testing.T) {
	st := newTestStorage(t)

	var groups []RoutedGroup
	for i := 0; i < 3; i++ {
		e, err := st.InsertEvent(newEvent("routed"), "")
		if err != nil {
			t.Fatal(err)
		}
		groups = append(groups, RoutedGroup{Event: e, SinkIDs: []string{"mail"}, Subject: "s", Body: "b"})
	}

	if err := st.EnqueueRouted(groups, groups[len(groups)-1].Event.Seq); err != nil {
		t.Fatal(err)
	}

	due, err := st.DueDeliveries(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 3 {
		t.Fatalf("%d deliveries queued, want 3", len(due))
	}
	mark, err := st.RoutedThrough()
	if err != nil {
		t.Fatal(err)
	}
	if mark != groups[2].Event.Seq {
		t.Errorf("watermark = %d, want %d", mark, groups[2].Event.Seq)
	}

	// A group naming an event whose snapshot cannot be written must take the
	// watermark down with it: nothing partial.
	before := mark
	bad := RoutedGroup{
		Event:   events.Event{ID: "x", Params: map[string]any{"cycle": make(chan int)}},
		SinkIDs: []string{"mail"},
	}
	if err := st.EnqueueRouted([]RoutedGroup{bad}, before+50); err == nil {
		t.Error("an unserialisable event was accepted")
	}
	after, _ := st.RoutedThrough()
	if after != before {
		t.Errorf("the watermark moved from %d to %d despite the failure", before, after)
	}
}

// A flush finishing out of order must not un-route what a later one accounted
// for.
func TestTheWatermarkNeverGoesBackwards(t *testing.T) {
	st := newTestStorage(t)

	// Real seqs, so the "past the end" guard is not what is being measured.
	var seqs []int64
	for i := 0; i < 130; i++ {
		e, err := st.InsertEvent(newEvent("e"), "")
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, e.Seq)
	}
	high, low := seqs[119], seqs[99]

	if err := st.EnqueueRouted(nil, high); err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueRouted(nil, low); err != nil {
		t.Fatal(err)
	}

	mark, err := st.RoutedThrough()
	if err != nil {
		t.Fatal(err)
	}
	if mark != high {
		t.Errorf("watermark = %d after a late batch reported %d; want it to stay at %d",
			mark, low, high)
	}
}

// Replay must return what was journalled, in order, with the fields routing
// needs — a rule matches on category, severity, source and OID.
func TestEventsAfterReturnsWhatRoutingNeeds(t *testing.T) {
	st := newTestStorage(t)

	want := newEvent("over the line")
	want.Source = "10.0.0.1"
	want.OID = ".1.3.6.1.6.3.1.1.5.3"
	want.Severity = events.SevMajor.String()
	saved, err := st.InsertEvent(want, "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := st.EventsAfter(saved.Seq-1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%d events returned, want 1", len(got))
	}
	e := got[0]
	if e.ID != saved.ID || e.Category != want.Category || e.Severity != want.Severity ||
		e.Source != want.Source || e.OID != want.OID || e.Summary != want.Summary {
		t.Errorf("the replayed event lost fields a rule matches on: %+v", e)
	}
	if e.Seq != saved.Seq {
		t.Errorf("seq = %d, want %d — the watermark is computed from this", e.Seq, saved.Seq)
	}

	// And nothing at or below the mark comes back.
	none, err := st.EventsAfter(saved.Seq, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("%d events returned above the newest seq", len(none))
	}
}

// Replay is safe to repeat: the outbox's UNIQUE(event_id, sink_id) plus INSERT
// OR IGNORE is what makes a crash mid-flush harmless.
func TestReplayingTheSameEventQueuesNothingTwice(t *testing.T) {
	st := newTestStorage(t)

	e, err := st.InsertEvent(newEvent("once"), "")
	if err != nil {
		t.Fatal(err)
	}
	g := []RoutedGroup{{Event: e, SinkIDs: []string{"mail", "log"}, Subject: "s", Body: "b"}}

	for i := 0; i < 3; i++ {
		if err := st.EnqueueRouted(g, e.Seq); err != nil {
			t.Fatal(err)
		}
	}

	due, err := st.DueDeliveries(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 2 {
		t.Errorf("%d deliveries after routing the same event three times, want 2", len(due))
	}
}
