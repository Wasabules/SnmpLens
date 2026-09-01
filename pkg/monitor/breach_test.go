package monitor

import (
	"testing"
	"time"

	"SnmpLens/pkg/events"
)

type harness struct {
	*Evaluator
	recorded []events.Event
	saved    []EpisodeRecord
	deleted  []string
}

func newHarness() *harness {
	h := &harness{Evaluator: NewEvaluator()}
	h.Record = func(e events.Event, _ string) error {
		h.recorded = append(h.recorded, e)
		return nil
	}
	h.SaveEpi = func(r EpisodeRecord) error { h.saved = append(h.saved, r); return nil }
	h.DeleteEpi = func(k string) error { h.deleted = append(h.deleted, k); return nil }
	h.NewCorrID = func() string { return "corr-fixed" }
	return h
}

func (h *harness) kinds() []string {
	out := make([]string, 0, len(h.recorded))
	for _, e := range h.recorded {
		out = append(out, e.Kind)
	}
	return out
}

func at(sec int) string {
	return time.Date(2026, 9, 1, 10, 0, sec, 0, time.UTC).Format(time.RFC3339)
}

func f(v float64) *float64 { return &v }

func maxThreshold(max float64, forSeconds int) map[string]*Threshold {
	return map[string]*Threshold{"oid1": {Max: f(max), ForSeconds: forSeconds, AlertEnabled: true}}
}

func sample(sec int, value *float64) []Sample {
	return []Sample{{Target: "10.0.0.1", OID: "oid1", Timestamp: at(sec), Value: value}}
}

// The dwell time is the whole point: a single sample outside the band is noise,
// the same condition held for 30s is an incident.
func TestThresholdFiresOnlyAfterDwellAndOncePerEpisode(t *testing.T) {
	h := newHarness()
	th := maxThreshold(100, 30)

	h.Ingest("s1", "session", sample(0, f(50)), th)   // inside
	h.Ingest("s1", "session", sample(10, f(150)), th) // breach starts
	h.Ingest("s1", "session", sample(20, f(150)), th) // 10s held
	h.Ingest("s1", "session", sample(30, f(150)), th) // 20s held

	if len(h.recorded) != 0 {
		t.Fatalf("alerted before the dwell elapsed: %v", h.kinds())
	}

	h.Ingest("s1", "session", sample(45, f(150)), th) // 35s >= 30s
	if got := h.kinds(); len(got) != 1 || got[0] != events.KindThresholdOpened {
		t.Fatalf("expected one threshold.opened, got %v", got)
	}

	h.Ingest("s1", "session", sample(50, f(150)), th) // still out
	if len(h.recorded) != 1 {
		t.Fatalf("a sustained breach logged twice: %v", h.kinds())
	}
}

// An alert stream that can only say FIRED and never RECOVERED is unusable for
// on-call. The resolution must carry the SAME correlation id as its opening.
func TestThresholdResolvesAndCorrelates(t *testing.T) {
	h := newHarness()
	th := maxThreshold(100, 0)

	h.Ingest("s1", "session", sample(0, f(150)), th) // fires immediately (dwell 0)
	h.Ingest("s1", "session", sample(5, f(50)), th)  // back inside

	got := h.kinds()
	if len(got) != 2 || got[0] != events.KindThresholdOpened || got[1] != events.KindThresholdResolved {
		t.Fatalf("expected opened then resolved, got %v", got)
	}
	if h.recorded[0].CorrID == "" || h.recorded[0].CorrID != h.recorded[1].CorrID {
		t.Errorf("resolution not correlated: %q vs %q", h.recorded[0].CorrID, h.recorded[1].CorrID)
	}
	if h.recorded[0].State != events.StateOpen || h.recorded[1].State != events.StateResolved {
		t.Errorf("states = %q / %q", h.recorded[0].State, h.recorded[1].State)
	}
	if len(h.deleted) != 1 {
		t.Errorf("episode not closed in the store: %v", h.deleted)
	}
}

