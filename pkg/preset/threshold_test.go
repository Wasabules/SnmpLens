package preset

import (
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

// yes is an explicit "evaluate this band". Absent means the same thing;
// spelling it out here is what makes the tests that turn it OFF readable.
func yes() *bool { t := true; return &t }

func withThreshold(kind string, oids []string, t *Threshold) Preset {
	return Preset{
		FormatVersion: 1, Name: "Watched", IntervalSec: 60,
		Widgets: []Widget{{Kind: kind, Title: "W", OIDs: oids, Threshold: t}},
	}
}

// A band belongs to the WIDGET and reaches every reading in it: "no port may be
// anything but up" is one statement about however many ports there are.
func TestABandCoversEveryReadingOfItsWidget(t *testing.T) {
	p := withThreshold(KindGrid,
		[]string{"1.3.6.1.2.1.2.2.1.8.1", ".1.3.6.1.2.1.2.2.1.8.2"},
		&Threshold{Max: f(1), ForSeconds: 60, AlertEnabled: yes()})
	p.Widgets[0].Labels = map[string]string{"1": "up", "2": "down"}

	if errs := Validate(p); len(errs) > 0 {
		t.Fatalf("a valid band was refused: %v", errs)
	}
	bands := ThresholdsFor(p)
	if len(bands) != 2 {
		t.Fatalf("%d band(s) for two readings: %v", len(bands), bands)
	}
	// Keyed WITHOUT the leading dot, because that is how the session stores its
	// OIDs and how the evaluator looks a band up. ".1.3.6" and "1.3.6" are one
	// reading, and two entries for it would apply to neither.
	if _, ok := bands["1.3.6.1.2.1.2.2.1.8.2"]; !ok {
		t.Errorf("a leading dot made a second reading: %v", bands)
	}
	if b := bands["1.3.6.1.2.1.2.2.1.8.1"]; b.Max == nil || *b.Max != 1 || !b.Alerts() {
		t.Errorf("the band did not survive: %+v", b)
	}
}

// A band with no bound cannot fire. It is not a default — it is a mistake with
// no symptom at all: the dashboard draws, the session polls, and the thing the
// author wrote it to catch never raises anything.
func TestABandNeedsABound(t *testing.T) {
	p := withThreshold(KindValue, []string{"1.3.6.1.2.1.1.3.0"}, &Threshold{ForSeconds: 30})
	errs := Validate(p)
	if len(errs) != 1 || !strings.HasSuffix(errs[0].Message, "thresholdWithoutBound") {
		t.Fatalf("errors = %v", errs)
	}
	if errs[0].Field != "widgets[0].threshold" {
		t.Errorf("field = %q", errs[0].Field)
	}
}

// A minimum above the maximum is a band no reading can be inside, so every
// sample is a breach.
func TestABandCannotBeInverted(t *testing.T) {
	p := withThreshold(KindValue, []string{"1.3.6.1.2.1.1.3.0"},
		&Threshold{Min: f(90), Max: f(10)})
	errs := Validate(p)
	if len(errs) != 1 || !strings.HasSuffix(errs[0].Message, "thresholdInverted") {
		t.Fatalf("errors = %v", errs)
	}
	// The numbers are in the message: "invalid threshold" leaves the author to
	// work out which of the two they mistyped.
	if errs[0].Args["min"] != "90" || errs[0].Args["max"] != "10" {
		t.Errorf("args = %v", errs[0].Args)
	}
}

// The hold is bounded for the reason the interval is: a hold longer than a day
// is not a hold, it is a threshold that never fires.
func TestTheHoldIsBounded(t *testing.T) {
	for _, hold := range []int{-1, MaxThresholdHoldSec + 1} {
		p := withThreshold(KindValue, []string{"1.3.6.1.2.1.1.3.0"},
			&Threshold{Max: f(10), ForSeconds: hold})
		errs := Validate(p)
		if len(errs) != 1 || errs[0].Field != "widgets[0].threshold.forSeconds" {
			t.Errorf("forSeconds %d gave %v", hold, errs)
		}
	}
	// And the edges are legal: 0 fires on the first sample, which is right for
	// a state, and a day is the longest hold the format admits.
	for _, hold := range []int{0, MaxThresholdHoldSec} {
		p := withThreshold(KindValue, []string{"1.3.6.1.2.1.1.3.0"},
			&Threshold{Max: f(10), ForSeconds: hold})
		if errs := Validate(p); len(errs) > 0 {
			t.Errorf("forSeconds %d was refused: %v", hold, errs)
		}
	}
}

// The one kind that cannot carry a band, and the reason is measured rather than
// stylistic: the evaluator compares Sample.Value, which is the RAW reading. A
// `rate` widget says its OID is a counter, and a counter only goes up — so a
// maximum is breached on the first sample and never recovers, while the tile
// beside it shows the rate the author meant.
func TestABandCannotBePutOnARateWidget(t *testing.T) {
	p := withThreshold(KindRate, []string{"1.3.6.1.2.1.2.2.1.10.1"},
		&Threshold{Max: f(1000000)})
	errs := Validate(p)
	if len(errs) != 1 || !strings.HasSuffix(errs[0].Message, "thresholdOnRate") {
		t.Fatalf("a band on a counter was accepted: %v", errs)
	}
	if errs[0].Args["kind"] != KindRate {
		t.Errorf("args = %v", errs[0].Args)
	}
}

// The session holds ONE map keyed by OID. Two different bands on one reading is
// a question with no answer: whichever is written last would win silently, and
// the other widget would be drawn under a rule not being applied to it.
func TestTwoDifferentBandsOnOneReadingAreRefused(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Clash", IntervalSec: 60,
		Widgets: []Widget{
			{Kind: KindValue, Title: "A", OIDs: []string{"1.3.6.1.2.1.1.3.0"},
				Threshold: &Threshold{Max: f(10)}},
			{Kind: KindChart, Title: "B", OIDs: []string{".1.3.6.1.2.1.1.3.0"},
				Threshold: &Threshold{Max: f(20)}},
		},
	}
	errs := Validate(p)
	if len(errs) != 1 || !strings.HasSuffix(errs[0].Message, "thresholdConflict") {
		t.Fatalf("errors = %v", errs)
	}
	if errs[0].Field != "widgets[1].threshold" || errs[0].Args["other"] != "1" {
		t.Errorf("the conflict does not point anywhere useful: %+v", errs[0])
	}
}

