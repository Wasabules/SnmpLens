package storage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnmpLens/pkg/preset"
)

// The snapshot survives the round trip, and it is a SNAPSHOT: what the session
// draws is what the file said WHEN IT WAS BOUND, not what the file says now.
func TestASessionCarriesItsPresetSnapshot(t *testing.T) {
	st := newTestStorage(t)
	now := time.Now().UTC().Format(time.RFC3339)

	bound := &SessionPreset{
		File: "cisco.json", Name: "Cisco Catalyst", FormatVersion: 1, BoundAt: now,
		Widgets: []preset.Widget{
			{Kind: preset.KindValue, Title: "Uptime", OIDs: []string{"1.3.6.1.2.1.1.3.0"}},
			{Kind: preset.KindGrid, Title: "Ports", OIDs: []string{"1.3.6.1.2.1.2.2.1.8.1"},
				Labels: map[string]string{"1": "up", "2": "down"}},
		},
	}
	id, err := st.CreateSession("Cisco Catalyst", "1.3.6.1.2.1.1.3.0,1.3.6.1.2.1.2.2.1.8.1",
		[]string{"10.0.0.1"}, 30000, "v2c", now, nil, &SessionConn{Port: 161}, bound)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sessions, err := st.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Preset == nil {
		t.Fatalf("the snapshot did not round-trip: %+v", sessions)
	}
	got := sessions[0].Preset
	if got.File != "cisco.json" || got.FormatVersion != 1 || len(got.Widgets) != 2 {
		t.Fatalf("snapshot changed shape: %+v", got)
	}
	if got.Widgets[1].Kind != preset.KindGrid || got.Widgets[1].Labels["2"] != "down" {
		t.Errorf("the widget layout did not survive: %+v", got.Widgets[1])
	}

	// What to POLL lives in the session's own columns, not in the snapshot. The
	// poll clock never opens this blob, so a session whose layout cannot be
	// decoded keeps polling and loses only its dashboard.
	if sessions[0].OID != "1.3.6.1.2.1.1.3.0,1.3.6.1.2.1.2.2.1.8.1" || sessions[0].IntervalMs != 30000 {
		t.Errorf("what to poll was not materialised: oid=%q interval=%d", sessions[0].OID, sessions[0].IntervalMs)
	}

	// And a preset carries no credential, so nothing secret may reach the blob.
	var raw sql.NullString
	if err := st.db.QueryRow(`SELECT preset FROM sessions WHERE id = ?`, id).Scan(&raw); err != nil {
		t.Fatalf("read the stored snapshot: %v", err)
	}
	for _, forbidden := range []string{"community", "authpass", "privpass", "password", "secret"} {
		if strings.Contains(strings.ToLower(raw.String), forbidden) {
			t.Errorf("the snapshot carries %q into the database: %s", forbidden, raw.String)
		}
	}
}

