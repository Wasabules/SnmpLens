package preset

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
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
	// A grid is a wall of states, so it takes them too.
	p2 := valid()
	p2.Widgets[0] = Widget{Kind: KindGrid, Title: "Ports", OIDs: []string{"1.3.6.1.2.1.2.2.1.8.1"},
		Labels: map[string]string{"1": "up"}}
	if errs := Validate(p2); has(errs, "widgets[0].labels") {
		t.Error("labels on a grid widget were refused")
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
	if c.PollsPerDay != 2880 {
		t.Errorf("PollsPerDay = %d, want 2880", c.PollsPerDay)
	}
	// One round carries every OID, so counting rounds alone understates what
	// the agent does work for.
	if c.VarbindsPerDay != 11520 {
		t.Errorf("VarbindsPerDay = %d, want 11520", c.VarbindsPerDay)
	}
}

// The screen whose whole job is to say what a preset will cost must not answer
// "nothing" for a preset that polls slowly.
//
// An hourly base is integer-divided to ZERO for every cadence past 3600 s, and
// MaxIntervalSec is 86400 — so a third of the legal range reported no cost at
// all, on the one number an operator is shown before agreeing to it.
func TestACadenceLongerThanAnHourStillHasACost(t *testing.T) {
	for _, iv := range []int{3601, 7200, 43200, MaxIntervalSec} {
		p := valid()
		p.IntervalSec = iv
		if errs := Validate(p); len(errs) != 0 {
			t.Fatalf("interval %d is legal and was refused: %s", iv, fieldsOf(errs))
		}
		c := Estimate(p)
		if c.PollsPerDay < 1 || c.VarbindsPerDay < 1 {
			t.Errorf("interval %d reports %d polls and %d varbinds a day; a preset that polls has a cost",
				iv, c.PollsPerDay, c.VarbindsPerDay)
		}
	}
	// And two different cadences do not collapse onto the same answer.
	a, b := valid(), valid()
	a.IntervalSec, b.IntervalSec = 2400, 3600
	if Estimate(a).PollsPerDay == Estimate(b).PollsPerDay {
		t.Error("forty minutes and an hour report the same number of polls")
	}
}

// MaxTextLen is about layout, and layout is measured in characters. This
// application ships a zh locale: counting bytes refuses a title that fits.
func TestAuthorTextIsBoundedInCharactersNotBytes(t *testing.T) {
	p := valid()
	p.Widgets[0].Title = strings.Repeat("端", 44) // 44 runes, 132 bytes
	if errs := Validate(p); has(errs, "widgets[0].title") {
		t.Error("a 44-character title was refused for being too long")
	}
	p.Widgets[0].Title = strings.Repeat("端", MaxTextLen+1)
	if errs := Validate(p); !has(errs, "widgets[0].title") {
		t.Error("an over-long title was accepted because its runes were counted as bytes")
	}
}

// clip quotes the offending value back to the author. Slicing bytes splits a
// rune, and encoding/json rewrites the broken tail — so the message naming the
// value would misquote it.
func TestClipNeverProducesInvalidUtf8(t *testing.T) {
	for _, s := range []string{strings.Repeat("é", 80), strings.Repeat("端", 80), strings.Repeat("a", 80)} {
		if got := clip(s); !utf8.ValidString(got) {
			t.Errorf("clip produced invalid UTF-8 from %d runes", utf8.RuneCountInString(s))
		}
	}
}

// An OID is not just a shape: gosnmp marshals a sub-identifier as a uint32 and
// RFC 2578 caps the depth. Past either, it is not a large OID — it is not one.
func TestAnOidOutsideTheWireFormatIsRefused(t *testing.T) {
	bad := map[string]string{
		"an arc past uint32":   "1.3.6.1.4.1.4294967296",
		"a huge arc":           "1.3.6.1.99999999999999999999",
		"a non-root first arc": "3.6.1.2.1.1.3.0",
		"too deep":             "1." + strings.TrimSuffix(strings.Repeat("1.", maxOIDDepth), "."),
	}
	for name, oid := range bad {
		p := valid()
		p.Widgets[0].OIDs = []string{oid}
		if errs := Validate(p); !has(errs, "widgets[0].oids[0]") {
			t.Errorf("%s: %q was accepted", name, oid)
		}
	}
	// The top of the range is still an OID.
	p := valid()
	p.Widgets[0].OIDs = []string{"1.3.6.1.4.1.4294967295"}
	if errs := Validate(p); has(errs, "widgets[0].oids[0]") {
		t.Error("the largest legal sub-identifier was refused")
	}
}