func TestNoResolutionWithoutAnOpening(t *testing.T) {
	h := newHarness()
	th := maxThreshold(100, 30)

	h.Ingest("s1", "session", sample(0, f(150)), th) // breach starts, never fires
	h.Ingest("s1", "session", sample(5, f(50)), th)  // clears early

	if len(h.recorded) != 0 {
		t.Fatalf("a breach that never alerted produced events: %v", h.kinds())
	}
}

// A target that goes completely dark has no value to compare, so threshold
// alerting is blind to it — reachability is what makes it visible.
func TestReachabilityDownAndUp(t *testing.T) {
	h := newHarness()
	th := maxThreshold(100, 0)

	h.Ingest("s1", "session", sample(0, f(50)), th)
	if len(h.recorded) != 0 {
		t.Fatalf("unexpected events while healthy: %v", h.kinds())
	}

	dead := []Sample{{Target: "10.0.0.1", OID: "oid1", Timestamp: at(10), Value: nil, Error: "timeout"}}
	h.Ingest("s1", "session", dead, th)
	h.Ingest("s1", "session", dead, th) // still dark: must not repeat

	if got := h.kinds(); len(got) != 1 || got[0] != events.KindReachabilityDown {
		t.Fatalf("expected exactly one reachability.down, got %v", got)
	}
	if h.recorded[0].Params["error"] != "timeout" {
		t.Errorf("failure reason not carried: %+v", h.recorded[0].Params)
	}

	h.Ingest("s1", "session", sample(20, f(50)), th)
	if got := h.kinds(); len(got) != 2 || got[1] != events.KindReachabilityUp {
		t.Fatalf("expected a recovery, got %v", got)
	}
}

// Below -> above is a different incident, not a continuation of the first.
func TestFlippingDirectionStartsANewEpisode(t *testing.T) {
	h := newHarness()
	th := map[string]*Threshold{"oid1": {Min: f(10), Max: f(100), ForSeconds: 0, AlertEnabled: true}}

	h.Ingest("s1", "session", sample(0, f(5)), th)   // below
	h.Ingest("s1", "session", sample(5, f(500)), th) // above

	got := h.kinds()
	if len(got) != 2 {
		t.Fatalf("expected two openings, got %v", got)
	}
	if h.recorded[0].Params["kind"] != "below" || h.recorded[1].Params["kind"] != "above" {
		t.Errorf("directions = %v / %v", h.recorded[0].Params["kind"], h.recorded[1].Params["kind"])
	}
}

// A restart must not re-announce an incident that was already reported.
func TestRestoreSuppressesAlreadyFiredEpisode(t *testing.T) {
	h := newHarness()
	th := maxThreshold(100, 0)

	h.Restore([]EpisodeRecord{{
		DedupKey:  thresholdKey("s1", "10.0.0.1", "oid1"),
		Kind:      "above",
		SessionID: "s1",
		Target:    "10.0.0.1",
		OID:       "oid1",
		FirstSeen: at(0),
		FiredAt:   at(1),
		CorrID:    "corr-old",
	}})

	h.Ingest("s1", "session", sample(30, f(150)), th) // still breaching
	if len(h.recorded) != 0 {
		t.Fatalf("re-announced a restored incident: %v", h.kinds())
	}

	h.Ingest("s1", "session", sample(40, f(50)), th) // recovers
	got := h.kinds()
	if len(got) != 1 || got[0] != events.KindThresholdResolved {
		t.Fatalf("expected the restored incident to resolve, got %v", got)
	}
	if h.recorded[0].CorrID != "corr-old" {
		t.Errorf("resolution lost the original correlation id: %q", h.recorded[0].CorrID)
	}
}

func TestDisabledThresholdNeverAlerts(t *testing.T) {
	h := newHarness()
	th := map[string]*Threshold{"oid1": {Max: f(100), AlertEnabled: false}}

	h.Ingest("s1", "session", sample(0, f(9999)), th)
	if len(h.recorded) != 0 {
		t.Fatalf("alerted on a disabled threshold: %v", h.kinds())
	}
}
