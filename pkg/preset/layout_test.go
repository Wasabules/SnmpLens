package preset

import (
	"strings"
	"testing"
)

func placed(x, y, w, h int) *Layout { return &Layout{X: x, Y: y, W: w, H: h} }

// A preset with a layout is one widget per cell of a twelve-column grid, and
// the ordinary case has to be accepted without ceremony.
func TestAValidLayoutIsAccepted(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Ports", IntervalSec: 60,
		Widgets: []Widget{
			{Kind: "grid", Title: "Ports", OIDs: []string{"1.3.6.1.2.1.2.2.1.8.1"},
				Layout: placed(0, 0, 12, 2)},
			{Kind: "rate", Title: "In", OIDs: []string{"1.3.6.1.2.1.2.2.1.10.1"},
				Layout: placed(0, 2, 6, 0)},
			{Kind: "rate", Title: "Out", OIDs: []string{"1.3.6.1.2.1.2.2.1.16.1"},
				Layout: placed(6, 2, 6, 0)},
		},
	}
	if errs := Validate(p); len(errs) > 0 {
		t.Fatalf("a valid layout was refused: %v", errs)
	}
	if !HasLayout(p) {
		t.Error("HasLayout says this preset places nothing")
	}
	// An omitted height is one row. Making every author write h: 1 buys
	// nothing, and reading 0 as "no height" would collapse the widget.
	if got := p.Widgets[1].Layout.Height(); got != 1 {
		t.Errorf("an omitted height is %d rows, want 1", got)
	}
	if got := p.Widgets[0].Layout.Height(); got != 2 {
		t.Errorf("a declared height of 2 came back as %d", got)
	}
}

// A preset that places nothing is not a preset with a broken layout. It keeps
// the reflowing list of cards it has always had, and nothing about the grid
// applies to it.
func TestAPresetWithoutALayoutIsUnaffected(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Plain", IntervalSec: 60,
		Widgets: []Widget{
			{Kind: "value", Title: "Uptime", OIDs: []string{"1.3.6.1.2.1.1.3.0"}},
			{Kind: "value", Title: "Interfaces", OIDs: []string{"1.3.6.1.2.1.2.1.0"}},
		},
	}
	if errs := Validate(p); len(errs) > 0 {
		t.Fatalf("a preset that places nothing was refused: %v", errs)
	}
	if HasLayout(p) {
		t.Error("HasLayout says this preset places something")
	}
}

// "x is 10 and w is 6" is two legal numbers and one widget hanging off the
// right-hand side. The browser answers that by growing a thirteenth column and
// wrapping, so the whole row below moves and nothing says why.
func TestAWidgetCannotRunPastTheGrid(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Over", IntervalSec: 60,
		Widgets: []Widget{{Kind: "value", Title: "T", OIDs: []string{"1.3.6.1.2.1.1.3.0"},
			Layout: placed(10, 0, 6, 1)}},
	}
	errs := Validate(p)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly one", errs)
	}
	if errs[0].Field != "widgets[0].layout" || !strings.HasSuffix(errs[0].Message, "layoutOverflow") {
		t.Errorf("wrong error: %+v", errs[0])
	}
	// The message carries the numbers, because "invalid layout" leaves the
	// author to work out which of the four it means.
	if errs[0].Args["x"] != "10" || errs[0].Args["w"] != "6" || errs[0].Args["columns"] != "12" {
		t.Errorf("args = %v", errs[0].Args)
	}
}

// The ranges, one at a time. Each is a number that produces a dashboard nobody
// can read rather than an error anywhere.
func TestTheLayoutBoundsAreEnforced(t *testing.T) {
	cases := []struct {
		why   string
		l     *Layout
		field string
	}{
		{"a negative column", &Layout{X: -1, W: 4}, "widgets[0].layout.x"},
		{"past the last column", &Layout{X: 12, W: 1}, "widgets[0].layout.x"},
		{"no width at all", &Layout{X: 0, W: 0}, "widgets[0].layout.w"},
		{"wider than the grid", &Layout{X: 0, W: 13}, "widgets[0].layout.w"},
		{"a negative row", &Layout{X: 0, Y: -1, W: 4}, "widgets[0].layout.y"},
		{"a row past the bound", &Layout{X: 0, Y: MaxLayoutRow + 1, W: 4}, "widgets[0].layout.y"},
		{"taller than the bound", &Layout{X: 0, W: 4, H: MaxLayoutHeight + 1}, "widgets[0].layout.h"},
		{"a negative height", &Layout{X: 0, W: 4, H: -1}, "widgets[0].layout.h"},
	}
	for _, c := range cases {
		p := Preset{
			FormatVersion: 1, Name: "Bad", IntervalSec: 60,
			Widgets: []Widget{{Kind: "value", Title: "T",
				OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Layout: c.l}},
		}
		found := false
		for _, e := range Validate(p) {
			if e.Field == c.field {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was accepted (%+v): %v", c.why, *c.l, Validate(p))
		}
	}
}

// Two cards drawn on top of each other is not a taste question: CSS grid stacks
// them, so the widget written second covers the one written first and the
// dashboard is missing something that is right there in the file.
func TestTwoWidgetsCannotSitOnTheSameCells(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Clash", IntervalSec: 60,
		Widgets: []Widget{
			{Kind: "value", Title: "A", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Layout: placed(0, 0, 6, 2)},
			{Kind: "value", Title: "B", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Layout: placed(3, 1, 6, 2)},
		},
	}
	errs := Validate(p)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly one", errs)
	}
	// Against the LATER widget: the earlier one is where the author started,
	// and blaming both says nothing about which to move.
	if errs[0].Field != "widgets[1].layout" || !strings.HasSuffix(errs[0].Message, "layoutOverlap") {
		t.Errorf("wrong error: %+v", errs[0])
	}
	if errs[0].Args["other"] != "1" {
		t.Errorf("the message does not name the widget it collides with: %v", errs[0].Args)
	}
}

