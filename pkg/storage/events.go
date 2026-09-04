package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"SnmpLens/pkg/events"
)

// Retention caps per category. Traps arrive unsolicited and in bursts while
// system events trickle, so one shared FIFO cap would let a trap storm evict
// everything else — which is exactly the flaw that rules out storing events in
// query_history.
var eventRetention = map[string]int{
	events.CategoryTrap:         20000,
	events.CategoryThreshold:    20000,
	events.CategoryReachability: 20000,
	events.CategorySystem:       5000,
	events.CategoryOperation:    5000,
}

const maxEventPayloads = 2000

// InsertEvent writes an event, its optional payload and its outbox rows in ONE
// transaction, synchronously.
//
// This is a deliberate divergence from QueueDataPoints, which buffers and
// flushes on a ticker: losing five seconds of chart samples in a crash is
// tolerable, losing five seconds of alerts between "the operator was notified"
// and "a row exists" destroys the audit trail. Data points batch. Events do not.
func (s *Storage) InsertEvent(e events.Event, payload string) (events.Event, error) {
	if err := events.Validate(e); err != nil {
		return e, err
	}
	if e.ID == "" {
		e.ID = generateUUID()
	}
	if e.Ts == "" {
		e.Ts = time.Now().UTC().Format(time.RFC3339)
	}
	if e.State == "" {
		e.State = events.StateOneshot
	}
	if e.Summary == "" {
		e.Summary = e.Kind
	}

	params := "{}"
	if len(e.Params) > 0 {
		if b, err := json.Marshal(e.Params); err == nil {
			params = string(b)
		}
	}
	e.PayloadSize = len(payload)

	tx, err := s.db.Begin()
	if err != nil {
		return e, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO events (id, ts, category, kind, severity, state, source, oid,
		                    session_id, session_name, dedup_key, corr_id,
		                    title_key, params, summary, value, payload_size, acked)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Ts, e.Category, e.Kind, int(events.ParseSeverity(e.Severity)), e.State,
		nullableString([]byte(e.Source)), nullableString([]byte(e.OID)),
		nullableString([]byte(e.SessionID)), nullableString([]byte(e.SessionName)),
		nullableString([]byte(e.DedupKey)), nullableString([]byte(e.CorrID)),
		e.TitleKey, params, e.Summary, e.Value, e.PayloadSize, boolToInt(e.Acked),
	)
	if err != nil {
		return e, fmt.Errorf("insert event: %w", err)
	}
	if seq, err := res.LastInsertId(); err == nil {
		e.Seq = seq
	}

	if payload != "" {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO event_payloads (event_id, body) VALUES (?, ?)`,
			e.ID, payload); err != nil {
			return e, fmt.Errorf("insert event payload: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return e, err
	}

	s.noteEventWrite()
	return e, nil
}

// noteEventWrite trims periodically rather than on every insert: the hot path
// stays a single INSERT, and the caps are soft by design.
func (s *Storage) noteEventWrite() {
	s.mu.Lock()
	s.eventWrites++
	shouldTrim := s.eventWrites%256 == 0
	s.mu.Unlock()
	if shouldTrim {
		s.TrimEvents()
	}
}

// TrimEvents enforces the per-category caps and drops orphaned payloads.
func (s *Storage) TrimEvents() {
	for category, keep := range eventRetention {
		s.trimCategory(category, keep)
	}
	s.trimPayloads()
}

// trimCategory drops everything in a category older than its newest `keep`.
//
// The shape matters more than it looks. This used to be one statement,
//
//	DELETE FROM events WHERE category = ? AND seq NOT IN (
//	    SELECT seq FROM events WHERE category = ? ORDER BY seq DESC LIMIT ?)
//
// which EXPLAIN QUERY PLAN shows materialising a twenty-thousand-entry LIST
// SUBQUERY and a bloom filter on EVERY call — measured 80 ms for the trap
// category alone, and 130-330 ms for a whole trim.
//
// That cost lands on the CALLER's goroutine, and for a trap that caller is
// gosnmp's serial UDP receive loop: every millisecond here is a millisecond not
// reading the socket. Moving it to a background goroutine does not help and was
// measured making things worse — SQLite serialises writers, so InsertEvent just
// blocks on the write lock instead (mean 2.5 -> 8.5 ms, worst case 62 -> 335 ms).
//
// Finding the cutoff first turns the delete into an indexed RANGE over
// idx_ev_cat_seq(category, seq), which is what that index is for.
func (s *Storage) trimCategory(category string, keep int) {
	var cutoff int64
	err := s.db.QueryRow(`
		SELECT seq FROM events WHERE category = ?
		ORDER BY seq DESC LIMIT 1 OFFSET ?`, category, keep-1).Scan(&cutoff)
	if errors.Is(err, sql.ErrNoRows) {
		return // fewer rows than the cap: nothing to do
	}
	if err != nil {
		return
	}

	// The payloads first, while their events still name them: deleting the
	// events first would turn every one of these into an orphan for the sweep
	// below to find the expensive way.
	if _, err := s.db.Exec(`
		DELETE FROM event_payloads WHERE event_id IN (
			SELECT id FROM events WHERE category = ? AND seq < ? AND payload_size > 0)`,
		category, cutoff); err != nil {
		return
	}
	s.db.Exec(`DELETE FROM events WHERE category = ? AND seq < ?`, category, cutoff)
}

// trimPayloads enforces the payload cap and reaps anything orphaned.
//
// Explicit reaping: there is no CASCADE, because deletion must not depend on a
// connection-scoped pragma.
func (s *Storage) trimPayloads() {
	var cutoff int64
	err := s.db.QueryRow(`
		SELECT seq FROM events WHERE payload_size > 0
		ORDER BY seq DESC LIMIT 1 OFFSET ?`, maxEventPayloads-1).Scan(&cutoff)
	if err == nil {
		s.db.Exec(`
			DELETE FROM event_payloads WHERE event_id IN (
				SELECT id FROM events WHERE payload_size > 0 AND seq < ?)`, cutoff)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return
	}

	// NOT EXISTS rather than NOT IN: events.id is UNIQUE, so this is one index
	// lookup per payload row. NOT IN materialised every id in the journal —
	// seventy thousand of them — to check at most a couple of thousand rows,
	// and planned as a full SCAN of events.
	s.db.Exec(`
		DELETE FROM event_payloads WHERE NOT EXISTS (
			SELECT 1 FROM events WHERE events.id = event_payloads.event_id)`)
}

// buildEventWhere turns a filter into a WHERE fragment plus its arguments.
func buildEventWhere(f events.Filter) (string, []interface{}) {
	var clauses []string
	var args []interface{}

	// A single category is an index search on idx_ev_cat_seq. An IN() over
	// several plans as a search PLUS a temp b-tree sort of the whole match set,
	// so emit no predicate at all when every category is selected.
	if len(f.Categories) == 1 {
		clauses = append(clauses, "category = ?")
		args = append(args, f.Categories[0])
	} else if len(f.Categories) > 1 && len(f.Categories) < len(eventRetention) {
		clauses = append(clauses, "category IN ("+placeholders(len(f.Categories))+")")
		for _, c := range f.Categories {
			args = append(args, c)
		}
	}
	if len(f.Kinds) > 0 {
		clauses = append(clauses, "kind IN ("+placeholders(len(f.Kinds))+")")
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	if f.MinSeverity != "" {
		clauses = append(clauses, "severity >= ?")
		args = append(args, int(events.ParseSeverity(f.MinSeverity)))
	}
	if f.Source != "" {
		clauses = append(clauses, "source LIKE ?")
		args = append(args, "%"+f.Source+"%")
	}
	if f.OID != "" {
		clauses = append(clauses, "oid LIKE ?")
		args = append(args, f.OID+"%")
	}
	if f.SessionID != "" {
		clauses = append(clauses, "session_id = ?")
		args = append(args, f.SessionID)
	}
	if f.From != "" {
		clauses = append(clauses, "ts >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		clauses = append(clauses, "ts <= ?")
		args = append(args, f.To)
	}
	if f.Search != "" {
		clauses = append(clauses, "(summary LIKE ? OR source LIKE ? OR oid LIKE ?)")
		like := "%" + f.Search + "%"
		args = append(args, like, like, like)
	}
	if f.UnackedOnly {
		clauses = append(clauses, "acked = 0")
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// QueryEvents returns one page, newest first, using keyset pagination.
//
// Keyset rather than OFFSET: the journal is written to while it is being read,
// so an OFFSET page would skip or repeat rows as new events arrive.
func (s *Storage) QueryEvents(f events.Filter) (events.Page, error) {
	page := events.Page{Items: []events.Event{}}

	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	where, args := buildEventWhere(f)

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events`+where, args...).Scan(&page.Total); err != nil {
		return page, err
	}

	query := `SELECT seq, id, ts, category, kind, severity, state, source, oid,
	                 session_id, session_name, dedup_key, corr_id, title_key,
	                 params, summary, value, payload_size, acked
	          FROM events` + where
	pageArgs := append([]interface{}{}, args...)
	if f.BeforeSeq > 0 {
		if where == "" {
			query += " WHERE seq < ?"
		} else {
			query += " AND seq < ?"
		}
		pageArgs = append(pageArgs, f.BeforeSeq)
	}
	query += " ORDER BY seq DESC LIMIT ?"
	pageArgs = append(pageArgs, limit)

	rows, err := s.db.Query(query, pageArgs...)
	if err != nil {
		return page, err
	}
	defer rows.Close()

	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, e)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) == limit {
		page.NextCursor = page.Items[len(page.Items)-1].Seq
	}
	return page, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanEvent(rows rowScanner) (events.Event, error) {
	var e events.Event
	var sev, acked int
	var source, oid, sessionID, sessionName, dedupKey, corrID sql.NullString
	var params string
	var value sql.NullFloat64

	if err := rows.Scan(&e.Seq, &e.ID, &e.Ts, &e.Category, &e.Kind, &sev, &e.State,
		&source, &oid, &sessionID, &sessionName, &dedupKey, &corrID, &e.TitleKey,
		&params, &e.Summary, &value, &e.PayloadSize, &acked); err != nil {
		return e, err
	}

	e.Severity = events.Severity(sev).String()
	e.Acked = acked == 1
	e.Source = source.String
	e.OID = oid.String
	e.SessionID = sessionID.String
	e.SessionName = sessionName.String
	e.DedupKey = dedupKey.String
	e.CorrID = corrID.String
	if value.Valid {
		v := value.Float64
		e.Value = &v
	}
	if params != "" && params != "{}" {
		json.Unmarshal([]byte(params), &e.Params)
	}
	return e, nil
}

