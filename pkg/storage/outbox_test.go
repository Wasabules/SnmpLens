package storage

import (
	"fmt"
	"testing"
	"time"

	"SnmpLens/pkg/events"
	"SnmpLens/pkg/notify"
)

// The outbox is what makes a notification survive a closed window, a crash and
// a relay that is down for an hour. Every function below was at 0% coverage:
// the whole durable half of the alerting path was verified only by the app
// happening to work.

func alertEvent(id string) events.Event {
	return events.Event{
		ID:       id,
		Ts:       "2026-01-01T00:00:00Z",
		Category: events.CategoryThreshold,
		Kind:     events.KindThresholdOpened,
		Severity: events.SevMajor.String(),
		Source:   "10.0.0.1",
		Summary:  "over the line",
	}
}

func queueOne(t *testing.T, st *Storage, eventID, sinkID string) notify.Queued {
	t.Helper()
	if err := st.EnqueueDeliveries(alertEvent(eventID), []string{sinkID}, "subject", "body"); err != nil {
		t.Fatalf("EnqueueDeliveries: %v", err)
	}
	due, err := st.DueDeliveries(50)
	if err != nil {
		t.Fatalf("DueDeliveries: %v", err)
	}
	for _, q := range due {
		if q.SinkID == sinkID {
			return q
		}
	}
	t.Fatalf("the delivery just queued for %q is not due", sinkID)
	return notify.Queued{}
}

// What is queued must come back out intact: the dispatcher never reads the
// journal, so this snapshot IS the alert. A field lost here is a field missing
// from the mail at 03:00.
func TestAQueuedDeliveryComesBackWhole(t *testing.T) {
	st := newTestStorage(t)

	e := alertEvent("evt-1")
	if err := st.EnqueueDeliveries(e, []string{"sink-a"}, "SUBJECT", "BODY"); err != nil {
		t.Fatal(err)
	}

	due, err := st.DueDeliveries(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("%d deliveries due, want 1", len(due))
	}
	q := due[0]
	if q.SinkID != "sink-a" || q.Subject != "SUBJECT" || q.Body != "BODY" {
		t.Errorf("delivery came back as %+v", q)
	}
	if q.Attempts != 0 {
		t.Errorf("a new delivery already has %d attempts", q.Attempts)
	}
	if q.Event.Summary != e.Summary || q.Event.Source != e.Source ||
		q.Event.Severity != e.Severity || q.Event.Kind != e.Kind {
		t.Errorf("the event snapshot lost fields: %+v", q.Event)
	}
}

