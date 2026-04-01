package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Storage manages SQLite persistence for monitoring data.
type Storage struct {
	db          *sql.DB
	mu          sync.Mutex
	batch       []DataPoint
	batchTicker *time.Ticker
	done        chan struct{}
}

type Session struct {
	ID          string      `json:"id"`
	OID         string      `json:"oid"`
	Targets     []string    `json:"targets"`
	IntervalMs  int         `json:"intervalMs"`
	SnmpVersion string      `json:"snmpVersion"`
	StartedAt   string      `json:"startedAt"`
	StoppedAt   string      `json:"stoppedAt,omitempty"`
	Thresholds  *Thresholds `json:"thresholds,omitempty"`
	Active      bool        `json:"active"`
}

type Thresholds struct {
	Min          *float64 `json:"min,omitempty"`
	Max          *float64 `json:"max,omitempty"`
	AlertEnabled bool     `json:"alertEnabled"`
}

type DataPoint struct {
	SessionID      string   `json:"sessionId"`
	Target         string   `json:"target"`
	Timestamp      string   `json:"timestamp"`
	Value          *float64 `json:"value"`
	Delta          *float64 `json:"delta"`
	Rate           *float64 `json:"rate"`
	ResponseTimeMs int      `json:"responseTimeMs"`
	Error          string   `json:"error,omitempty"`
}

type SessionStats struct {
	TotalPoints    int      `json:"totalPoints"`
	FirstTimestamp string   `json:"firstTimestamp"`
	LastTimestamp  string   `json:"lastTimestamp"`
	MinValue       *float64 `json:"minValue"`
	MaxValue       *float64 `json:"maxValue"`
	AvgValue       *float64 `json:"avgValue"`
	AvgLatency     *float64 `json:"avgLatency"`
	ErrorCount     int      `json:"errorCount"`
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Init opens or creates the SQLite database and starts the batch flush goroutine.
func Init(dbPath string) (*Storage, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Configure SQLite for concurrent access
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %s: %w", p, err)
		}
	}

	// Create tables
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id           TEXT PRIMARY KEY,
		oid          TEXT NOT NULL,
		targets      TEXT NOT NULL,
		interval_ms  INTEGER NOT NULL,
		snmp_version TEXT NOT NULL,
		started_at   TEXT NOT NULL,
		stopped_at   TEXT,
		thresholds   TEXT,
		active       INTEGER NOT NULL DEFAULT 1
	);

	CREATE TABLE IF NOT EXISTS data_points (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id       TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		target           TEXT NOT NULL,
		timestamp        TEXT NOT NULL,
		value            REAL,
		delta            REAL,
		rate             REAL,
		response_time_ms INTEGER,
		error            TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_dp_session_ts ON data_points(session_id, timestamp);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	s := &Storage{
		db:          db,
		batchTicker: time.NewTicker(5 * time.Second),
		done:        make(chan struct{}),
	}

	// Background batch flush goroutine
	go func() {
		for {
			select {
			case <-s.batchTicker.C:
				s.flushBatch()
			case <-s.done:
				return
			}
		}
	}()

	log.Printf("Monitoring storage initialized: %s", dbPath)
	return s, nil
}

// Close flushes pending data and closes the database.
func (s *Storage) Close() error {
	s.batchTicker.Stop()
	close(s.done)
	s.flushBatch() // final flush
	return s.db.Close()
}

// CreateSession inserts a new monitoring session and returns its UUID.
func (s *Storage) CreateSession(oid string, targets []string, intervalMs int, snmpVersion, startedAt string, thresholds *Thresholds) (string, error) {
	id := generateUUID()
	targetsJSON, _ := json.Marshal(targets)
	var thresholdsJSON []byte
	if thresholds != nil {
		thresholdsJSON, _ = json.Marshal(thresholds)
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, oid, targets, interval_ms, snmp_version, started_at, thresholds, active)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
		id, oid, string(targetsJSON), intervalMs, snmpVersion, startedAt, nullableString(thresholdsJSON),
	)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

