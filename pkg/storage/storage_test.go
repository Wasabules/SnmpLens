package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	// A directory name with a space on purpose: the pragmas travel in the DSN,
	// and a path that needed URL escaping would break silently.
	dir := filepath.Join(t.TempDir(), "SnmpLens data")
	st, err := Init(filepath.Join(dir, "monitoring.db"))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func f64(v float64) *float64 { return &v }

// Pragmas are connection-scoped and *sql.DB is a pool: configuring them with
// db.Exec only reaches one connection. This pins the DSN form, which reaches
// them all — the difference between ON DELETE CASCADE working and silently
// doing nothing.
func TestPragmasApplyToEveryPooledConnection(t *testing.T) {
	st := newTestStorage(t)
	ctx := context.Background()

	// Hold several connections open at once so the pool cannot hand back the
	// same one over and over.
	const want = 3
	conns := make([]interface{ Close() error }, 0, want)
	for i := 0; i < want; i++ {
		conn, err := st.db.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn %d: %v", i, err)
		}
		conns = append(conns, conn)

		var foreignKeys, busyTimeout int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("read foreign_keys on conn %d: %v", i, err)
		}
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatalf("read busy_timeout on conn %d: %v", i, err)
		}
		if foreignKeys != 1 {
			t.Errorf("conn %d: foreign_keys = %d, want 1", i, foreignKeys)
		}
		if busyTimeout != 5000 {
			t.Errorf("conn %d: busy_timeout = %d, want 5000", i, busyTimeout)
		}
	}
	for _, c := range conns {
		c.Close()
	}
}