// The same band said twice is one band. Refusing that would make it impossible
// to show a reading in two widgets — a tile and a chart of the same thing is
// the ordinary shape of a dashboard.
func TestTheSameBandTwiceIsFine(t *testing.T) {
	band := func() *Threshold { return &Threshold{Max: f(10), ForSeconds: 30, AlertEnabled: yes()} }
	p := Preset{
		FormatVersion: 1, Name: "Same", IntervalSec: 60,
		Widgets: []Widget{
			{Kind: KindValue, Title: "A", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Threshold: band()},
			{Kind: KindChart, Title: "B", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Threshold: band()},
		},
	}
	if errs := Validate(p); len(errs) > 0 {
		t.Fatalf("one band written twice was refused: %v", errs)
	}
	if len(ThresholdsFor(p)) != 1 {
		t.Errorf("one reading, %d band(s)", len(ThresholdsFor(p)))
	}
}

// A band on a discovering widget is written before its OIDs exist, which is the
// whole reason it is per widget. Expansion is what turns it into readings.
func TestABandOnADiscoveringWidgetLandsOnWhatTheWalkFound(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Ports", IntervalSec: 60,
		Widgets: []Widget{{
			Kind: KindGrid, Title: "Ports",
			OIDs:      []string{"1.3.6.1.2.1.2.2.1.8.{#}"},
			Labels:    map[string]string{"1": "up"},
			Discover:  &Discover{Walk: "1.3.6.1.2.1.2.2.1.2"},
			Threshold: &Threshold{Max: f(1), AlertEnabled: yes()},
		}},
	}
	if errs := Validate(p); len(errs) > 0 {
		t.Fatalf("refused before discovery: %v", errs)
	}
	// Before the walk there is nothing to key a band by, and a map entry for
	// "…8.{#}" would be a threshold on an OID that never arrives.
	if got := ThresholdsFor(p); len(got) != 0 {
		t.Errorf("a template was keyed as a reading: %v", got)
	}

	expanded := Expand(p, map[int][]Instance{
		0: {{Suffix: "1", Label: "eth0"}, {Suffix: "2", Label: "eth1"}},
	})
	bands := ThresholdsFor(expanded)
	if len(bands) != 2 {
		t.Fatalf("%d band(s) after discovering two ports: %v", len(bands), bands)
	}
	for _, oid := range []string{"1.3.6.1.2.1.2.2.1.8.1", "1.3.6.1.2.1.2.2.1.8.2"} {
		if b, ok := bands[oid]; !ok || b.Max == nil || *b.Max != 1 {
			t.Errorf("%s is not watched: %+v", oid, b)
		}
	}
}

