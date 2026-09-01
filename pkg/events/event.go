// Package events defines the vocabulary of the event journal: what an event is,
// how it is categorised and how severe it is.
//
// It performs no I/O and depends on nothing else in the project. That is the
// point: pkg/snmp and pkg/monitor emit events through the Recorder interface
// declared here, so neither has to import pkg/storage or pkg/notify. app.go
// wires the real implementations together.
//
// The journal is deliberately separate from the SNMP query history. Query
// history records what the operator ASKED a device to do; the journal records
// what HAPPENED — traps arriving, thresholds breached, targets going dark.
// Only the second kind is worth forwarding to syslog or email, and only the
// second kind must keep working with no window open.
package events

// Event is one journal entry. JSON tags are lowerCamelCase to match the rest of
// the Wails bridge, since the TypeScript models are generated from these structs.
type Event struct {
	Seq         int64          `json:"seq"` // monotonic; also the keyset cursor
	ID          string         `json:"id"`  // UUID, echoed to every sink for dedup
	Ts          string         `json:"ts"`  // RFC3339 UTC
	Category    string         `json:"category"`
	Kind        string         `json:"kind"`
	Severity    string         `json:"severity"` // X.733 name on the wire, INTEGER in SQL
	State       string         `json:"state"`
	Source      string         `json:"source,omitempty"` // target address; empty for app-level events
	OID         string         `json:"oid,omitempty"`
	SessionID   string         `json:"sessionId,omitempty"`
	SessionName string         `json:"sessionName,omitempty"` // denormalised: sessions get reaped
	DedupKey    string         `json:"dedupKey,omitempty"`
	CorrID      string         `json:"corrId,omitempty"` // ties a .resolved back to its .opened
	TitleKey    string         `json:"titleKey"`         // i18n key, rendered by the UI
	Params      map[string]any `json:"params,omitempty"` // values for TitleKey
	Summary     string         `json:"summary"`          // English render at insert: search target + fallback
	Value       *float64       `json:"value,omitempty"`
	PayloadSize int            `json:"payloadSize"`
	Acked       bool           `json:"acked"`
}

// Filter selects a page of events. An empty field means "no constraint".
type Filter struct {
	Categories  []string `json:"categories,omitempty"`
	Kinds       []string `json:"kinds,omitempty"`
	MinSeverity string   `json:"minSeverity,omitempty"`
	Source      string   `json:"source,omitempty"`
	OID         string   `json:"oid,omitempty"`
	SessionID   string   `json:"sessionId,omitempty"`
	From        string   `json:"from,omitempty"`
	To          string   `json:"to,omitempty"`
	Search      string   `json:"search,omitempty"`
	UnackedOnly bool     `json:"unackedOnly,omitempty"`
	Limit       int      `json:"limit,omitempty"`     // default 100, capped at 500
	BeforeSeq   int64    `json:"beforeSeq,omitempty"` // keyset cursor; 0 = newest page
}

// Page is one screenful of journal, newest first.
type Page struct {
	// Items is never nil: a nil slice crosses the bridge as JSON null, which
	// the frontend would have to guard on every access.
	Items      []Event `json:"items"`
	NextCursor int64   `json:"nextCursor"` // pass as BeforeSeq for the next page; 0 = end
	Total      int     `json:"total"`      // total matching the filter, ignoring pagination
}

// Counts drives the unacknowledged badge without fetching any rows.
type Counts struct {
	Unacked         int            `json:"unacked"`
	UnackedBySev    map[string]int `json:"unackedBySeverity"`
	UnackedByCatego map[string]int `json:"unackedByCategory"`
}