// EventPayload returns the bulk detail of one event, loaded only on demand.
func (s *Storage) EventPayload(id string) (string, error) {
	var body string
	err := s.db.QueryRow(`SELECT body FROM event_payloads WHERE event_id = ?`, id).Scan(&body)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return body, err
}

// EventCounts feeds the unacknowledged badge without fetching rows.
func (s *Storage) EventCounts() (events.Counts, error) {
	counts := events.Counts{
		UnackedBySev:    map[string]int{},
		UnackedByCatego: map[string]int{},
	}
	rows, err := s.db.Query(`SELECT category, severity, COUNT(*) FROM events WHERE acked = 0 GROUP BY category, severity`)
	if err != nil {
		return counts, err
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		var sev, n int
		if err := rows.Scan(&category, &sev, &n); err != nil {
			return counts, err
		}
		counts.Unacked += n
		counts.UnackedByCatego[category] += n
		counts.UnackedBySev[events.Severity(sev).String()] += n
	}
	return counts, rows.Err()
}

// AckEvents marks events as acknowledged so they stop counting towards the badge.
func (s *Storage) AckEvents(ids []string) error {
	return s.applyToEvents(ids, `UPDATE events SET acked = 1 WHERE id = ?`)
}

// AckAllEvents acknowledges everything matching a filter — the "clear the badge"
// action, which must not require paging through thousands of rows.
func (s *Storage) AckAllEvents(f events.Filter) error {
	where, args := buildEventWhere(f)
	_, err := s.db.Exec(`UPDATE events SET acked = 1`+where, args...)
	return err
}