// What binding will do is stated before it does it, and the figure that matters
// is the one that leaves the machine.
func TestTheCostCountsWhatIsWatchedAndWhatAlerts(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Mixed", IntervalSec: 60,
		Widgets: []Widget{
			{Kind: KindValue, Title: "Uptime", OIDs: []string{"1.3.6.1.2.1.1.3.0"}},
			// Explicitly not evaluated: drawn as a reference line and nothing
			// more. Omitting the flag would mean the opposite.
			{Kind: KindStatus, Title: "Battery", OIDs: []string{"1.3.6.1.2.1.33.1.2.1.0"},
				Labels:    map[string]string{"1": "unknown"},
				Threshold: &Threshold{Max: f(2), AlertEnabled: new(bool)}},
			{Kind: KindChart, Title: "Load", OIDs: []string{"1.3.6.1.2.1.25.3.3.1.2.1", "1.3.6.1.2.1.25.3.3.1.2.2"},
				Threshold: &Threshold{Max: f(90), AlertEnabled: yes()}},
		},
	}
	c := Estimate(p)
	if c.OIDs != 4 {
		t.Errorf("oids = %d, want 4", c.OIDs)
	}
	if c.Watched != 3 {
		t.Errorf("watched = %d, want 3 (one state and two processors)", c.Watched)
	}
	if c.Alerting != 2 {
		t.Errorf("alerting = %d, want 2 (the chart, on two processors; the state says no)", c.Alerting)
	}
}

// A discovering widget's band is counted at the SAME ceiling its OIDs are, or
// the one screen that says what binding will do would report two thresholds for
// a preset that is about to create ninety-six.
func TestAWatchedCountIsACeilingToo(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Ports", IntervalSec: 60,
		Widgets: []Widget{{
			Kind: KindGrid, Title: "Ports",
			OIDs:      []string{"1.3.6.1.2.1.2.2.1.8.{#}"},
			Labels:    map[string]string{"1": "up"},
			Discover:  &Discover{Walk: "1.3.6.1.2.1.2.2.1.2"},
			Threshold: &Threshold{Max: f(1), AlertEnabled: yes()},
		}},
	}
	c := Estimate(p)
	if c.Watched != c.OIDs || c.Watched == 0 {
		t.Fatalf("watched = %d for %d OID(s)", c.Watched, c.OIDs)
	}
	if c.Alerting != c.Watched {
		t.Errorf("alerting = %d, want all %d", c.Alerting, c.Watched)
	}
	if c.Discovered != c.OIDs {
		t.Errorf("discovered = %d of %d", c.Discovered, c.OIDs)
	}
}

// An omitted alertEnabled means YES, and that is the whole reason the field is
// a pointer.
//
// encoding/json decodes an absent bool to false, and false is not "notify me
// less" — classify() returns nothing without it, in Go and in the renderer
// alike, so the band opens no episode and records nothing at all. A plain bool
// would make every threshold written the obvious way inert, with the dashboard
// looking exactly as though it were armed. That failure has no symptom: the
// preset validates, the session polls, and the incident it was written to catch
// simply never arrives.
func TestAnOmittedAlertFlagStillEvaluates(t *testing.T) {
	var quiet = false
	cases := []struct {
		why    string
		flag   *bool
		alerts bool
	}{
		{"omitted, as an author would write it", nil, true},
		{"explicitly on", yes(), true},
		{"explicitly off — a reference line and nothing more", &quiet, false},
	}
	for _, c := range cases {
		p := withThreshold(KindValue, []string{"1.3.6.1.2.1.1.3.0"},
			&Threshold{Max: f(10), AlertEnabled: c.flag})
		if errs := Validate(p); len(errs) > 0 {
			t.Fatalf("%s: refused: %v", c.why, errs)
		}
		if got := ThresholdsFor(p)["1.3.6.1.2.1.1.3.0"].Alerts(); got != c.alerts {
			t.Errorf("%s: Alerts() = %v, want %v", c.why, got, c.alerts)
		}
		// And the figure the operator is shown before binding follows it.
		c2 := Estimate(p)
		want := 0
		if c.alerts {
			want = 1
		}
		if c2.Alerting != want || c2.Watched != 1 {
			t.Errorf("%s: watched %d, alerting %d", c.why, c2.Watched, c2.Alerting)
		}
	}
}
