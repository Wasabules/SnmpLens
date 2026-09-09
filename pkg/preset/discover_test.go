package preset

import (
	"strconv"
	"strings"
	"testing"
)

func discovering() Preset {
	p := valid()
	p.Widgets = []Widget{
		{Kind: KindValue, Title: "Uptime", OIDs: []string{"1.3.6.1.2.1.1.3.0"}},
		{
			Kind: KindGrid, Title: "Ports",
			OIDs:     []string{"1.3.6.1.2.1.2.2.1.8.{#}"},
			Labels:   map[string]string{"1": "up", "2": "down"},
			Discover: &Discover{Walk: "1.3.6.1.2.1.2.2.1.2"},
		},
	}
	return p
}

func ports(n int) []Instance {
	out := make([]Instance, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, Instance{Suffix: strconv.Itoa(i), Label: "Gi0/" + strconv.Itoa(i)})
	}
	return out
}

// The whole point: one file, any equipment.
func TestExpansionTurnsATemplateIntoTheInstancesFound(t *testing.T) {
	w := discovering().Widgets[1]
	got := ExpandWidget(w, ports(3))

	if got.Discover != nil {
		t.Error("the expanded widget still asks to be discovered")
	}
	want := []string{"1.3.6.1.2.1.2.2.1.8.1", "1.3.6.1.2.1.2.2.1.8.2", "1.3.6.1.2.1.2.2.1.8.3"}
	if strings.Join(got.OIDs, ",") != strings.Join(want, ",") {
		t.Errorf("OIDs = %v", got.OIDs)
	}

	// The walked column is the LABEL source as well as the instance source,
	// which is why a discovered grid reads "Gi0/1" rather than "8".
	if got.OIDLabels["1.3.6.1.2.1.2.2.1.8.2"] != "Gi0/2" {
		t.Errorf("labels = %v", got.OIDLabels)
	}
	// And the author's own state map is untouched by it.
	if got.Labels["1"] != "up" {
		t.Errorf("the state labels were lost: %v", got.Labels)
	}
}

// An index is not always a number: a table keyed by an address has several
// sub-identifiers, and the suffix is all of them.
func TestAMultiPartInstanceSurvives(t *testing.T) {
	w := Widget{
		Kind: KindStatus, Title: "Neighbours", OIDs: []string{"1.3.6.1.2.1.4.22.1.4.{#}"},
		Discover: &Discover{Walk: "1.3.6.1.2.1.4.22.1.2"},
	}
	got := ExpandWidget(w, []Instance{{Suffix: "3.10.0.0.5", Label: "aa:bb"}})
	if len(got.OIDs) != 1 || got.OIDs[0] != "1.3.6.1.2.1.4.22.1.4.3.10.0.0.5" {
		t.Errorf("OIDs = %v", got.OIDs)
	}
}

// Instance-major: a port's in and out counters stay next to each other rather
// than twenty-four ins arriving before the first out.
func TestTemplatesExpandInstanceMajor(t *testing.T) {
	w := Widget{
		Kind: KindChart, Title: "Traffic",
		OIDs:     []string{"1.3.6.1.2.1.2.2.1.10.{#}", "1.3.6.1.2.1.2.2.1.16.{#}"},
		Discover: &Discover{Walk: "1.3.6.1.2.1.2.2.1.2"},
	}
	got := ExpandWidget(w, ports(2))
	want := "1.3.6.1.2.1.2.2.1.10.1,1.3.6.1.2.1.2.2.1.16.1,1.3.6.1.2.1.2.2.1.10.2,1.3.6.1.2.1.2.2.1.16.2"
	if strings.Join(got.OIDs, ",") != want {
		t.Errorf("OIDs = %v", got.OIDs)
	}
}

// A widget may mix a fixed reading with discovered ones.
func TestAnOidWithNoPlaceholderIsKept(t *testing.T) {
	w := Widget{
		Kind: KindChart, Title: "Traffic",
		OIDs:     []string{"1.3.6.1.2.1.2.2.1.10.{#}", "1.3.6.1.2.1.1.3.0"},
		Discover: &Discover{Walk: "1.3.6.1.2.1.2.2.1.2"},
	}
	got := ExpandWidget(w, ports(2))
	if len(got.OIDs) != 3 || got.OIDs[2] != "1.3.6.1.2.1.1.3.0" {
		t.Errorf("OIDs = %v", got.OIDs)
	}
}

