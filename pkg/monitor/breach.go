// Package monitor turns polling samples into incidents.
//
// An incident is an episode, not a sample: "below 5" is often one noisy poll,
// while "below 5 for 30s" is something worth waking someone for. So a breach is
// tracked from the moment the value leaves its band, becomes an alert only once
// it has held for the configured dwell time, and — crucially — is CLOSED with a
// matching resolution event when the value comes back.
//
// That closing event is what makes forwarding usable: an alert stream that can
// only ever say FIRED and never RECOVERED is a spam cannon, not an alerting
// system.
package monitor

import (
	"fmt"
	"sync"
	"time"

	"SnmpLens/pkg/events"
)

// Threshold is the alert band for one OID.
type Threshold struct {
	Min          *float64 `json:"min,omitempty"`
	Max          *float64 `json:"max,omitempty"`
	ForSeconds   int      `json:"forSeconds,omitempty"`
	AlertEnabled bool     `json:"alertEnabled"`
}

// Sample is one polled value. Value is nil when the poll failed or returned
// something non-numeric; Error carries why.
type Sample struct {
	Target    string   `json:"target"`
	OID       string   `json:"oid"`
	Timestamp string   `json:"timestamp"`
	Value     *float64 `json:"value"`
	Error     string   `json:"error,omitempty"`
}

// EpisodeRecord is an in-progress breach, persisted so a restart resumes it
// instead of re-raising an incident that was already reported.
type EpisodeRecord struct {
	DedupKey  string `json:"dedupKey"`
	Kind      string `json:"kind"`
	SessionID string `json:"sessionId"`
	Target    string `json:"target"`
	OID       string `json:"oid"`
	FirstSeen string `json:"firstSeen"`
	FiredAt   string `json:"firedAt"`
	CorrID    string `json:"corrId"`
}

// Evaluator holds episode state and emits events. Its dependencies are plain
// function values so this package needs no knowledge of storage.
type Evaluator struct {
	Record     func(events.Event, string) error
	SaveEpi    func(EpisodeRecord) error
	DeleteEpi  func(dedupKey string) error
	NewCorrID  func() string
	mu         sync.Mutex
	episodes   map[string]*EpisodeRecord
	restored   bool
	downStates map[string]bool // session|target -> currently unreachable
}

// NewEvaluator returns a ready evaluator. Missing hooks are replaced by no-ops
// so an evaluator is always safe to call.
func NewEvaluator() *Evaluator {
	return &Evaluator{
		Record:     func(events.Event, string) error { return nil },
		SaveEpi:    func(EpisodeRecord) error { return nil },
		DeleteEpi:  func(string) error { return nil },
		NewCorrID:  func() string { return fmt.Sprintf("corr-%d", time.Now().UnixNano()) },
		episodes:   map[string]*EpisodeRecord{},
		downStates: map[string]bool{},
	}
}

// Restore seeds in-memory state from persisted episodes. Without this, an
// already-reported incident re-fires as new on every restart.
func (e *Evaluator) Restore(records []EpisodeRecord) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range records {
		r := records[i]
		e.episodes[r.DedupKey] = &r
	}
	e.restored = true
}

func thresholdKey(sessionID, target, oid string) string {
	return "threshold|" + sessionID + "|" + target + "|" + oid
}

func reachKey(sessionID, target string) string {
	return "reach|" + sessionID + "|" + target
}

// classify reports which bound the value is outside of, if any.
func classify(value *float64, th *Threshold) (kind string, bound float64, ok bool) {
	if th == nil || !th.AlertEnabled || value == nil {
		return "", 0, false
	}
	if th.Min != nil && *value < *th.Min {
		return "below", *th.Min, true
	}
	if th.Max != nil && *value > *th.Max {
		return "above", *th.Max, true
	}
	return "", 0, false
}