// A session nobody bound from a preset reports nil, rather than an empty
// snapshot that reads as "a preset with no widgets".
func TestASessionWithoutAPresetReportsNil(t *testing.T) {
	st := newTestStorage(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := st.CreateSession("", "1.1", []string{"10.0.0.1"}, 5000, "v2c", now, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	sessions, _ := st.ListSessions()
	if len(sessions) != 1 || sessions[0].Preset != nil {
		t.Errorf("a hand-built session must report Preset == nil, got %+v", sessions[0].Preset)
	}
}

// The upgrade direction, which is the one with the worst blast radius.
//
// ensureColumn adds a column to a database that predates it, and it LOGS AND
// CONTINUES when the ALTER fails — which is right for an optional column and
// fatal for a SELECT that names it anyway: "no such column: preset" takes every
// session with it, including the ones still polling. So this builds the old
// schema by hand, opens it, and requires the sessions to still be there.
func TestASessionCreatedBeforeThePresetColumnStillLists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "monitoring.db")

	// The sessions table as it was before this change: no preset column.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (
		id TEXT PRIMARY KEY, name TEXT, oid TEXT NOT NULL, targets TEXT NOT NULL,
		interval_ms INTEGER NOT NULL, snmp_version TEXT NOT NULL, started_at TEXT NOT NULL,
		stopped_at TEXT, thresholds TEXT, active INTEGER NOT NULL DEFAULT 0, conn TEXT)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO sessions (id, name, oid, targets, interval_ms, snmp_version, started_at, active)
		 VALUES ('old-1', 'before the column', '1.3.6.1.2.1.1.3.0', '["10.0.0.1"]', 5000, 'v2c', ?, 1)`,
		now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Init(path)
	if err != nil {
		t.Fatalf("Init on an older database: %v", err)
	}
	defer st.Close()

	if !st.sessionsHavePreset {
		t.Error("the migration did not add the column, and nothing said so")
	}
	sessions, err := st.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions after the migration: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "old-1" {
		t.Fatalf("the pre-existing session disappeared: %+v", sessions)
	}
	if sessions[0].Preset != nil {
		t.Errorf("a session that predates presets reported one: %+v", sessions[0].Preset)
	}
	if !sessions[0].Active {
		t.Error("it also lost its active flag")
	}

	// And a new session can still be created on the upgraded database.
	if _, err := st.CreateSession("after", "1.1", []string{"10.0.0.2"}, 5000, "v2c", now, nil, nil,
		&SessionPreset{File: "x.json", FormatVersion: 1}); err != nil {
		t.Fatalf("CreateSession after the migration: %v", err)
	}
	if sessions, _ := st.ListSessions(); len(sessions) != 2 {
		t.Errorf("%d sessions after adding one", len(sessions))
	}
}

// The other half of the same rule: when the column is NOT there, everything
// still works and only the layout is lost.
//
// Simulated rather than hoped for — sessionsHavePreset is what every branch
// reads, so forcing it false exercises exactly the state a failed ALTER leaves.
func TestWithoutThePresetColumnSessionsStillWork(t *testing.T) {
	st := newTestStorage(t)
	st.sessionsHavePreset = false
	now := time.Now().UTC().Format(time.RFC3339)

	id, err := st.CreateSession("degraded", "1.1", []string{"10.0.0.1"}, 5000, "v2c", now, nil, nil,
		&SessionPreset{File: "cisco.json", FormatVersion: 1,
			Widgets: []preset.Widget{{Kind: preset.KindValue, Title: "x", OIDs: []string{"1.1"}}}})
	if err != nil {
		t.Fatalf("a session could not be created without the column: %v", err)
	}
	sessions, err := st.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions without the column: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != id {
		t.Fatalf("the session is not listed: %+v", sessions)
	}
	if sessions[0].Preset != nil {
		t.Error("a snapshot appeared although the column is absent")
	}
	if sessions[0].OID != "1.1" || sessions[0].IntervalMs != 5000 {
		t.Error("what to poll was lost with the layout")
	}
}

// A snapshot that cannot be decoded costs the dashboard its layout and must not
// cost the session its poll.
func TestAnUnreadableSnapshotDoesNotHideTheSession(t *testing.T) {
	st := newTestStorage(t)
	now := time.Now().UTC().Format(time.RFC3339)
	id, err := st.CreateSession("corrupt", "1.1", []string{"10.0.0.1"}, 5000, "v2c", now, nil, nil,
		&SessionPreset{File: "x.json", FormatVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE sessions SET preset = ? WHERE id = ?`, "{not json", id); err != nil {
		t.Fatal(err)
	}

	sessions, err := st.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions with a corrupt snapshot: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("the session vanished: %+v", sessions)
	}
	if sessions[0].Preset != nil {
		t.Error("a corrupt snapshot was decoded into something")
	}
	if sessions[0].OID != "1.1" {
		t.Error("the session lost what it polls")
	}
}
