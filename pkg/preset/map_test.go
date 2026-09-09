package preset

import (
	"strings"
	"testing"
)

const portOID = "1.3.6.1.2.1.2.2.1.8.1"

func mapPreset(m *Map, oids ...string) Preset {
	if len(oids) == 0 {
		oids = []string{portOID}
	}
	return Preset{
		FormatVersion: 1, Name: "Rack", IntervalSec: 60,
		Widgets: []Widget{{
			Kind: KindMap, Title: "Rack A", OIDs: oids,
			Labels: map[string]string{"1": "up", "2": "down"},
			Map:    m,
		}},
	}
}

// The ordinary case: a box bound to a port, a line, and a label that is
// decoration. All three have to be accepted without ceremony, or nobody draws
// anything.
func TestADrawingIsAccepted(t *testing.T) {
	p := mapPreset(&Map{
		Background: "rack-a.png",
		Shapes: []Shape{
			{Type: ShapeRect, X: 10, Y: 20, W: 8, H: 4, OID: portOID, Text: "Gi0/1"},
			{Type: ShapeLine, X: 18, Y: 22, X2: 60, Y2: 22},
			{Type: ShapeLabel, X: 60, Y: 5, Text: "Core"},
			{Type: ShapeDot, X: 90, Y: 90, OID: "." + portOID},
		},
	})
	if errs := Validate(p); len(errs) > 0 {
		t.Fatalf("a valid drawing was refused: %v", errs)
	}
	if got := MapBackgrounds(p); len(got) != 1 || got[0] != "rack-a.png" {
		t.Errorf("backgrounds = %v", got)
	}
}

// A map is a drawing and a drawing is what a map widget is. The two must go
// together in both directions, or a map widget renders as an empty box and a
// value tile carries a drawing nothing looks at.
func TestAMapAndAMapWidgetGoTogether(t *testing.T) {
	empty := mapPreset(nil)
	errs := Validate(empty)
	if len(errs) != 1 || errs[0].Field != "widgets[0].map" {
		t.Fatalf("a map widget with no drawing: %v", errs)
	}

	elsewhere := Preset{
		FormatVersion: 1, Name: "Odd", IntervalSec: 60,
		Widgets: []Widget{{
			Kind: KindValue, Title: "Uptime", OIDs: []string{"1.3.6.1.2.1.1.3.0"},
			Map: &Map{Shapes: []Shape{{Type: ShapeDot, X: 1, Y: 1}}},
		}},
	}
	errs = Validate(elsewhere)
	if len(errs) != 1 || !strings.HasSuffix(errs[0].Message, "mapOnlyForMap") {
		t.Fatalf("a drawing on a value tile: %v", errs)
	}
}

// The vocabulary is frozen. A shape this version does not know is refused here
// rather than skipped in the renderer, where it would be an invisible part of
// a drawing that looks complete.
func TestTheShapeVocabularyIsFrozen(t *testing.T) {
	for _, bad := range []string{"svg", "image", "path", "polygon", ""} {
		p := mapPreset(&Map{Shapes: []Shape{{Type: bad, X: 1, Y: 1, W: 2, H: 2}}})
		found := false
		for _, e := range Validate(p) {
			if strings.HasSuffix(e.Message, "unknownShape") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q was accepted as a shape: %v", bad, Validate(p))
		}
	}
}