// Ingest evaluates one poll's worth of samples for a session and emits any
// resulting events. thresholds is keyed by OID.
func (e *Evaluator) Ingest(sessionID, sessionName string, samples []Sample, thresholds map[string]*Threshold) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// --- Reachability, per target ---
	// A target that stops answering entirely is invisible to threshold
	// alerting: with no value there is nothing to compare, so the operator
	// would see silence rather than an incident.
	perTarget := map[string]struct{ total, failed int }{}
	for _, s := range samples {
		st := perTarget[s.Target]
		st.total++
		if s.Value == nil || s.Error != "" {
			st.failed++
		}
		perTarget[s.Target] = st
	}
	for target, st := range perTarget {
		if st.total == 0 {
			continue
		}
		key := reachKey(sessionID, target)
		down := st.failed == st.total
		if down && !e.downStates[key] {
			e.downStates[key] = true
			note(e.emitReachability(sessionID, sessionName, target, false, samples))
		} else if !down && e.downStates[key] {
			delete(e.downStates, key)
			note(e.emitReachability(sessionID, sessionName, target, true, samples))
		}
	}

	// --- Threshold episodes, per (target, OID) ---
	for _, s := range samples {
		th := thresholds[s.OID]
		key := thresholdKey(sessionID, s.Target, s.OID)
		kind, bound, breached := classify(s.Value, th)

		if !breached {
			// Back inside the band (or unreadable): close any open episode and
			// say so, once.
			if epi, ok := e.episodes[key]; ok {
				if epi.FiredAt != "" && s.Value != nil {
					note(e.emitThresholdResolved(sessionID, sessionName, s, epi))
				}
				delete(e.episodes, key)
				note(e.DeleteEpi(key))
			}
			continue
		}

		at, err := time.Parse(time.RFC3339, s.Timestamp)
		if err != nil {
			at = time.Now().UTC()
		}

		epi, ok := e.episodes[key]
		// A flip from below to above starts a new episode, not a continuation.
		if !ok || epi.Kind != kind {
			epi = &EpisodeRecord{
				DedupKey:  key,
				Kind:      kind,
				SessionID: sessionID,
				Target:    s.Target,
				OID:       s.OID,
				FirstSeen: at.UTC().Format(time.RFC3339),
				CorrID:    e.NewCorrID(),
			}
			e.episodes[key] = epi
			note(e.SaveEpi(*epi))
		}

		if epi.FiredAt != "" {
			continue // already raised; wait for it to clear
		}

		first, err := time.Parse(time.RFC3339, epi.FirstSeen)
		if err != nil {
			first = at
		}
		held := at.Sub(first)
		if held < time.Duration(th.ForSeconds)*time.Second {
			continue // not sustained long enough yet
		}

		epi.FiredAt = at.UTC().Format(time.RFC3339)
		note(e.SaveEpi(*epi))
		note(e.emitThresholdOpened(sessionID, sessionName, s, epi, bound, th, int(held.Seconds())))
	}

	return firstErr
}

func (e *Evaluator) emitThresholdOpened(sessionID, sessionName string, s Sample, epi *EpisodeRecord, bound float64, th *Threshold, heldSeconds int) error {
	value := *s.Value
	ev := events.Event{
		Ts:          s.Timestamp,
		Category:    events.CategoryThreshold,
		Kind:        events.KindThresholdOpened,
		Severity:    events.SevMajor.String(),
		State:       events.StateOpen,
		Source:      s.Target,
		OID:         s.OID,
		SessionID:   sessionID,
		SessionName: sessionName,
		DedupKey:    epi.DedupKey,
		CorrID:      epi.CorrID,
		TitleKey:    "events.kind." + events.KindThresholdOpened,
		Params: map[string]any{
			"target":      s.Target,
			"oid":         s.OID,
			"kind":        epi.Kind,
			"bound":       bound,
			"value":       value,
			"heldSeconds": heldSeconds,
			"forSeconds":  th.ForSeconds,
		},
		Summary: fmt.Sprintf("%s on %s is %s %g (value %g, held %ds)",
			s.OID, s.Target, epi.Kind, bound, value, heldSeconds),
		Value: &value,
	}
	return e.Record(ev, "")
}

func (e *Evaluator) emitThresholdResolved(sessionID, sessionName string, s Sample, epi *EpisodeRecord) error {
	value := *s.Value
	ev := events.Event{
		Ts:          s.Timestamp,
		Category:    events.CategoryThreshold,
		Kind:        events.KindThresholdResolved,
		Severity:    events.SevInfo.String(),
		State:       events.StateResolved,
		Source:      s.Target,
		OID:         s.OID,
		SessionID:   sessionID,
		SessionName: sessionName,
		DedupKey:    epi.DedupKey,
		CorrID:      epi.CorrID, // ties this back to the opening event
		TitleKey:    "events.kind." + events.KindThresholdResolved,
		Params: map[string]any{
			"target": s.Target,
			"oid":    s.OID,
			"value":  value,
		},
		Summary: fmt.Sprintf("%s on %s is back within range (value %g)", s.OID, s.Target, value),
		Value:   &value,
	}
	return e.Record(ev, "")
}

func (e *Evaluator) emitReachability(sessionID, sessionName, target string, up bool, samples []Sample) error {
	kind := events.KindReachabilityDown
	severity := events.SevMajor.String()
	state := events.StateOpen
	summary := target + " stopped responding"
	if up {
		kind = events.KindReachabilityUp
		severity = events.SevInfo.String()
		state = events.StateResolved
		summary = target + " is responding again"
	}

	reason := ""
	for _, s := range samples {
		if s.Target == target && s.Error != "" {
			reason = s.Error
			break
		}
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	for _, s := range samples {
		if s.Target == target && s.Timestamp != "" {
			ts = s.Timestamp
			break
		}
	}

	ev := events.Event{
		Ts:          ts,
		Category:    events.CategoryReachability,
		Kind:        kind,
		Severity:    severity,
		State:       state,
		Source:      target,
		SessionID:   sessionID,
		SessionName: sessionName,
		DedupKey:    reachKey(sessionID, target),
		TitleKey:    "events.kind." + kind,
		Params:      map[string]any{"target": target, "error": reason},
		Summary:     summary,
	}
	return e.Record(ev, "")
}
