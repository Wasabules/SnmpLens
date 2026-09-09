package preset

import (
	"fmt"
	"strings"
)

// A map: a drawing whose parts are readings.
//
// The want is Zabbix's: a picture of the rack, or of the site, with the ports
// and the links on it coloured by what they are actually doing. Two ways to
// give somebody that, and only one of them can be a file from a stranger.
//
// NOT SVG. It is the obvious answer and it is a script-execution vector: an
// <svg> carries <script>, event-handler attributes, <foreignObject> and
// external references, and sanitising it means shipping a sanitiser that has to
// be right forever against a format designed to be extensible. A preset is a
// file somebody else wrote and this application renders it in a WebView with a
// bridge to the operating system on the other side of it. There is no version
// of "mostly safe SVG" worth that.
//
// So the same answer pkg/notify/template.go gave for message templates and this
// package already gave for widgets: a FROZEN VOCABULARY. Four shapes — a
// rectangle, a line, a label and a dot — placed in PERCENTAGES of the widget's
// own box. A preset picks from that list; it cannot describe a shape, and there
// is no path by which what it writes becomes markup.
//
// Percentages rather than pixels for the reason the layout is a grid: the map
// has to survive the window being resized and the widget being given six
// columns instead of twelve, and a drawing pinned to pixels is a drawing that
// is right at one size.
//
// The BACKGROUND is the operator's, never the preset's. A preset names a file;
// the file is resolved inside the operator's own assets directory and nowhere
// else. A preset that carried image bytes would be an arbitrary blob this
// application decodes, and a preset that carried a PATH would be an
// arbitrary-file-read primitive over the bridge — the same pair app_preset.go's
// import gate is written about.

// Shape types. This IS the vocabulary.
const (
	// ShapeRect is a box: a port, a device, a room.
	ShapeRect = "rect"
	// ShapeLine is a link between two points.
	ShapeLine = "line"
	// ShapeLabel is text with no body of its own.
	ShapeLabel = "label"
	// ShapeDot is a small marker, for something too small to draw.
	ShapeDot = "dot"
)

var shapeTypes = []string{ShapeRect, ShapeLine, ShapeLabel, ShapeDot}

// ShapeTypes serves the vocabulary to the UI, as i18n key suffixes for the
// same reason WidgetKinds does.
func ShapeTypes() []string {
	out := make([]string, len(shapeTypes))
	copy(out, shapeTypes)
	return out
}

func knownShape(t string) bool {
	for _, s := range shapeTypes {
		if s == t {
			return true
		}
	}
	return false
}

const (
	// MaxShapesPerMap bounds one drawing. Two hundred is a rack elevation with
	// every port on it and then some; past that it is not a map, and every one
	// of them is a node this application renders and a reading it looks up.
	MaxShapesPerMap = 200
	// MaxAssetNameLen bounds the background's file name.
	MaxAssetNameLen = 120
)

// The shape of the drawing's own box.
const (
	// DefaultMapAspect is sixteen by nine: the shape of a photograph, which is
	// what an operator with no opinion has.
	DefaultMapAspect = 16.0 / 9.0
	// MinMapAspect and MaxMapAspect bound it. Twelve to one is a 48-port front
	// panel; a fifth is a tall rack elevation. Past either the widget is a
	// sliver, which is a drawing nobody can read and a row nothing else fits
	// beside.
	MinMapAspect = 0.2
	MaxMapAspect = 12.0
)

// Map is what a map widget draws.
type Map struct {
	// Background is a FILE NAME in the operator's assets directory — never a
	// path, never a URL, never bytes. Optional: the shapes alone are a drawing.
	Background string `json:"background,omitempty"`
	// Aspect is the drawing's width divided by its height, and it is the
	// difference between a picture of a switch and a poster of one.
	//
	// The shapes are percentages of the BOX, so the box's shape decides what
	// the drawing looks like: a rack front panel is about eight to one and a
	// site plan is about four to three, and neither survives being given the
	// other's frame. Declared once by whoever drew it; zero means the default.
	Aspect float64 `json:"aspect,omitempty"`
	// Shapes are drawn in order, so a preset can put a label over a box by
	// writing it after.
	Shapes []Shape `json:"shapes"`
}

// AspectOr returns the declared aspect, or the default.
func (m Map) AspectOr() float64 {
	if m.Aspect < MinMapAspect || m.Aspect > MaxMapAspect {
		return DefaultMapAspect
	}
	return m.Aspect
}

// Shape is one part of a drawing.
//
// One struct for all four types rather than four, because the alternative
// across the bridge is a tagged union that TypeScript and Go each model
// differently — and the fields that go unused for a given type cost a few bytes
// in a file that is already bounded.
type Shape struct {
	Type string `json:"type"`
	// X and Y are percentages of the widget's box, from its top left.
	X float64 `json:"x"`
	Y float64 `json:"y"`
	// W and H size a rect. Ignored by the other types.
	W float64 `json:"w,omitempty"`
	H float64 `json:"h,omitempty"`
	// X2 and Y2 are a line's other end.
	X2 float64 `json:"x2,omitempty"`
	Y2 float64 `json:"y2,omitempty"`
	// OID binds this shape to a reading: its colour then comes from the value,
	// through the widget's own label map, exactly as a grid cell's does. A
	// shape without one is decoration — the outline of the rack, the name of a
	// room — and that is a thing a map needs.
	OID string `json:"oid,omitempty"`
	// Text is drawn on the shape. A label is text and nothing else.
	Text string `json:"text,omitempty"`
}