// Adjacency is not overlap, and a dashboard is mostly adjacency: two counters
// side by side, a row of tiles under a chart. An off-by-one here would refuse
// every layout anybody writes.
func TestWidgetsThatMerelyTouchAreFine(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Tight", IntervalSec: 60,
		Widgets: []Widget{
			// Side by side: 0..6 and 6..12.
			{Kind: "value", Title: "A", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Layout: placed(0, 0, 6, 1)},
			{Kind: "value", Title: "B", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Layout: placed(6, 0, 6, 1)},
			// Directly underneath: rows 0..1 and 1..3.
			{Kind: "chart", Title: "C", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Layout: placed(0, 1, 12, 2)},
			// Same columns as C, starting where it ends.
			{Kind: "chart", Title: "D", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Layout: placed(0, 3, 12, 1)},
		},
	}
	if errs := Validate(p); len(errs) > 0 {
		t.Fatalf("adjacent widgets were called an overlap: %v", errs)
	}
}

// A widget with no layout is placed by the browser, which by definition uses
// cells nothing else has claimed. Reporting it as an overlap would make mixing
// the two impossible, which is the case an author hits the moment they add a
// widget to a laid-out preset.
func TestAnUnplacedWidgetNeverOverlaps(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Mixed", IntervalSec: 60,
		Widgets: []Widget{
			{Kind: "value", Title: "A", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Layout: placed(0, 0, 12, 1)},
			{Kind: "value", Title: "B", OIDs: []string{"1.3.6.1.2.1.1.3.0"}},
			{Kind: "value", Title: "C", OIDs: []string{"1.3.6.1.2.1.1.3.0"}},
		},
	}
	if errs := Validate(p); len(errs) > 0 {
		t.Fatalf("a preset mixing placed and unplaced widgets was refused: %v", errs)
	}
	if !HasLayout(p) {
		t.Error("one placed widget is enough for HasLayout")
	}
}

// A widget whose own layout is nonsense is reported once, for that. Feeding a
// zero width into the overlap comparison would add a second error about a
// collision with a widget that has no size.
func TestABrokenLayoutIsNotAlsoAnOverlap(t *testing.T) {
	p := Preset{
		FormatVersion: 1, Name: "Broken", IntervalSec: 60,
		Widgets: []Widget{
			{Kind: "value", Title: "A", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Layout: placed(0, 0, 12, 1)},
			{Kind: "value", Title: "B", OIDs: []string{"1.3.6.1.2.1.1.3.0"}, Layout: placed(0, 0, 0, 1)},
		},
	}
	errs := Validate(p)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly one (the width)", errs)
	}
	if errs[0].Field != "widgets[1].layout.w" {
		t.Errorf("wrong error: %+v", errs[0])
	}
}

// Discovery rewrites a widget's OIDs and must not move it. The layout is the
// author's arrangement of the dashboard; how many ports the equipment turned
// out to have does not change where the port wall goes.
func TestExpansionKeepsTheLayout(t *testing.T) {
	w := Widget{
		Kind: "grid", Title: "Ports",
		OIDs:     []string{"1.3.6.1.2.1.2.2.1.8.{#}"},
		Discover: &Discover{Walk: "1.3.6.1.2.1.2.2.1.2"},
		Layout:   placed(0, 0, 12, 2),
	}
	out := ExpandWidget(w, []Instance{{Suffix: "1", Label: "eth0"}, {Suffix: "2", Label: "eth1"}})
	if out.Layout == nil {
		t.Fatal("expansion dropped the layout")
	}
	if *out.Layout != *w.Layout {
		t.Errorf("layout = %+v, want %+v", *out.Layout, *w.Layout)
	}
}