// One event, several destinations: one row each, and each is due.
func TestEnqueueQueuesOneRowPerSink(t *testing.T) {
	st := newTestStorage(t)

	if err := st.EnqueueDeliveries(alertEvent("evt-2"),
		[]string{"mail", "syslog", "hook"}, "s", "b"); err != nil {
		t.Fatal(err)
	}
	due, err := st.DueDeliveries(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 3 {
		t.Fatalf("%d deliveries due, want one per sink", len(due))
	}
}

// Routing the same event twice must not send it twice. The UNIQUE(event_id,
// sink_id) plus INSERT OR IGNORE is the guard, and it is the only one.
func TestEnqueueingTheSameEventTwiceQueuesItOnce(t *testing.T) {
	st := newTestStorage(t)

	for i := 0; i < 3; i++ {
		if err := st.EnqueueDeliveries(alertEvent("evt-3"), []string{"mail"}, "s", "b"); err != nil {
			t.Fatal(err)
		}
	}
	due, err := st.DueDeliveries(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Errorf("%d deliveries queued for one event and one sink", len(due))
	}
}

// A delivery scheduled for the future must not come back. Without this the
// backoff means nothing and a relay that is down is hammered every 15 seconds.
func TestADeliveryScheduledForLaterIsNotDue(t *testing.T) {
	st := newTestStorage(t)
	q := queueOne(t, st, "evt-4", "mail")

	if err := st.MarkFailed(q.ID, "connection refused", time.Now().Add(10*time.Minute), false); err != nil {
		t.Fatal(err)
	}

	due, err := st.DueDeliveries(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("a delivery backed off for 10 minutes came back immediately: %+v", due)
	}

	// And it IS still pending — backing off must not lose it.
	pending, err := st.ListDeliveries("pending", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("%d pending rows, want the one backing off", len(pending))
	}
	if pending[0].Attempts != 1 {
		t.Errorf("attempts = %d after one failure, want 1", pending[0].Attempts)
	}
	if pending[0].LastError == "" {
		t.Error("the failure was not recorded, so the operator cannot see why")
	}
}

// The backoff having elapsed, it comes back.
func TestADeliveryWhoseBackoffHasElapsedIsDueAgain(t *testing.T) {
	st := newTestStorage(t)
	q := queueOne(t, st, "evt-5", "mail")

	if err := st.MarkFailed(q.ID, "connection refused", time.Now().Add(-time.Minute), false); err != nil {
		t.Fatal(err)
	}
	due, err := st.DueDeliveries(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("%d due, want the retry", len(due))
	}
	if due[0].Attempts != 1 {
		t.Errorf("the retry came back with %d attempts; the dispatcher counts on this "+
			"to know when to give up", due[0].Attempts)
	}
}

// A dead letter is never delivered again — but it is kept, because it is the
// only record that an alert never arrived.
func TestADeadLetterIsNeverDueAndIsKept(t *testing.T) {
	st := newTestStorage(t)
	q := queueOne(t, st, "evt-6", "mail")

	if err := st.MarkFailed(q.ID, "535 authentication failed", time.Now().Add(-time.Hour), true); err != nil {
		t.Fatal(err)
	}
	due, err := st.DueDeliveries(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("a dead letter was handed back for delivery: %+v", due)
	}
	dead, err := st.ListDeliveries("dead", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 1 {
		t.Fatalf("%d dead letters, want the one — it is the only trace of a lost alert", len(dead))
	}
	if dead[0].LastError == "" {
		t.Error("a dead letter with no reason tells the operator nothing")
	}
}

// A sent delivery is not sent twice.
func TestMarkSentTakesItOutOfTheQueue(t *testing.T) {
	st := newTestStorage(t)
	q := queueOne(t, st, "evt-7", "mail")

	if err := st.MarkSent(q.ID); err != nil {
		t.Fatal(err)
	}
	due, err := st.DueDeliveries(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("a sent delivery is still queued: %+v", due)
	}
	sent, err := st.ListDeliveries("sent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("%d sent rows, want 1", len(sent))
	}
	if sent[0].LastError != "" {
		t.Errorf("a stale error survived a successful send: %q", sent[0].LastError)
	}
}

// Retrying a dead letter must give it the FULL retry policy back, not one
// attempt. attempts was left at MaxAttempts, and Drain gives up when
// attempts+1 >= MaxAttempts — so a retried delivery died again on its first
// failure, with no backoff and no second try, which is not what asking to retry
// something means.
func TestRetryingADeadLetterRestoresItsAttempts(t *testing.T) {
	st := newTestStorage(t)
	q := queueOne(t, st, "evt-8", "mail")

	for i := 0; i < notify.MaxAttempts; i++ {
		dead := i == notify.MaxAttempts-1
		if err := st.MarkFailed(q.ID, "relay down", time.Now().Add(-time.Hour), dead); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.RetryDelivery(q.ID); err != nil {
		t.Fatal(err)
	}

	due, err := st.DueDeliveries(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("the retried delivery is not due: %d rows", len(due))
	}
	if due[0].Attempts != 0 {
		t.Errorf("attempts = %d after a retry; Drain gives up at %d, so this delivery "+
			"dies again on its first failure", due[0].Attempts, notify.MaxAttempts)
	}
	rows, err := st.ListDeliveries("pending", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 1 && rows[0].LastError != "" {
		t.Errorf("the old failure is still shown against a delivery being retried: %q",
			rows[0].LastError)
	}
}

// Trimming keeps what an operator still needs. Deleting a pending row loses the
// alert outright; deleting a dead letter destroys the only evidence one was
// lost.
func TestTrimOutboxRemovesOnlyOldSentRows(t *testing.T) {
	st := newTestStorage(t)

	sent := queueOne(t, st, "evt-old-sent", "mail")
	if err := st.MarkSent(sent.ID); err != nil {
		t.Fatal(err)
	}
	deadRow := queueOne(t, st, "evt-old-dead", "mail")
	if err := st.MarkFailed(deadRow.ID, "gone", time.Now(), true); err != nil {
		t.Fatal(err)
	}
	queueOne(t, st, "evt-old-pending", "mail")

	// Everything was created now, so a window of an hour keeps it all.
	if err := st.TrimOutbox(time.Hour); err != nil {
		t.Fatal(err)
	}
	all, err := st.ListDeliveries("", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("trimming inside the retention window removed rows: %d left of 3", len(all))
	}

	// Age every row by a week, the way real retention works — the cutoff is
	// compared against created_at, which is written at second precision, so a
	// zero window would not put a row created this same second past it.
	if _, err := st.db.Exec(`UPDATE notify_outbox SET created_at = ?`,
		time.Now().Add(-7*24*time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := st.TrimOutbox(time.Hour); err != nil {
		t.Fatal(err)
	}
	all, err = st.ListDeliveries("", 50)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]int{}
	for _, d := range all {
		states[d.State]++
	}
	if states["sent"] != 0 {
		t.Errorf("%d sent rows survived the trim", states["sent"])
	}
	if states["dead"] != 1 {
		t.Error("a dead letter was trimmed: the only record that an alert was lost")
	}
	if states["pending"] != 1 {
		t.Error("a pending delivery was trimmed: the alert is simply gone")
	}
}

// The limit is honoured, and the oldest go first — otherwise a backlog starves
// the deliveries that have been waiting longest.
func TestDueDeliveriesRespectsTheLimitOldestFirst(t *testing.T) {
	st := newTestStorage(t)

	for i := 0; i < 5; i++ {
		q := queueOne(t, st, "evt-lim", fmt.Sprintf("sink-%d", i))
		// Stagger the retry times so the ordering is defined: sink-0 has been
		// waiting longest.
		if err := st.MarkFailed(q.ID, "later",
			time.Now().Add(-time.Duration(10-i)*time.Minute), false); err != nil {
			t.Fatal(err)
		}
	}

	due, err := st.DueDeliveries(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 2 {
		t.Fatalf("%d deliveries returned for a limit of 2", len(due))
	}
	if due[0].SinkID != "sink-0" || due[1].SinkID != "sink-1" {
		t.Errorf("the longest-waiting deliveries were not served first: %s, %s",
			due[0].SinkID, due[1].SinkID)
	}
}

func TestDeleteSinkAndDeleteRouteRemoveTheirRows(t *testing.T) {
	st := newTestStorage(t)

	sink, err := st.SaveSink(notify.SinkConfig{Name: "NOC", Kind: notify.SinkWebhook, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	route, err := st.SaveRoute(notify.Route{Name: "all", Enabled: true, SinkIDs: []string{sink.ID}})
	if err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteSink(sink.ID); err != nil {
		t.Fatal(err)
	}
	sinks, err := st.ListSinks()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sinks {
		if s.ID == sink.ID {
			t.Error("the destination is still listed after being deleted")
		}
	}

	if err := st.DeleteRoute(route.ID); err != nil {
		t.Fatal(err)
	}
	routes, err := st.ListRoutes()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range routes {
		if r.ID == route.ID {
			t.Error("the rule is still listed after being deleted")
		}
	}

	// Deleting something that is not there is not an error: two windows, or a
	// double click, must not produce a failure the operator has to interpret.
	if err := st.DeleteSink(sink.ID); err != nil {
		t.Errorf("deleting an absent destination failed: %v", err)
	}
	if err := st.DeleteRoute(route.ID); err != nil {
		t.Errorf("deleting an absent rule failed: %v", err)
	}
}