// checkMap validates a map widget's drawing.
func checkMap(at string, w Widget) []Error {
	if w.Kind != KindMap {
		if w.Map != nil {
			return []Error{errf(at+".map", "mapOnlyForMap", map[string]string{"kind": clip(w.Kind)})}
		}
		return nil
	}
	if w.Map == nil {
		return []Error{errf(at+".map", "missing", nil)}
	}

	var errs []Error
	errs = append(errs, checkAssetName(at+".map.background", w.Map.Background)...)

	// Zero is "no opinion" and is the ordinary case; a number outside the
	// bounds is an opinion this cannot honour, and silently substituting the
	// default would move every shape without saying so.
	if w.Map.Aspect != 0 && (w.Map.Aspect < MinMapAspect || w.Map.Aspect > MaxMapAspect) {
		errs = append(errs, errf(at+".map.aspect", "outOfRange", map[string]string{
			"min": trimFloat(MinMapAspect), "max": trimFloat(MaxMapAspect),
		}))
	}

	if len(w.Map.Shapes) == 0 {
		errs = append(errs, errf(at+".map.shapes", "empty", nil))
	}
	if len(w.Map.Shapes) > MaxShapesPerMap {
		errs = append(errs, errf(at+".map.shapes", "tooMany", map[string]string{
			"found": fmt.Sprint(len(w.Map.Shapes)), "max": fmt.Sprint(MaxShapesPerMap),
		}))
	}

	// The readings a shape may bind to are the widget's own, and nothing else.
	// Without this a map could name any OID it liked and this application would
	// look it up against a session that never polls it — a shape permanently
	// grey, with the preset looking correct.
	known := map[string]bool{}
	for _, oid := range w.OIDs {
		known[normaliseOID(oid)] = true
	}

	for i, s := range w.Map.Shapes {
		errs = append(errs, checkShape(fmt.Sprintf("%s.map.shapes[%d]", at, i), s, known)...)
	}
	return errs
}

func checkShape(at string, s Shape, known map[string]bool) []Error {
	var errs []Error
	if !knownShape(s.Type) {
		errs = append(errs, errf(at+".type", "unknownShape", map[string]string{"type": clip(s.Type)}))
	}

	pct := func(field string, v float64) {
		if v < 0 || v > 100 {
			errs = append(errs, errf(field, "outOfRange", map[string]string{"min": "0", "max": "100"}))
		}
	}
	pct(at+".x", s.X)
	pct(at+".y", s.Y)

	switch s.Type {
	case ShapeRect:
		if s.W <= 0 || s.H <= 0 {
			errs = append(errs, errf(at, "shapeWithoutSize", nil))
		}
		pct(at+".w", s.W)
		pct(at+".h", s.H)
		// A box that runs off the drawing is two legal numbers and a shape
		// nobody can see the end of. The map is the widget's own box, so there
		// is nowhere for it to overflow INTO.
		if s.W > 0 && s.X+s.W > 100 {
			errs = append(errs, errf(at, "shapeOutsideMap", map[string]string{"axis": "x"}))
		}
		if s.H > 0 && s.Y+s.H > 100 {
			errs = append(errs, errf(at, "shapeOutsideMap", map[string]string{"axis": "y"}))
		}
	case ShapeLine:
		pct(at+".x2", s.X2)
		pct(at+".y2", s.Y2)
		if s.X == s.X2 && s.Y == s.Y2 {
			errs = append(errs, errf(at, "shapeWithoutSize", nil))
		}
	case ShapeLabel:
		if strings.TrimSpace(s.Text) == "" {
			// A label is text and nothing else, so one without any is a shape
			// that renders as a point nobody can see or select.
			errs = append(errs, errf(at+".text", "empty", nil))
		}
	}

	errs = append(errs, checkText(at+".text", s.Text)...)

	if s.OID != "" {
		// Checked as an OID first, so "eth0" is reported as not being one
		// rather than as not being in the widget.
		if oidErrs := checkOID(at+".oid", strings.ReplaceAll(s.OID, InstancePlaceholder, "1")); len(oidErrs) > 0 {
			errs = append(errs, oidErrs...)
		} else if !known[normaliseOID(s.OID)] {
			errs = append(errs, errf(at+".oid", "shapeOidNotInWidget", map[string]string{
				"oid": clip(s.OID),
			}))
		}
	}
	return errs
}

// checkAssetName validates a background's file name.
//
// A NAME, resolved inside the operator's assets directory: no separator, no
// parent, no drive letter, nothing that survives being joined to a directory as
// anything but a leaf. app_assets.go resolves it again the same way — this is
// the message, that is the enforcement, and neither trusts the other.
func checkAssetName(field, name string) []Error {
	if name == "" {
		return nil
	}
	if len(name) > MaxAssetNameLen {
		return []Error{errf(field, "tooLong", map[string]string{
			"found": fmt.Sprint(len(name)), "max": fmt.Sprint(MaxAssetNameLen),
		})}
	}
	if strings.ContainsAny(name, `/\:`) || strings.Contains(name, "..") ||
		strings.HasPrefix(name, ".") || strings.TrimSpace(name) != name {
		return []Error{errf(field, "assetName", map[string]string{"name": clip(name)})}
	}
	if errs := checkText(field, name); len(errs) > 0 {
		return errs
	}
	return nil
}

// MapBackgrounds lists the asset names a preset asks for, deduplicated.
//
// The UI uses it to say, before binding, which backgrounds this preset expects
// and which of them the operator actually has: a map whose background is
// missing draws its shapes on nothing, which is a legitimate thing to do and a
// confusing thing to discover afterwards.
func MapBackgrounds(p Preset) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, w := range p.Widgets {
		if w.Map == nil || w.Map.Background == "" || seen[w.Map.Background] {
			continue
		}
		seen[w.Map.Background] = true
		out = append(out, w.Map.Background)
	}
	return out
}
