package preset

import (
	"strconv"
	"strings"
	"testing"
)

func valid() Preset {
	return Preset{
		FormatVersion: FormatVersion,
		Name:          "Cisco Catalyst",
		Author:        "someone",
		IntervalSec:   30,
		Match:         Match{SysObjectIDPrefix: []string{"1.3.6.1.4.1.9"}, Vendor: "Cisco"},
		Widgets: []Widget{
			{Kind: KindValue, Title: "Uptime", OIDs: []string{"1.3.6.1.2.1.1.3.0"}},
			{Kind: KindChart, Title: "Traffic", OIDs: []string{"1.3.6.1.2.1.2.2.1.10.1", "1.3.6.1.2.1.2.2.1.16.1"}},
			{Kind: KindStatus, Title: "Link", OIDs: []string{"1.3.6.1.2.1.2.2.1.8.1"},
				Labels: map[string]string{"1": "up", "2": "down"}},
		},
	}
}

func fieldsOf(errs []Error) string {
	var f []string
	for _, e := range errs {
		f = append(f, e.Field)
	}
	return strings.Join(f, ", ")
}

func has(errs []Error, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

func TestAWellFormedPresetIsAccepted(t *testing.T) {
	if errs := Validate(valid()); len(errs) != 0 {
		t.Fatalf("a valid preset was refused: %s", fieldsOf(errs))
	}
}

// A preset chooses a widget from the vocabulary. It cannot describe one.
//
// This is the whole reason the package exists, and it is the same answer
// pkg/notify/template.go gave for message templates: a fixed set that can be
// listed and validated beats a language that has to be interpreted.
func TestAPresetCannotInventAWidget(t *testing.T) {
	p := valid()
	p.Widgets[0].Kind = "iframe"
	errs := Validate(p)
	if !has(errs, "widgets[0].kind") {
		t.Fatalf("an unknown widget kind was accepted: %s", fieldsOf(errs))
	}
}

// The vocabulary the UI is served must be the vocabulary the validator
// enforces. A hand-copied list is a list that drifts, and the first symptom
// would be a kind the editor offers and the validator rejects.
func TestTheServedVocabularyIsTheEnforcedOne(t *testing.T) {
	docs := WidgetKinds()
	if len(docs) == 0 {
		t.Fatal("no widget kinds are served")
	}
	for _, d := range docs {
		p := valid()
		p.Widgets = []Widget{{Kind: d.Kind, Title: "x", OIDs: []string{"1.3.6.1.2.1.1.3.0"}}}
		if errs := Validate(p); has(errs, "widgets[0].kind") {
			t.Errorf("%s is offered to the UI and refused by the validator", d.Kind)
		}
		// The description is an i18n key, not English. Prose here is how the
		// vocabulary becomes the one part of the app that never translates.
		if !strings.HasPrefix(d.Description, "preset.widget.") {
			t.Errorf("%s: description %q is not an i18n key", d.Kind, d.Description)
		}
	}
	// And the served copy cannot be used to edit the real one.
	docs[0].Kind = "tampered"
	if WidgetKinds()[0].Kind == "tampered" {
		t.Error("WidgetKinds hands out the underlying slice")
	}
}

// A preset carries numeric OIDs. A name would have to be resolved, which means
// taking pkg/mib's exclusive gosmi lock to validate a FILE, and would make what
// a preset polls depend on which MIBs happen to be loaded.
func TestOnlyNumericOidsAreAccepted(t *testing.T) {
	bad := []string{
		"sysUpTime.0", "IF-MIB::ifInOctets.1", "", "1.3.6.1.2.1.1.3.",
		"1.3.6.1..2", "1", "abc", "1.3.6.1.2.1.1.3.0; rm -rf /",
	}
	for _, oid := range bad {
		p := valid()
		p.Widgets[0].OIDs = []string{oid}
		if errs := Validate(p); !has(errs, "widgets[0].oids[0]") {
			t.Errorf("%q was accepted as an OID: %s", oid, fieldsOf(errs))
		}
	}
	for _, oid := range []string{"1.3.6.1.2.1.1.3.0", ".1.3.6.1.2.1.1.3.0", "1.3"} {
		p := valid()
		p.Widgets[0].OIDs = []string{oid}
		if errs := Validate(p); has(errs, "widgets[0].oids[0]") {
			t.Errorf("%q is a valid OID and was refused", oid)
		}
	}
}

// The cadence is REQUIRED, not defaulted. A preset that does not say how often
// it polls cannot have its cost stated before it runs, which is the whole point
// of naming the cost at bind time.
func TestTheCadenceMustBeDeclared(t *testing.T) {
	p := valid()
	p.IntervalSec = 0
	if errs := Validate(p); !has(errs, "intervalSec") {
		t.Fatal("a preset with no cadence was accepted; its cost cannot be stated")
	}
	for _, iv := range []int{1, MinIntervalSec - 1, MaxIntervalSec + 1, -30} {
		p := valid()
		p.IntervalSec = iv
		if errs := Validate(p); !has(errs, "intervalSec") {
			t.Errorf("intervalSec %d was accepted", iv)
		}
	}
}

// Text a stranger wrote reaches a panel, a log line, and through the event
// journal a syslog collector. pkg/notify escapes control characters on the way
// out; refusing them on the way IN means they never travel.
func TestAuthorSuppliedTextIsBounded(t *testing.T) {
	p := valid()
	p.Widgets[0].Title = strings.Repeat("x", MaxTextLen+1)
	if errs := Validate(p); !has(errs, "widgets[0].title") {
		t.Error("an over-long title was accepted")
	}

	for _, s := range []string{"up\nBcc: x", "a\rb", "tab\there", "nul\x00"} {
		p := valid()
		p.Name = s
		if errs := Validate(p); !has(errs, "name") {
			t.Errorf("%q was accepted as a name", s)
		}
	}
}

// A file from a version that has moved on is refused with a sentence rather
// than misread.
func TestAFutureFormatIsRefused(t *testing.T) {
	p := valid()
	p.FormatVersion = FormatVersion + 1
	if errs := Validate(p); !has(errs, "formatVersion") {
		t.Error("a newer format version was accepted")
	}
	p.FormatVersion = 0
	if errs := Validate(p); !has(errs, "formatVersion") {
		t.Error("a missing format version was accepted")
	}
}

// Every problem at once. A preset is edited in a text editor by someone who is
// not looking at this code; one error per attempt turns a five-minute fix into
// five round trips.
func TestValidateReportsEverythingAtOnce(t *testing.T) {
	p := Preset{
		FormatVersion: FormatVersion,
		IntervalSec:   1,
		Widgets: []Widget{
			{Kind: "nope", Title: "a", OIDs: []string{"sysUpTime.0"}},
			{Kind: KindValue, Title: "b", OIDs: []string{}},
		},
	}
	errs := Validate(p)
	for _, want := range []string{"name", "intervalSec", "widgets[0].kind", "widgets[0].oids[0]", "widgets[1].oids"} {
		if !has(errs, want) {
			t.Errorf("%s was not reported; got: %s", want, fieldsOf(errs))
		}
	}
}

// A widget kind that shows one reading cannot be handed eight.
func TestAKindsOidCountIsEnforced(t *testing.T) {
	p := valid()
	p.Widgets[0] = Widget{Kind: KindValue, Title: "x", OIDs: []string{
		"1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.1.5.0",
	}}
	if errs := Validate(p); !has(errs, "widgets[0].oids") {
		t.Error("a single-reading widget accepted two OIDs")
	}
}

// Labels map a number to a name for a status tile. On any other kind they are
// a sign the author expected something this vocabulary does not do, and
// silence would leave them wondering why nothing happened.
func TestLabelsBelongOnlyToStatus(t *testing.T) {
	p := valid()
	p.Widgets[0].Labels = map[string]string{"1": "up"}
	if errs := Validate(p); !has(errs, "widgets[0].labels") {
		t.Error("labels on a value widget were accepted silently")
	}
}

// The cost is arithmetic that can be shown BEFORE anything is polled, and it
// counts what the device is actually asked for.
func TestTheCostIsStatedInVarbinds(t *testing.T) {
	p := valid() // 4 OIDs across 3 widgets, every 30 s
	c := Estimate(p)

	if c.OIDs != 4 {
		t.Errorf("OIDs = %d, want 4", c.OIDs)
	}
	if c.Widgets != 3 {
		t.Errorf("Widgets = %d, want 3", c.Widgets)
	}
	if c.RequestsPerHour != 120 {
		t.Errorf("RequestsPerHour = %d, want 120", c.RequestsPerHour)
	}
	// One request carries every OID, so the request count alone understates
	// what the agent does work for.
	if c.VarbindsPerHour != 480 {
		t.Errorf("VarbindsPerHour = %d, want 480", c.VarbindsPerHour)
	}
}

// Two widgets showing the same OID are polled once. Counting it twice would
// overstate the cost of exactly the presets that are well written.
func TestTheSameOidInTwoWidgetsIsPolledOnce(t *testing.T) {
	p := valid()
	p.Widgets = []Widget{
		{Kind: KindValue, Title: "a", OIDs: []string{"1.3.6.1.2.1.1.3.0"}},
		{Kind: KindChart, Title: "b", OIDs: []string{".1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.1.5.0"}},
	}
	if got := Estimate(p).OIDs; got != 2 {
		t.Errorf("cost counts %d OIDs; the leading dot is the same OID", got)
	}
	poll := PollOIDs(p)
	if len(poll) != 2 {
		t.Fatalf("PollOIDs returned %d: %v", len(poll), poll)
	}
	for _, oid := range poll {
		if strings.HasPrefix(oid, ".") {
			t.Errorf("%q keeps its leading dot; the scheduler would ask twice for one OID", oid)
		}
	}
	// Stable order, so a bound preset polls the same way on every run.
	for i := 0; i < 5; i++ {
		again := PollOIDs(p)
		for j := range poll {
			if again[j] != poll[j] {
				t.Fatalf("PollOIDs is not stable: %v then %v", poll, again)
			}
		}
	}
}

// The sanity bounds are sanity bounds, and must sit far past any real preset —
// a limit that bites a legitimate file would be a policy, and the policy lives
// with the measured cycle time instead.
func TestTheSanityBoundsDoNotBiteARealPreset(t *testing.T) {
	p := valid()
	// A generous chassis view: 48 ports, four readings each.
	p.Widgets = nil
	for i := 1; i <= 48; i++ {
		p.Widgets = append(p.Widgets, Widget{
			Kind: KindChart, Title: "port", OIDs: []string{
				"1.3.6.1.2.1.2.2.1.10." + strconv.Itoa(i), "1.3.6.1.2.1.2.2.1.16." + strconv.Itoa(i),
			},
		})
	}
	errs := Validate(p)
	// 48 widgets is past MaxWidgets, which is the honest answer: a 48-tile
	// panel is a different feature. What must NOT happen is the OID count
	// being the thing that refuses it.
	if has(errs, "widgets") {
		for _, e := range errs {
			if e.Field == "widgets" && strings.HasSuffix(e.Message, "tooManyOids") {
				t.Errorf("96 OIDs hit the OID bound; it is meant to be far past any real preset")
			}
		}
	}
}