// Percentages of the widget's own box, in both axes. A coordinate outside them
// is a shape nobody can see, which is indistinguishable from a shape that
// failed to draw.
func TestCoordinatesArePercentages(t *testing.T) {
	cases := []struct {
		why   string
		shape Shape
		field string
	}{
		{"a negative x", Shape{Type: ShapeDot, X: -1, Y: 0}, "widgets[0].map.shapes[0].x"},
		{"past the right edge", Shape{Type: ShapeDot, X: 101, Y: 0}, "widgets[0].map.shapes[0].x"},
		{"a negative y", Shape{Type: ShapeDot, X: 0, Y: -0.5}, "widgets[0].map.shapes[0].y"},
		{"a line ending outside", Shape{Type: ShapeLine, X: 0, Y: 0, X2: 140, Y2: 10}, "widgets[0].map.shapes[0].x2"},
	}
	for _, c := range cases {
		p := mapPreset(&Map{Shapes: []Shape{c.shape}})
		found := false
		for _, e := range Validate(p) {
			if e.Field == c.field {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was accepted: %v", c.why, Validate(p))
		}
	}
}

// A box needs a size, a line needs a length, and a label needs its text: each
// of the three renders as nothing at all otherwise, which is the failure a
// drawing cannot afford — every other shape still draws, so it reads as a
// finished map with one thing missing.
func TestAShapeWithNothingToDrawIsRefused(t *testing.T) {
	cases := []struct {
		why   string
		shape Shape
		says  string
	}{
		{"a box with no width", Shape{Type: ShapeRect, X: 1, Y: 1, H: 4}, "shapeWithoutSize"},
		{"a box with no height", Shape{Type: ShapeRect, X: 1, Y: 1, W: 4}, "shapeWithoutSize"},
		{"a line that goes nowhere", Shape{Type: ShapeLine, X: 5, Y: 5, X2: 5, Y2: 5}, "shapeWithoutSize"},
		{"a label with no words", Shape{Type: ShapeLabel, X: 5, Y: 5}, "empty"},
	}
	for _, c := range cases {
		p := mapPreset(&Map{Shapes: []Shape{c.shape}})
		found := false
		for _, e := range Validate(p) {
			if strings.HasSuffix(e.Message, c.says) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was accepted: %v", c.why, Validate(p))
		}
	}
	// A dot has no size to give, so it is the one shape that needs only a
	// position.
	if errs := Validate(mapPreset(&Map{Shapes: []Shape{{Type: ShapeDot, X: 5, Y: 5}}})); len(errs) > 0 {
		t.Errorf("a dot was refused: %v", errs)
	}
}

// A box that runs off the edge is two legal numbers and a shape nobody can see
// the end of. The map IS the widget's box, so there is nowhere to overflow into.
func TestABoxCannotRunOffTheDrawing(t *testing.T) {
	p := mapPreset(&Map{Shapes: []Shape{{Type: ShapeRect, X: 96, Y: 10, W: 8, H: 4}}})
	found := ""
	for _, e := range Validate(p) {
		if strings.HasSuffix(e.Message, "shapeOutsideMap") {
			found = e.Args["axis"]
		}
	}
	if found != "x" {
		t.Errorf("a box running off the right edge: %v", Validate(p))
	}

	p = mapPreset(&Map{Shapes: []Shape{{Type: ShapeRect, X: 10, Y: 97, W: 8, H: 4}}})
	found = ""
	for _, e := range Validate(p) {
		if strings.HasSuffix(e.Message, "shapeOutsideMap") {
			found = e.Args["axis"]
		}
	}
	if found != "y" {
		t.Errorf("a box running off the bottom: %v", Validate(p))
	}

	// Exactly to the edge is inside it.
	if errs := Validate(mapPreset(&Map{Shapes: []Shape{
		{Type: ShapeRect, X: 90, Y: 90, W: 10, H: 10},
	}})); len(errs) > 0 {
		t.Errorf("a box ending exactly at the edge was refused: %v", errs)
	}
}

// A shape binds to one of the WIDGET's readings and to nothing else.
//
// Without this a map could name any OID it liked, and this application would
// look it up against a session that never polls it: a shape permanently grey,
// with the preset looking entirely correct and the cost screen never mentioning
// the reading it appears to show.
func TestAShapeCanOnlyBindToAReadingTheWidgetHas(t *testing.T) {
	p := mapPreset(&Map{Shapes: []Shape{
		{Type: ShapeRect, X: 1, Y: 1, W: 4, H: 4, OID: "1.3.6.1.2.1.2.2.1.8.99"},
	}})
	errs := Validate(p)
	if len(errs) != 1 || !strings.HasSuffix(errs[0].Message, "shapeOidNotInWidget") {
		t.Fatalf("a shape bound to a reading nobody polls: %v", errs)
	}

	// And a leading dot is the same reading, not a different one.
	if errs := Validate(mapPreset(&Map{Shapes: []Shape{
		{Type: ShapeRect, X: 1, Y: 1, W: 4, H: 4, OID: "." + portOID},
	}})); len(errs) > 0 {
		t.Errorf("a leading dot made it a different reading: %v", errs)
	}

	// Something that is not an OID at all is reported as that, rather than as
	// not being in the widget — which would send the author to the wrong field.
	errs = Validate(mapPreset(&Map{Shapes: []Shape{
		{Type: ShapeRect, X: 1, Y: 1, W: 4, H: 4, OID: "ifOperStatus.1"},
	}}))
	if len(errs) != 1 || !strings.HasSuffix(errs[0].Message, "notAnOid") {
		t.Errorf("a name was reported as: %v", errs)
	}
}

// The background is a NAME, resolved inside the operator's own directory. A
// preset that could write a path would be an arbitrary-file-read primitive over
// the bridge — the pair app_preset.go's import gate is written about.
func TestABackgroundIsANameAndNotAPath(t *testing.T) {
	bad := []string{
		"../monitoring.db", "..\\service.json", "sub/rack.png", `sub\rack.png`,
		"C:rack.png", ".hidden.png", " rack.png", strings.Repeat("a", 200),
	}
	for _, name := range bad {
		p := mapPreset(&Map{
			Background: name,
			Shapes:     []Shape{{Type: ShapeDot, X: 1, Y: 1}},
		})
		if errs := Validate(p); len(errs) == 0 {
			t.Errorf("%q was accepted as a background", name)
		}
	}
	// No background at all is a legitimate drawing: the shapes are the picture.
	if errs := Validate(mapPreset(&Map{
		Shapes: []Shape{{Type: ShapeDot, X: 1, Y: 1}},
	})); len(errs) > 0 {
		t.Errorf("a drawing with no background was refused: %v", errs)
	}
}

// A drawing has to have something in it, and it is bounded like everything else
// a stranger's file can put in a list this application renders.
func TestTheDrawingIsBounded(t *testing.T) {
	if errs := Validate(mapPreset(&Map{Background: "a.png"})); len(errs) == 0 {
		t.Error("a map with no shapes was accepted")
	}
	many := make([]Shape, MaxShapesPerMap+1)
	for i := range many {
		many[i] = Shape{Type: ShapeDot, X: 1, Y: 1}
	}
	errs := Validate(mapPreset(&Map{Shapes: many}))
	found := false
	for _, e := range errs {
		if e.Field == "widgets[0].map.shapes" && strings.HasSuffix(e.Message, "tooMany") {
			found = true
		}
	}
	if !found {
		t.Errorf("%d shapes were accepted", len(many))
	}
}

// A map colours its shapes through the widget's label map, exactly as a grid
// colours its cells — so a map widget is one of the kinds that may carry one.
func TestAMapMayCarryLabels(t *testing.T) {
	if !kindTakesLabels(KindMap) {
		t.Error("a map cannot name its states")
	}
}
