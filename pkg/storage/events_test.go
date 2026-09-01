package storage

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"SnmpLens/pkg/events"
	"SnmpLens/pkg/notify"
)

func sampleEvent(kind, category, severity, source string) events.Event {
	return events.Event{
		Category: category,
		Kind:     kind,
		Severity: severity,
		Source:   source,
		TitleKey: "events.kind." + kind,
		Summary:  kind + " from " + source,
	}
}

func TestInsertEventAssignsIdentityAndDefaults(t *testing.T) {
	st := newTestStorage(t)

	saved, err := st.InsertEvent(sampleEvent(events.KindTrapReceived, events.CategoryTrap, "minor", "10.0.0.1"), `[{"oid":"1.2.3"}]`)
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if saved.ID == "" || saved.Seq == 0 || saved.Ts == "" {
		t.Fatalf("identity not filled in: %+v", saved)
	}
	if saved.State != events.StateOneshot {
		t.Errorf("state = %q, want oneshot", saved.State)
	}
	if saved.PayloadSize == 0 {
		t.Errorf("payloadSize not recorded")
	}

	body, err := st.EventPayload(saved.ID)
	if err != nil {
		t.Fatalf("EventPayload: %v", err)
	}
	if body != `[{"oid":"1.2.3"}]` {
		t.Errorf("payload = %q", body)
	}
}

func TestInsertEventRejectsUnknownCategory(t *testing.T) {
	st := newTestStorage(t)
	_, err := st.InsertEvent(sampleEvent("whatever", "not-a-category", "info", ""), "")
	if err == nil {
		t.Fatal("expected an error for an unknown category")
	}
}

// Severity is stored as an INTEGER precisely so that "at least major" is a
// numeric comparison. Stored as text, SQLite would order it alphabetically and
// 'critical' < 'info' < 'major' would return the wrong rows.
func TestMinSeverityFilterIsNumericNotLexicographic(t *testing.T) {
	st := newTestStorage(t)

	for _, sev := range []string{"info", "warning", "minor", "major", "critical"} {
		if _, err := st.InsertEvent(sampleEvent(events.KindSystemPollFailed, events.CategorySystem, sev, "10.0.0.1"), ""); err != nil {
			t.Fatalf("insert %s: %v", sev, err)
		}
	}

	page, err := st.QueryEvents(events.Filter{MinSeverity: "major"})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("minSeverity=major matched %d events, want 2 (major + critical)", page.Total)
	}
	for _, e := range page.Items {
		if e.Severity != "major" && e.Severity != "critical" {
			t.Errorf("unexpected severity %q in a >= major result", e.Severity)
		}
	}
}

