package storage

import (
	"database/sql"
	"encoding/json"
	"time"

	"SnmpLens/pkg/events"
	"SnmpLens/pkg/notify"
)

// *Storage must satisfy the dispatcher's queue contract; this fails the build
// rather than at runtime if a signature drifts.
var _ notify.Queue = (*Storage)(nil)

// Delivery is one queued attempt to hand an event to a sink.
type Delivery struct {
	ID        int64  `json:"id"`
	EventID   string `json:"eventId"`
	SinkID    string `json:"sinkId"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Attempts  int    `json:"attempts"`
	NextTryAt string `json:"nextTryAt"`
	LastError string `json:"lastError,omitempty"`
	State     string `json:"state"` // pending | sent | dead
	CreatedAt string `json:"createdAt"`
	// Event is the snapshot the sink will render. Stored inline so the
	// dispatcher never has to read the journal.
	Event events.Event `json:"event"`
}

// --- Sinks ---

// ListSinks returns every configured destination.
func (s *Storage) ListSinks() ([]notify.SinkConfig, error) {
	rows, err := s.db.Query(`SELECT id, name, kind, enabled, config FROM notify_sinks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []notify.SinkConfig{}
	for rows.Next() {
		var cfg notify.SinkConfig
		var enabled int
		var raw string
		if err := rows.Scan(&cfg.ID, &cfg.Name, &cfg.Kind, &enabled, &raw); err != nil {
			return nil, err
		}
		cfg.Enabled = enabled == 1
		// The typed sub-config lives in the JSON blob so adding a sink kind
		// never needs a migration.
		json.Unmarshal([]byte(raw), &cfg)
		out = append(out, cfg)
	}
	return out, rows.Err()
}

