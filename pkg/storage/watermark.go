package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"SnmpLens/pkg/events"
)

// RoutedGroup is one rendering, and the sinks it goes to.
//
// Routing produces one of these per group of destinations that share a rendered
// subject and body, which is what EnqueueDeliveries takes for a single event.
// This is the same thing for many events at once.
type RoutedGroup struct {
	Event   events.Event
	SinkIDs []string
	Subject string
	Body    string
}

// RoutedThrough returns the seq routing has been carried through.
//
// Anything above it has been journalled but may not have had its deliveries
// written, so it is replayed at startup. Replay is safe to repeat because
// EnqueueDeliveries is INSERT OR IGNORE against UNIQUE(event_id, sink_id).
func (s *Storage) RoutedThrough() (int64, error) {
	var mark int64
	err := s.db.QueryRow(`SELECT routed_through_seq FROM notify_watermark WHERE id = 1`).Scan(&mark)
	if errors.Is(err, sql.ErrNoRows) {
		// The schema seeds this row, so an absent one means a database from
		// before the column existed, or one that has been replaced underneath
		// us. Seeding to the newest event is the conservative answer: it
		// replays nothing rather than replaying everything.
		return s.seedWatermark()
	}
	if err != nil {
		return 0, err
	}

	// A watermark above the newest event means the events table was replaced or
	// trimmed below it — the only signature of a swapped database. Trust the
	// events, not the number.
	var newest int64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM events`).Scan(&newest); err != nil {
		return mark, nil
	}
	if mark > newest {
		if _, err := s.db.Exec(
			`UPDATE notify_watermark SET routed_through_seq = ? WHERE id = 1`, newest); err == nil {
			return newest, nil
		}
	}
	return mark, nil
}

func (s *Storage) seedWatermark() (int64, error) {
	var newest int64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM events`).Scan(&newest); err != nil {
		return 0, err
	}
	_, err := s.db.Exec(`
		INSERT INTO notify_watermark (id, routed_through_seq) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET routed_through_seq = excluded.routed_through_seq`, newest)
	return newest, err
}

// EnqueueRouted writes many events' deliveries and moves the watermark, in ONE
// transaction.
//
// The two must be atomic together. A watermark written separately could commit
// while the deliveries did not, and the events it names would then never be
// replayed and never be delivered — the exact loss the watermark exists to
// prevent.
//
// The caller decides how many groups to pass. It should not be "all of them":
// SQLite serialises writers, so this transaction holds the write lock against
// the trap listener's InsertEvent for as long as it runs. Ten thousand rows was
// measured at 392 ms, and a replay of the full retention caps would be about
// 14 seconds — long enough for the busy timeout to expire and a trap to be lost
// while its INFORM is acknowledged.
func (s *Storage) EnqueueRouted(groups []RoutedGroup, watermark int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if len(groups) > 0 {
		stmt, err := tx.Prepare(`
			INSERT OR IGNORE INTO notify_outbox
				(event_id, sink_id, event_json, subject, body, attempts, next_try_at, state, created_at)
			VALUES (?, ?, ?, ?, ?, 0, ?, 'pending', ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		now := time.Now().UTC().Format(time.RFC3339)
		for _, g := range groups {
			if len(g.SinkIDs) == 0 {
				continue
			}
			snapshot, err := json.Marshal(g.Event)
			if err != nil {
				return fmt.Errorf("snapshot event %s: %w", g.Event.ID, err)
			}
			for _, sinkID := range g.SinkIDs {
				if _, err := stmt.Exec(g.Event.ID, sinkID, string(snapshot),
					g.Subject, g.Body, now, now); err != nil {
					return err
				}
			}
		}
	}

	// Never backwards. A batch that finishes out of order — the flush covering
	// seq 100 committing after the one covering seq 120 — must not un-route what
	// the later one accounted for.
	if _, err := tx.Exec(`
		UPDATE notify_watermark SET routed_through_seq = ?
		WHERE id = 1 AND routed_through_seq < ?`, watermark, watermark); err != nil {
		return err
	}
	return tx.Commit()
}

// EventsAfter returns journalled events above seq, oldest first, for replay.
func (s *Storage) EventsAfter(seq int64, limit int) ([]events.Event, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT id, ts, category, kind, severity, state,
		       COALESCE(source, ''), COALESCE(oid, ''),
		       COALESCE(session_id, ''), COALESCE(session_name, ''),
		       COALESCE(dedup_key, ''), COALESCE(corr_id, ''),
		       title_key, params, summary, value, payload_size, acked, seq
		FROM events WHERE seq > ? ORDER BY seq LIMIT ?`, seq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []events.Event
	for rows.Next() {
		var (
			e        events.Event
			severity int
			params   string
			value    sql.NullFloat64
			acked    int
		)
		if err := rows.Scan(&e.ID, &e.Ts, &e.Category, &e.Kind, &severity, &e.State,
			&e.Source, &e.OID, &e.SessionID, &e.SessionName, &e.DedupKey, &e.CorrID,
			&e.TitleKey, &params, &e.Summary, &value, &e.PayloadSize, &acked, &e.Seq); err != nil {
			return nil, err
		}
		e.Severity = events.Severity(severity).String()
		e.Acked = acked == 1
		if value.Valid {
			v := value.Float64
			e.Value = &v
		}
		if params != "" {
			_ = json.Unmarshal([]byte(params), &e.Params)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