// DeleteEvents removes events and their payloads.
func (s *Storage) DeleteEvents(ids []string) error {
	if err := s.applyToEvents(ids, `DELETE FROM events WHERE id = ?`); err != nil {
		return err
	}
	return s.applyToEvents(ids, `DELETE FROM event_payloads WHERE event_id = ?`)
}

func (s *Storage) applyToEvents(ids []string, query string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, id := range ids {
		if _, err := stmt.Exec(id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearEvents empties the journal entirely.
func (s *Storage) ClearEvents() error {
	if _, err := s.db.Exec(`DELETE FROM events`); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM event_payloads`)
	return err
}

// --- Episode state (restart-safe dwell tracking) ---

// Episode is an in-progress threshold or reachability breach.
type Episode struct {
	DedupKey  string `json:"dedupKey"`
	Kind      string `json:"kind"`
	SessionID string `json:"sessionId"`
	Target    string `json:"target"`
	OID       string `json:"oid"`
	FirstSeen string `json:"firstSeen"`
	FiredAt   string `json:"firedAt"`
	CorrID    string `json:"corrId"`
}

// LoadEpisodes returns every open episode, so a restart resumes them instead of
// re-raising an incident that was already reported.
func (s *Storage) LoadEpisodes() ([]Episode, error) {
	rows, err := s.db.Query(`SELECT dedup_key, kind, COALESCE(session_id,''), target,
	                                COALESCE(oid,''), first_seen, COALESCE(fired_at,''),
	                                COALESCE(corr_id,'') FROM event_episodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	episodes := []Episode{}
	for rows.Next() {
		var e Episode
		if err := rows.Scan(&e.DedupKey, &e.Kind, &e.SessionID, &e.Target, &e.OID,
			&e.FirstSeen, &e.FiredAt, &e.CorrID); err != nil {
			return nil, err
		}
		episodes = append(episodes, e)
	}
	return episodes, rows.Err()
}

// SaveEpisode records or updates an in-progress breach.
func (s *Storage) SaveEpisode(e Episode) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO event_episodes
			(dedup_key, kind, session_id, target, oid, first_seen, fired_at, corr_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.DedupKey, e.Kind, nullableString([]byte(e.SessionID)), e.Target,
		nullableString([]byte(e.OID)), e.FirstSeen,
		nullableString([]byte(e.FiredAt)), nullableString([]byte(e.CorrID)))
	return err
}

// DeleteEpisode closes an episode.
func (s *Storage) DeleteEpisode(dedupKey string) error {
	_, err := s.db.Exec(`DELETE FROM event_episodes WHERE dedup_key = ?`, dedupKey)
	return err
}