// The bound applies whether or not the author thought about it — a walk of a
// forty-thousand-row table must not become a dashboard.
func TestTheWalkIsBounded(t *testing.T) {
	// A grid's own MaxOIDs is below the default walk bound, so it is what
	// applies here; TestTheDisplayBoundCapsTheWalkBound is about that rule.
	grid, _ := widgetDoc(KindGrid)
	w := discovering().Widgets[1]
	if got := ExpandWidget(w, ports(500)); len(got.OIDs) != grid.MaxOIDs {
		t.Errorf("%d OIDs from 500 instances, want %d", len(got.OIDs), grid.MaxOIDs)
	}

	// A declared bound below both is the one that applies.
	w.Discover.Max = 8
	if got := ExpandWidget(w, ports(500)); len(got.OIDs) != 8 {
		t.Errorf("%d OIDs with an explicit bound of 8", len(got.OIDs))
	}

	// And the default is what catches a kind with no ceiling of its own.
	if MaxDiscoveredPerWidget <= grid.MaxOIDs {
		t.Errorf("the default walk bound %d is below every kind's, so it can never apply",
			MaxDiscoveredPerWidget)
	}
}

// One walk per COLUMN, not per widget: a grid of states and a chart of counters
// discover from the same ifDescr.
func TestTwoWidgetsOnOneColumnAreOneWalk(t *testing.T) {
	p := discovering()
	p.Widgets = append(p.Widgets, Widget{
		Kind: KindChart, Title: "Traffic", OIDs: []string{"1.3.6.1.2.1.2.2.1.10.{#}"},
		Discover: &Discover{Walk: ".1.3.6.1.2.1.2.2.1.2"},
	})

	walks := DiscoveryWalks(p)
	if len(walks) != 1 || walks[0] != "1.3.6.1.2.1.2.2.1.2" {
		t.Errorf("walks = %v; the leading dot is the same column", walks)
	}
	if !HasDiscovery(p) || HasDiscovery(valid()) {
		t.Error("HasDiscovery does not tell the two apart")
	}
}

// A row outside the walked column must not become an instance addressed at
// random.
func TestInstancesComeOnlyFromTheWalkedColumn(t *testing.T) {
	const column = "1.3.6.1.2.1.2.2.1.2"
	oids := []string{
		".1.3.6.1.2.1.2.2.1.2.1",
		"1.3.6.1.2.1.2.2.1.2.2",
		"1.3.6.1.2.1.2.2.1.3.1", // the next column: not ours
		"1.3.6.1.2.1.2.2.1.2",   // the column itself, no instance
		"1.3.6.1.4.1.9.1.1",     // somewhere else entirely
	}
	values := map[string]string{".1.3.6.1.2.1.2.2.1.2.1": "Gi0/1", "1.3.6.1.2.1.2.2.1.2.2": "Gi0/2"}

	got := InstancesFrom(column, oids, func(o string) string { return values[o] })
	if len(got) != 2 {
		t.Fatalf("%d instances: %+v", len(got), got)
	}
	if got[0].Suffix != "1" || got[0].Label != "Gi0/1" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Suffix != "2" {
		t.Errorf("second = %+v", got[1])
	}
}

/* --- what the format refuses ---------------------------------------------- */

func TestAPlaceholderNeedsADiscoverAndViceVersa(t *testing.T) {
	// A template with nothing to fill it would reach the wire as
	// "…8.{#}", which fails somewhere far from the file that caused it.
	p := valid()
	p.Widgets[0].OIDs = []string{"1.3.6.1.2.1.2.2.1.8.{#}"}
	if errs := Validate(p); !has(errs, "widgets[0].oids") {
		t.Error("a placeholder without a discover was accepted")
	}

	// And a walk nothing uses is a walk paid for and thrown away.
	p2 := valid()
	p2.Widgets[0].Discover = &Discover{Walk: "1.3.6.1.2.1.2.2.1.2"}
	if errs := Validate(p2); !has(errs, "widgets[0].oids") {
		t.Error("a discover with no placeholder was accepted")
	}

	// The walk is an OID like any other.
	p3 := discovering()
	p3.Widgets[1].Discover.Walk = "ifDescr"
	if errs := Validate(p3); !has(errs, "widgets[1].discover.walk") {
		t.Error("a walk that is not a numeric OID was accepted")
	}

	// A template is still checked as the OID it becomes.
	p4 := discovering()
	p4.Widgets[1].OIDs = []string{"9.3.6.1.2.1.2.2.1.8.{#}"}
	if errs := Validate(p4); !has(errs, "widgets[1].oids[0]") {
		t.Error("a template with a non-root first arc was accepted")
	}

	// And a well-formed discovering preset passes.
	if errs := Validate(discovering()); len(errs) != 0 {
		t.Errorf("a valid discovering preset was refused: %s", fieldsOf(errs))
	}
}