// Keyset pagination, not OFFSET: the journal is written while it is read, so an
// OFFSET page would skip or repeat rows as new events arrive.
func TestQueryEventsKeysetPagination(t *testing.T) {
	st := newTestStorage(t)

	const total = 25
	for i := 0; i < total; i++ {
		e := sampleEvent(events.KindTrapReceived, events.CategoryTrap, "info", fmt.Sprintf("10.0.0.%d", i))
		if _, err := st.InsertEvent(e, ""); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	seen := map[string]bool{}
	cursor := int64(0)
	pages := 0
	for {
		page, err := st.QueryEvents(events.Filter{Limit: 10, BeforeSeq: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		if page.Total != total {
			t.Errorf("Total = %d, want %d", page.Total, total)
		}
		for _, e := range page.Items {
			if seen[e.ID] {
				t.Fatalf("event %s returned twice across pages", e.ID)
			}
			seen[e.ID] = true
		}
		pages++
		if page.NextCursor == 0 {
			break
		}
		cursor = page.NextCursor
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != total {
		t.Errorf("saw %d distinct events across %d pages, want %d", len(seen), pages, total)
	}
}

func TestQueryEventsNeverReturnsNilItems(t *testing.T) {
	st := newTestStorage(t)
	page, err := st.QueryEvents(events.Filter{})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	// A nil slice crosses the Wails bridge as JSON null, which the frontend
	// would then have to guard on every access.
	if page.Items == nil {
		t.Fatal("Items is nil; must be an empty slice")
	}
}

func TestAckAndCounts(t *testing.T) {
	st := newTestStorage(t)

	a, _ := st.InsertEvent(sampleEvent(events.KindTrapReceived, events.CategoryTrap, "minor", "10.0.0.1"), "")
	_, _ = st.InsertEvent(sampleEvent(events.KindThresholdOpened, events.CategoryThreshold, "major", "10.0.0.2"), "")

	counts, err := st.EventCounts()
	if err != nil {
		t.Fatalf("EventCounts: %v", err)
	}
	if counts.Unacked != 2 || counts.UnackedByCatego[events.CategoryTrap] != 1 {
		t.Fatalf("unexpected counts: %+v", counts)
	}

	if err := st.AckEvents([]string{a.ID}); err != nil {
		t.Fatalf("AckEvents: %v", err)
	}
	counts, _ = st.EventCounts()
	if counts.Unacked != 1 {
		t.Errorf("after ack: unacked = %d, want 1", counts.Unacked)
	}

	if err := st.AckAllEvents(events.Filter{}); err != nil {
		t.Fatalf("AckAllEvents: %v", err)
	}
	counts, _ = st.EventCounts()
	if counts.Unacked != 0 {
		t.Errorf("after ack-all: unacked = %d, want 0", counts.Unacked)
	}
}

// Retention is per category on purpose: traps arrive in bursts while system
// events trickle, and one shared FIFO cap would let a trap storm evict
// everything else.
func TestTrimEventsIsPerCategory(t *testing.T) {
	st := newTestStorage(t)

	// Shrink the cap for the test rather than inserting 20k rows.
	original := eventRetention[events.CategoryTrap]
	eventRetention[events.CategoryTrap] = 5
	t.Cleanup(func() { eventRetention[events.CategoryTrap] = original })

	for i := 0; i < 12; i++ {
		if _, err := st.InsertEvent(sampleEvent(events.KindTrapReceived, events.CategoryTrap, "info", "10.0.0.1"), ""); err != nil {
			t.Fatalf("insert trap %d: %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := st.InsertEvent(sampleEvent(events.KindSystemPollFailed, events.CategorySystem, "warning", "10.0.0.1"), ""); err != nil {
			t.Fatalf("insert system %d: %v", i, err)
		}
	}

	st.TrimEvents()

	traps, err := st.QueryEvents(events.Filter{Categories: []string{events.CategoryTrap}})
	if err != nil {
		t.Fatalf("query traps: %v", err)
	}
	if traps.Total != 5 {
		t.Errorf("traps kept = %d, want 5", traps.Total)
	}

	sys, err := st.QueryEvents(events.Filter{Categories: []string{events.CategorySystem}})
	if err != nil {
		t.Fatalf("query system: %v", err)
	}
	if sys.Total != 3 {
		t.Errorf("a trap burst evicted system events: kept %d, want 3", sys.Total)
	}
}

// Dwell state must survive a restart, otherwise an already-reported incident
// re-fires as new every time the app reopens.
func TestEpisodesRoundTrip(t *testing.T) {
	st := newTestStorage(t)
	now := time.Now().UTC().Format(time.RFC3339)

	ep := Episode{
		DedupKey:  "threshold|s1|10.0.0.1|1.3.6",
		Kind:      events.KindThresholdOpened,
		SessionID: "s1",
		Target:    "10.0.0.1",
		OID:       "1.3.6",
		FirstSeen: now,
		CorrID:    "corr-1",
	}
	if err := st.SaveEpisode(ep); err != nil {
		t.Fatalf("SaveEpisode: %v", err)
	}

	loaded, err := st.LoadEpisodes()
	if err != nil {
		t.Fatalf("LoadEpisodes: %v", err)
	}
	if len(loaded) != 1 || loaded[0].DedupKey != ep.DedupKey || loaded[0].Target != "10.0.0.1" {
		t.Fatalf("episode did not round-trip: %+v", loaded)
	}

	if err := st.DeleteEpisode(ep.DedupKey); err != nil {
		t.Fatalf("DeleteEpisode: %v", err)
	}
	loaded, _ = st.LoadEpisodes()
	if len(loaded) != 0 {
		t.Errorf("episode not closed: %+v", loaded)
	}
}

// A sink's credential must never reach the database. SaveSink marshals the
// whole SinkConfig into the row, so this pins the json:"-" tags: without them a
// SMTP password and a webhook token would sit in monitoring.db in the clear.
func TestSinkConfigNeverPersistsCredentials(t *testing.T) {
	st := newTestStorage(t)

	cfg := notify.SinkConfig{
		Name:    "NOC mail",
		Kind:    notify.SinkEmail,
		Enabled: true,
		Email: notify.EmailConfig{
			Host: "smtp.example.com", From: "a@example.com", To: []string{"b@example.com"},
			Username: "svc", Password: "TOP-SECRET-PASSWORD",
		},
		Webhook: notify.WebhookConfig{URL: "https://example.com", Token: "TOP-SECRET-TOKEN"},
		Syslog: notify.SyslogConfig{
			Address: "collector:6514", Protocol: notify.SyslogTLS,
			// The certificate is public and SHOULD be stored; the private key
			// must not be.
			ClientCert: "-----BEGIN CERTIFICATE-----PUBLIC-----END CERTIFICATE-----",
			ClientKey:  "TOP-SECRET-CLIENT-KEY",
		},
		Secret: "TOP-SECRET-TRANSPORT",
	}
	saved, err := st.SaveSink(cfg)
	if err != nil {
		t.Fatalf("SaveSink: %v", err)
	}

	var raw string
	if err := st.db.QueryRow(`SELECT config FROM notify_sinks WHERE id = ?`, saved.ID).Scan(&raw); err != nil {
		t.Fatalf("read back the stored config: %v", err)
	}
	for _, forbidden := range []string{"TOP-SECRET-PASSWORD", "TOP-SECRET-TOKEN", "TOP-SECRET-CLIENT-KEY"} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("credential %q was persisted into notify_sinks.config: %s", forbidden, raw)
		}
	}

	// Non-secret configuration must still survive, or the split would have
	// thrown out the baby with the bathwater.
	back, err := st.ListSinks()
	if err != nil {
		t.Fatalf("ListSinks: %v", err)
	}
	if len(back) != 1 || back[0].Email.Host != "smtp.example.com" || back[0].Email.Username != "svc" {
		t.Fatalf("sink configuration did not round-trip: %+v", back)
	}
	if back[0].Email.Password != "" || back[0].Webhook.Token != "" || back[0].Syslog.ClientKey != "" {
		t.Error("a credential came back out of the database")
	}
	// The mutual-TLS certificate is public: dropping it would break the sink
	// just as surely as leaking the key would compromise it.
	if !strings.Contains(back[0].Syslog.ClientCert, "PUBLIC") {
		t.Errorf("the client certificate was lost: %q", back[0].Syslog.ClientCert)
	}
}