// A version below one fell through both branches and was read as though it had
// been declared.
func TestANegativeFormatVersionIsRefused(t *testing.T) {
	p := valid()
	p.FormatVersion = -1
	if errs := Validate(p); !has(errs, "formatVersion") {
		t.Error("a negative format version was accepted")
	}
}

// The label KEY is author-supplied text too: it is interpolated into
// Error.Field and rendered as the name of a state.
func TestALabelKeyIsCheckedAsWellAsItsValue(t *testing.T) {
	for _, k := range []string{"up", "1\n2", "", strings.Repeat("9", 40)} {
		p := valid()
		p.Widgets[2] = Widget{Kind: KindStatus, Title: "Link", OIDs: []string{"1.3.6.1.2.1.2.2.1.8.1"},
			Labels: map[string]string{k: "up"}}
		if errs := Validate(p); !has(errs, "widgets[2].labels") {
			t.Errorf("%q was accepted as a label key: %s", k, fieldsOf(errs))
		}
	}
	p := valid()
	p.Widgets[2].Labels = map[string]string{"-1": "down", "2": "up"}
	if errs := Validate(p); has(errs, "widgets[2].labels") {
		t.Errorf("a negative state number was refused: %s", fieldsOf(errs))
	}
}

// The port panel: one widget, one cell per interface, one label map.
//
// It replaces the table kind, which the GET-only poll path cannot feed. What
// makes it work is that the instances are named explicitly — which is also what
// keeps the cost knowable before the preset runs.
func TestAPortPanelIsOneWidget(t *testing.T) {
	oids := make([]string, 0, 48)
	for i := 1; i <= 48; i++ {
		oids = append(oids, "1.3.6.1.2.1.2.2.1.8."+strconv.Itoa(i))
	}
	p := valid()
	p.Widgets = []Widget{{Kind: KindGrid, Title: "Ports", OIDs: oids,
		Labels: map[string]string{"1": "up", "2": "down"}}}
	if errs := Validate(p); len(errs) != 0 {
		t.Fatalf("a 48-port grid was refused: %s", fieldsOf(errs))
	}
	if c := Estimate(p); c.OIDs != 48 || c.Widgets != 1 {
		t.Errorf("cost = %d OIDs across %d widgets, want 48 across 1", c.OIDs, c.Widgets)
	}
	// And a table kind no longer exists: it would need a walk.
	if _, known := widgetDoc("table"); known {
		t.Error("the table kind is still in the vocabulary; the poll path is GET-only")
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

// A description is prose, not a label, and one bound cannot serve both.
//
// MaxTextLen's reasoning is about a title in a panel. Applied to a description
// it refused every example preset this application ships — which is the
// clearest evidence a bound is doing two jobs.
func TestADescriptionIsBoundedAsProseNotAsALabel(t *testing.T) {
	p := valid()
	p.Description = strings.Repeat("x", MaxTextLen+50)
	if errs := Validate(p); has(errs, "description") {
		t.Errorf("a %d-character description was refused: %s", len(p.Description), fieldsOf(errs))
	}

	p.Description = strings.Repeat("x", MaxDescriptionLen+1)
	if errs := Validate(p); !has(errs, "description") {
		t.Error("a description past its own bound was accepted")
	}

	// The label bound is unchanged: a name is still a label.
	p2 := valid()
	p2.Name = strings.Repeat("x", MaxTextLen+1)
	if errs := Validate(p2); !has(errs, "name") {
		t.Error("the label bound was widened along with the prose one")
	}

	// And the half that is about what TRAVELS applies to both.
	p3 := valid()
	p3.Description = "a paragraph\nwith a newline"
	if errs := Validate(p3); !has(errs, "description") {
		t.Error("a control character in a description was accepted")
	}
}