// The visible consequence of the pragma fix: deleting a session must take its
// data points with it instead of leaving orphans behind.
func TestDeleteSessionCascadesDataPoints(t *testing.T) {
	st := newTestStorage(t)
	now := time.Now().UTC().Format(time.RFC3339)

	id, err := st.CreateSession("", "1.3.6.1.2.1.1.3.0", []string{"10.0.0.1"}, 5000, "v2c", now, nil, nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	st.QueueDataPoints([]DataPoint{
		{SessionID: id, Target: "10.0.0.1", Timestamp: now, Value: f64(1), SnmpType: "Counter32", OID: "1.3.6.1.2.1.1.3.0"},
		{SessionID: id, Target: "10.0.0.1", Timestamp: now, Value: f64(2), SnmpType: "Counter32", OID: "1.3.6.1.2.1.1.3.0"},
	})
	st.flushBatch()

	if points, err := st.QueryDataPoints(id, "", "", 0); err != nil {
		t.Fatalf("QueryDataPoints: %v", err)
	} else if len(points) != 2 {
		t.Fatalf("before delete: got %d points, want 2", len(points))
	}

	if err := st.DeleteSession(id); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	points, err := st.QueryDataPoints(id, "", "", 0)
	if err != nil {
		t.Fatalf("QueryDataPoints after delete: %v", err)
	}
	if len(points) != 0 {
		t.Errorf("cascade did not fire: %d orphaned data points remain", len(points))
	}
}

// Thresholds are keyed by OID. Sessions written before that change stored a
// single object; they must still load, attached to the session's first OID.
func TestLegacyThresholdsMigrateToFirstOID(t *testing.T) {
	st := newTestStorage(t)
	now := time.Now().UTC().Format(time.RFC3339)

	id, err := st.CreateSession("legacy", "1.3.6.1.2.1.1.3.0,1.3.6.1.2.1.2.2.1.10.1",
		[]string{"10.0.0.1"}, 5000, "v2c", now, nil, nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := st.db.Exec(`UPDATE sessions SET thresholds = ? WHERE id = ?`,
		`{"min":5,"max":42,"alertEnabled":true}`, id); err != nil {
		t.Fatalf("seed legacy thresholds: %v", err)
	}

	sessions, err := st.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	var got *Session
	for i := range sessions {
		if sessions[i].ID == id {
			got = &sessions[i]
		}
	}
	if got == nil {
		t.Fatal("session not returned")
	}
	th, ok := got.Thresholds["1.3.6.1.2.1.1.3.0"]
	if !ok {
		t.Fatalf("legacy band not attached to the first OID, got keys %v", got.Thresholds)
	}
	if th.Min == nil || *th.Min != 5 || th.Max == nil || *th.Max != 42 {
		t.Errorf("legacy bounds lost: %+v", th)
	}
}

// Buckets are what make a long time range renderable; they must group by OID as
// well as by time, or two metrics would be averaged together.
func TestQueryBucketsAggregatesPerOIDAndWindow(t *testing.T) {
	st := newTestStorage(t)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	id, err := st.CreateSession("", "a,b", []string{"10.0.0.1"}, 5000, "v2c",
		base.Format(time.RFC3339), nil, nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var points []DataPoint
	for i := 0; i < 20; i++ { // 10 minutes at 30s
		ts := base.Add(time.Duration(i) * 30 * time.Second).Format(time.RFC3339)
		points = append(points,
			DataPoint{SessionID: id, Target: "10.0.0.1", Timestamp: ts, Value: f64(float64(100 + i)), OID: "a"},
			DataPoint{SessionID: id, Target: "10.0.0.1", Timestamp: ts, Value: f64(float64(i)), OID: "b"},
		)
	}
	st.QueueDataPoints(points)
	st.flushBatch()

	buckets, err := st.QueryBuckets(id, "", "", 300) // 5-minute windows
	if err != nil {
		t.Fatalf("QueryBuckets: %v", err)
	}
	if len(buckets) != 4 { // 2 windows x 2 OIDs
		t.Fatalf("got %d buckets, want 4", len(buckets))
	}
	for _, b := range buckets {
		if b.Count != 10 {
			t.Errorf("bucket %s/%s: count = %d, want 10", b.OID, b.Timestamp, b.Count)
		}
		if b.AvgValue == nil || b.MinValue == nil || b.MaxValue == nil {
			t.Fatalf("bucket %s/%s: missing aggregates", b.OID, b.Timestamp)
		}
	}
	// First window of OID "a" holds 100..109.
	for _, b := range buckets {
		if b.OID == "a" && b.Timestamp == "2026-09-01T10:00:00Z" {
			if *b.AvgValue != 104.5 || *b.MinValue != 100 || *b.MaxValue != 109 {
				t.Errorf("avg/min/max = %v/%v/%v, want 104.5/100/109", *b.AvgValue, *b.MinValue, *b.MaxValue)
			}
		}
	}
}

// Query history rows carry the whole entry as JSON; the extracted columns are
// only there for ordering and filtering.
func TestHistoryRoundTripAndDelete(t *testing.T) {
	st := newTestStorage(t)

	entry := map[string]interface{}{
		"id":        "abc-1",
		"timestamp": "2026-09-01T10:00:00Z",
		"operation": "WALK",
		"success":   true,
		"oid":       "1.3.6.1.2.1.2.2",
		"targets":   []string{"10.0.0.1"},
	}
	if err := st.SaveHistory(entry); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}

	entries, err := st.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0]["operation"] != "WALK" || entries[0]["oid"] != "1.3.6.1.2.1.2.2" {
		t.Errorf("entry did not round-trip: %+v", entries[0])
	}

	if err := st.DeleteHistoryEntry("abc-1"); err != nil {
		t.Fatalf("DeleteHistoryEntry: %v", err)
	}
	if n, err := st.CountHistory(); err != nil {
		t.Fatalf("CountHistory: %v", err)
	} else if n != 0 {
		t.Errorf("history count = %d, want 0", n)
	}
}

// The poll clock runs in Go, so the connection must survive a restart. What
// must NOT survive here is any credential: those belong to pkg/secrets, and a
// copied monitoring.db must not be enough to reach the devices.
func TestSessionConnRoundTripsWithoutCredentials(t *testing.T) {
	st := newTestStorage(t)
	now := time.Now().UTC().Format(time.RFC3339)

	conn := &SessionConn{
		Port: 1161, TimeoutSec: 3, Retries: 2,
		V3User: "snmplens", V3AuthProto: "SHA", V3PrivProto: "AES", V3SecLevel: "authPriv",
	}
	id, err := st.CreateSession("wan", "1.3.6.1.2.1.1.3.0", []string{"10.0.0.1"}, 5000, "v3", now, nil, conn)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sessions, err := st.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Conn == nil {
		t.Fatalf("connection profile did not round-trip: %+v", sessions)
	}
	got := *sessions[0].Conn
	if got != *conn {
		t.Errorf("got %+v, want %+v", got, *conn)
	}

	var raw sql.NullString
	if err := st.db.QueryRow(`SELECT conn FROM sessions WHERE id = ?`, id).Scan(&raw); err != nil {
		t.Fatalf("read back the stored profile: %v", err)
	}
	for _, forbidden := range []string{"community", "authPass", "privPass", "password"} {
		if strings.Contains(strings.ToLower(raw.String), forbidden) {
			t.Errorf("the connection profile carries %q into the database: %s", forbidden, raw.String)
		}
	}
}

// A session created before the profile existed must not be resumed headlessly
// rather than resumed with no credentials at all.
func TestSessionWithoutConnIsDistinguishable(t *testing.T) {
	st := newTestStorage(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := st.CreateSession("", "1.1", []string{"10.0.0.1"}, 5000, "v2c", now, nil, nil); err != nil {
		t.Fatal(err)
	}
	sessions, _ := st.ListSessions()
	if len(sessions) != 1 || sessions[0].Conn != nil {
		t.Errorf("a session with no profile must report Conn == nil, got %+v", sessions[0].Conn)
	}
}

func TestUpdateSessionConn(t *testing.T) {
	st := newTestStorage(t)
	now := time.Now().UTC().Format(time.RFC3339)
	id, _ := st.CreateSession("", "1.1", []string{"10.0.0.1"}, 5000, "v2c", now, nil, nil)

	if err := st.UpdateSessionConn(id, &SessionConn{Port: 1161, TimeoutSec: 9}); err != nil {
		t.Fatalf("UpdateSessionConn: %v", err)
	}
	sessions, _ := st.ListSessions()
	if sessions[0].Conn == nil || sessions[0].Conn.Port != 1161 || sessions[0].Conn.TimeoutSec != 9 {
		t.Errorf("profile not updated: %+v", sessions[0].Conn)
	}
}