// UpdateSession updates a session's active status and stopped_at timestamp.
func (s *Storage) UpdateSession(id string, active bool, stoppedAt string) error {
	activeInt := 0
	if active {
		activeInt = 1
	}
	var stopped interface{}
	if stoppedAt != "" {
		stopped = stoppedAt
	}
	_, err := s.db.Exec(
		`UPDATE sessions SET active = ?, stopped_at = ? WHERE id = ?`,
		activeInt, stopped, id,
	)
	return err
}

// DeleteSession removes a session and all its data points (via CASCADE).
func (s *Storage) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// ListSessions returns all persisted sessions.
func (s *Storage) ListSessions() ([]Session, error) {
	rows, err := s.db.Query(`SELECT id, oid, targets, interval_ms, snmp_version, started_at, stopped_at, thresholds, active FROM sessions ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		var targetsJSON string
		var stoppedAt sql.NullString
		var thresholdsJSON sql.NullString
		var active int

		if err := rows.Scan(&sess.ID, &sess.OID, &targetsJSON, &sess.IntervalMs, &sess.SnmpVersion, &sess.StartedAt, &stoppedAt, &thresholdsJSON, &active); err != nil {
			return nil, err
		}

		json.Unmarshal([]byte(targetsJSON), &sess.Targets)
		if stoppedAt.Valid {
			sess.StoppedAt = stoppedAt.String
		}
		if thresholdsJSON.Valid {
			json.Unmarshal([]byte(thresholdsJSON.String), &sess.Thresholds)
		}
		sess.Active = active == 1
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

// QueueDataPoints adds data points to the batch buffer for async insertion.
func (s *Storage) QueueDataPoints(points []DataPoint) {
	if len(points) == 0 {
		return
	}
	s.mu.Lock()
	s.batch = append(s.batch, points...)
	s.mu.Unlock()
}

func (s *Storage) flushBatch() {
	s.mu.Lock()
	if len(s.batch) == 0 {
		s.mu.Unlock()
		return
	}
	points := s.batch
	s.batch = nil
	s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("storage: begin tx: %v", err)
		return
	}

	stmt, err := tx.Prepare(`INSERT INTO data_points (session_id, target, timestamp, value, delta, rate, response_time_ms, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		log.Printf("storage: prepare insert: %v", err)
		return
	}
	defer stmt.Close()

	for _, p := range points {
		_, err := stmt.Exec(p.SessionID, p.Target, p.Timestamp, p.Value, p.Delta, p.Rate, p.ResponseTimeMs, nullableString([]byte(p.Error)))
		if err != nil {
			log.Printf("storage: insert point: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("storage: commit: %v", err)
	}
}

// QueryDataPoints retrieves data points for a session, optionally filtered by time range and limited.
func (s *Storage) QueryDataPoints(sessionID, from, to string, limit int) ([]DataPoint, error) {
	query := `SELECT session_id, target, timestamp, value, delta, rate, response_time_ms, error FROM data_points WHERE session_id = ?`
	args := []interface{}{sessionID}

	if from != "" {
		query += ` AND timestamp >= ?`
		args = append(args, from)
	}
	if to != "" {
		query += ` AND timestamp <= ?`
		args = append(args, to)
	}
	query += ` ORDER BY timestamp ASC`
	if limit > 0 {
		// For "last N points", use a subquery to get the tail
		query = fmt.Sprintf(`SELECT * FROM (%s ORDER BY timestamp DESC LIMIT ?) ORDER BY timestamp ASC`,
			`SELECT session_id, target, timestamp, value, delta, rate, response_time_ms, error FROM data_points WHERE session_id = ?`+
				timeFilter(from, to))
		args = []interface{}{sessionID}
		if from != "" {
			args = append(args, from)
		}
		if to != "" {
			args = append(args, to)
		}
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []DataPoint
	for rows.Next() {
		var p DataPoint
		var value, delta, rate sql.NullFloat64
		var errStr sql.NullString
		var respTime sql.NullInt64

		if err := rows.Scan(&p.SessionID, &p.Target, &p.Timestamp, &value, &delta, &rate, &respTime, &errStr); err != nil {
			return nil, err
		}
		if value.Valid {
			p.Value = &value.Float64
		}
		if delta.Valid {
			p.Delta = &delta.Float64
		}
		if rate.Valid {
			p.Rate = &rate.Float64
		}
		if respTime.Valid {
			p.ResponseTimeMs = int(respTime.Int64)
		}
		if errStr.Valid {
			p.Error = errStr.String
		}
		points = append(points, p)
	}
	return points, nil
}

// GetSessionStats computes aggregate statistics for a session.
func (s *Storage) GetSessionStats(sessionID string) (SessionStats, error) {
	var stats SessionStats
	err := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(MIN(timestamp), ''),
			COALESCE(MAX(timestamp), ''),
			MIN(value),
			MAX(value),
			AVG(value),
			AVG(response_time_ms),
			SUM(CASE WHEN error IS NOT NULL AND error != '' THEN 1 ELSE 0 END)
		FROM data_points WHERE session_id = ?
	`, sessionID).Scan(
		&stats.TotalPoints,
		&stats.FirstTimestamp,
		&stats.LastTimestamp,
		&stats.MinValue,
		&stats.MaxValue,
		&stats.AvgValue,
		&stats.AvgLatency,
		&stats.ErrorCount,
	)
	if err != nil {
		return stats, err
	}
	return stats, nil
}

// Cleanup deletes data points older than the given duration. Returns count deleted.
func (s *Storage) Cleanup(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan).UTC().Format(time.RFC3339)
	result, err := s.db.Exec(`DELETE FROM data_points WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	// Also remove sessions with no data points that are inactive
	s.db.Exec(`DELETE FROM sessions WHERE active = 0 AND id NOT IN (SELECT DISTINCT session_id FROM data_points)`)
	return result.RowsAffected()
}

// ImportLocalStorageData imports legacy data from the frontend's localStorage migration.
func (s *Storage) ImportLocalStorageData(sessions []Session, points map[string][]DataPoint) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	sessStmt, err := tx.Prepare(`INSERT OR IGNORE INTO sessions (id, oid, targets, interval_ms, snmp_version, started_at, thresholds, active) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer sessStmt.Close()

	dpStmt, err := tx.Prepare(`INSERT INTO data_points (session_id, target, timestamp, value, delta, rate, response_time_ms, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer dpStmt.Close()

	for _, sess := range sessions {
		targetsJSON, _ := json.Marshal(sess.Targets)
		var thresholdsJSON interface{}
		if sess.Thresholds != nil {
			b, _ := json.Marshal(sess.Thresholds)
			thresholdsJSON = string(b)
		}
		active := 0
		if sess.Active {
			active = 1
		}
		sessStmt.Exec(sess.ID, sess.OID, string(targetsJSON), sess.IntervalMs, sess.SnmpVersion, sess.StartedAt, thresholdsJSON, active)

		if pts, ok := points[sess.ID]; ok {
			for _, p := range pts {
				dpStmt.Exec(p.SessionID, p.Target, p.Timestamp, p.Value, p.Delta, p.Rate, p.ResponseTimeMs, nullableString([]byte(p.Error)))
			}
		}
	}

	return tx.Commit()
}

func nullableString(b []byte) interface{} {
	if len(b) == 0 || string(b) == "" {
		return nil
	}
	return string(b)
}

func timeFilter(from, to string) string {
	s := ""
	if from != "" {
		s += ` AND timestamp >= ?`
	}
	if to != "" {
		s += ` AND timestamp <= ?`
	}
	return s
}