// SaveSink inserts or updates a destination.
func (s *Storage) SaveSink(cfg notify.SinkConfig) (notify.SinkConfig, error) {
	if cfg.ID == "" {
		cfg.ID = generateUUID()
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	_, err = s.db.Exec(`
		INSERT INTO notify_sinks (id, name, kind, enabled, config) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, kind = excluded.kind,
			enabled = excluded.enabled, config = excluded.config`,
		cfg.ID, cfg.Name, cfg.Kind, boolToInt(cfg.Enabled), string(raw))
	return cfg, err
}

// DeleteSink removes a destination.
func (s *Storage) DeleteSink(id string) error {
	_, err := s.db.Exec(`DELETE FROM notify_sinks WHERE id = ?`, id)
	return err
}

// --- Routes ---

// ListRoutes returns every routing rule.
func (s *Storage) ListRoutes() ([]notify.Route, error) {
	rows, err := s.db.Query(`SELECT id, name, enabled, priority, stop, match, sink_ids
	                         FROM notify_routes ORDER BY priority, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []notify.Route{}
	for rows.Next() {
		var r notify.Route
		var enabled, stop int
		var matchJSON, sinkJSON string
		if err := rows.Scan(&r.ID, &r.Name, &enabled, &r.Priority, &stop, &matchJSON, &sinkJSON); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		r.Stop = stop == 1
		json.Unmarshal([]byte(matchJSON), &r.Match)
		json.Unmarshal([]byte(sinkJSON), &r.SinkIDs)
		if r.SinkIDs == nil {
			r.SinkIDs = []string{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveRoute inserts or updates a routing rule.
func (s *Storage) SaveRoute(r notify.Route) (notify.Route, error) {
	if r.ID == "" {
		r.ID = generateUUID()
	}
	matchJSON, err := json.Marshal(r.Match)
	if err != nil {
		return r, err
	}
	if r.SinkIDs == nil {
		r.SinkIDs = []string{}
	}
	sinkJSON, err := json.Marshal(r.SinkIDs)
	if err != nil {
		return r, err
	}
	_, err = s.db.Exec(`
		INSERT INTO notify_routes (id, name, enabled, priority, stop, match, sink_ids)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, enabled = excluded.enabled,
			priority = excluded.priority, stop = excluded.stop,
			match = excluded.match, sink_ids = excluded.sink_ids`,
		r.ID, r.Name, boolToInt(r.Enabled), r.Priority, boolToInt(r.Stop),
		string(matchJSON), string(sinkJSON))
	return r, err
}

// DeleteRoute removes a routing rule.
func (s *Storage) DeleteRoute(id string) error {
	_, err := s.db.Exec(`DELETE FROM notify_routes WHERE id = ?`, id)
	return err
}

// --- Outbox ---

// EnqueueDeliveries queues one event for each sink, in a single transaction.
//
// INSERT OR IGNORE, not plain INSERT: with a UNIQUE(event_id, sink_id) a replay
// would otherwise abort the whole publish — losing the EVENT, not merely the
// duplicate delivery.
func (s *Storage) EnqueueDeliveries(e events.Event, sinkIDs []string, subject, body string) error {
	if len(sinkIDs) == 0 {
		return nil
	}
	snapshot, err := json.Marshal(e)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO notify_outbox
			(event_id, sink_id, event_json, subject, body, attempts, next_try_at, state, created_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, 'pending', ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sinkID := range sinkIDs {
		if _, err := stmt.Exec(e.ID, sinkID, string(snapshot), subject, body, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DueDeliveries returns queued deliveries whose retry time has arrived.
// It returns notify.Queued so that *Storage satisfies notify.Queue directly.
func (s *Storage) DueDeliveries(limit int) ([]notify.Queued, error) {
	if limit <= 0 {
		limit = 20
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.db.Query(`
		SELECT id, event_id, sink_id, event_json, subject, body, attempts, next_try_at,
		       COALESCE(last_error, ''), state, created_at
		FROM notify_outbox
		WHERE state = 'pending' AND next_try_at <= ?
		ORDER BY next_try_at LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rowsOut, err := scanDeliveries(rows)
	if err != nil {
		return nil, err
	}
	queued := make([]notify.Queued, 0, len(rowsOut))
	for _, d := range rowsOut {
		queued = append(queued, notify.Queued{
			ID:       d.ID,
			SinkID:   d.SinkID,
			Subject:  d.Subject,
			Body:     d.Body,
			Attempts: d.Attempts,
			Event:    d.Event,
		})
	}
	return queued, nil
}

// ListDeliveries returns the delivery log, newest first. state may be empty for
// all, or "pending"/"sent"/"dead".
func (s *Storage) ListDeliveries(state string, limit int) ([]Delivery, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id, event_id, sink_id, event_json, subject, body, attempts, next_try_at,
	                 COALESCE(last_error, ''), state, created_at FROM notify_outbox`
	args := []interface{}{}
	if state != "" {
		query += ` WHERE state = ?`
		args = append(args, state)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

func scanDeliveries(rows *sql.Rows) ([]Delivery, error) {
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		var snapshot string
		if err := rows.Scan(&d.ID, &d.EventID, &d.SinkID, &snapshot, &d.Subject, &d.Body,
			&d.Attempts, &d.NextTryAt, &d.LastError, &d.State, &d.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(snapshot), &d.Event)
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkSent closes a delivery successfully. Part of notify.Queue.
func (s *Storage) MarkSent(id int64) error {
	_, err := s.db.Exec(`UPDATE notify_outbox SET state = 'sent', last_error = NULL, attempts = attempts + 1 WHERE id = ?`, id)
	return err
}

// MarkFailed records a failure and schedules the next attempt, or gives up. A
// dead letter is kept, never deleted: it is the only way an operator learns
// that a notification never arrived. Part of notify.Queue.
func (s *Storage) MarkFailed(id int64, errMsg string, nextTry time.Time, dead bool) error {
	state := "pending"
	if dead {
		state = "dead"
	}
	_, err := s.db.Exec(`
		UPDATE notify_outbox
		SET attempts = attempts + 1, last_error = ?, next_try_at = ?, state = ?
		WHERE id = ?`,
		errMsg, nextTry.UTC().Format(time.RFC3339), state, id)
	return err
}

// RetryDelivery puts a dead letter back in the queue.
func (s *Storage) RetryDelivery(id int64) error {
	_, err := s.db.Exec(`
		UPDATE notify_outbox SET state = 'pending', next_try_at = ?, last_error = NULL
		WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// TrimOutbox drops delivered rows older than the retention window. Dead letters
// are deliberately excluded: they are kept until the operator deals with them.
func (s *Storage) TrimOutbox(keepSentFor time.Duration) error {
	cutoff := time.Now().Add(-keepSentFor).UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`DELETE FROM notify_outbox WHERE state = 'sent' AND created_at < ?`, cutoff)
	return err
}