// The cost of a discovering preset is an UPPER BOUND, and it says so.
//
// "at most 3 072 values a day" and "3 072 values a day" are different
// statements, and only one of them is true before the walk.
func TestTheCostOfADiscoveringPresetIsACeiling(t *testing.T) {
	p := discovering()
	p.Widgets[1].Discover.Max = 48

	c := Estimate(p)
	if c.Discovered != 48 {
		t.Errorf("Discovered = %d, want 48", c.Discovered)
	}
	if c.OIDs != 49 { // the uptime, plus the ceiling
		t.Errorf("OIDs = %d, want 49", c.OIDs)
	}

	// After the walk it is a plain preset, and the count is exact.
	expanded := Expand(p, map[int][]Instance{1: ports(3)})
	after := Estimate(expanded)
	if after.Discovered != 0 {
		t.Errorf("an expanded preset still reports %d discovered", after.Discovered)
	}
	if after.OIDs != 4 {
		t.Errorf("OIDs after the walk = %d, want 4", after.OIDs)
	}
	if HasDiscovery(expanded) {
		t.Error("the expanded preset still asks for a walk")
	}
}

// A template is not an OID, and polling one would fail far from its cause.
func TestPollOidsNeverReturnsATemplate(t *testing.T) {
	for _, oid := range PollOIDs(discovering()) {
		if strings.Contains(oid, InstancePlaceholder) {
			t.Errorf("%q would go on the wire", oid)
		}
	}
	// Expanded, they are all there.
	got := PollOIDs(Expand(discovering(), map[int][]Instance{1: ports(2)}))
	if len(got) != 3 {
		t.Errorf("%d OIDs after expansion: %v", len(got), got)
	}
}

// Two bounds meet, and the SMALLER wins.
//
// The walk bound stops a forty-thousand-row table becoming a dashboard; the
// kind's own MaxOIDs is what the widget can actually draw. Discovering more
// than the second is polling readings nobody sees.
func TestTheDisplayBoundCapsTheWalkBound(t *testing.T) {
	grid, _ := widgetDoc(KindGrid)

	// A grid asking for more than it can draw gets what it can draw.
	w := discovering().Widgets[1]
	w.Discover.Max = MaxOIDsPerPreset
	if got := ExpandWidget(w, ports(400)); len(got.OIDs) != grid.MaxOIDs {
		t.Errorf("%d OIDs, want the kind's own bound %d", len(got.OIDs), grid.MaxOIDs)
	}

	// A chart is capped at eight series by the palette, so a chart discovering
	// an in and an out counter shows FOUR ports. A real limitation, stated.
	chart := Widget{
		Kind: KindChart, Title: "Traffic",
		OIDs:     []string{"1.3.6.1.2.1.2.2.1.10.{#}", "1.3.6.1.2.1.2.2.1.16.{#}"},
		Discover: &Discover{Walk: "1.3.6.1.2.1.2.2.1.2"},
	}
	if got := ExpandWidget(chart, ports(50)); len(got.OIDs) != 8 {
		t.Errorf("%d OIDs on a chart, want 8", len(got.OIDs))
	}

	// A fixed OID takes its share of the room before the discovered ones.
	mixed := chart
	mixed.OIDs = []string{"1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.2.2.1.10.{#}"}
	if got := ExpandWidget(mixed, ports(50)); len(got.OIDs) != 8 {
		t.Errorf("%d OIDs on a mixed chart, want 8 (7 discovered + 1 fixed)", len(got.OIDs))
	}
}

// Which means the preset bound is out of reach for ONE widget, and reachable
// across several — and only after the walk, which is why binding validates
// again.
func TestThePresetBoundIsReachedAcrossWidgets(t *testing.T) {
	one := Expand(discovering(), map[int][]Instance{1: ports(MaxOIDsPerPreset + 10)})
	if errs := Validate(one); len(errs) != 0 {
		t.Errorf("a single bounded widget was refused: %s", fieldsOf(errs))
	}

	grid, _ := widgetDoc(KindGrid)
	need := MaxOIDsPerPreset/grid.MaxOIDs + 1 // enough grids to overshoot

	// A DIFFERENT column per widget. Six grids on the same column poll the same
	// ninety-six OIDs, and the cost deduplicates — so a degenerate fixture would
	// prove nothing about the bound.
	columns := []string{"8", "10", "14", "16", "19", "20", "13", "21"}
	many := valid()
	many.Widgets = nil
	found := map[int][]Instance{}
	for i := 0; i < need; i++ {
		many.Widgets = append(many.Widgets, Widget{
			Kind: KindGrid, Title: "Ports",
			OIDs:     []string{"1.3.6.1.2.1.2.2.1." + columns[i%len(columns)] + ".{#}"},
			Discover: &Discover{Walk: "1.3.6.1.2.1.2.2.1.2"},
		})
		found[i] = ports(grid.MaxOIDs)
	}

	expanded := Expand(many, found)
	if got := Estimate(expanded).OIDs; got <= MaxOIDsPerPreset {
		t.Fatalf("setup: %d OIDs does not overshoot %d", got, MaxOIDsPerPreset)
	}
	if errs := Validate(expanded); !has(errs, "widgets") {
		t.Errorf("%d OIDs across %d widgets was accepted: %s",
			Estimate(expanded).OIDs, need, fieldsOf(errs))
	}
}
