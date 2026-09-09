// Package preset defines what a dashboard preset is, and refuses one that is
// not.
//
// A preset is a file somebody else wrote. It is bound to a target when the
// target is added, and it says three things: WHAT TO POLL, HOW OFTEN, and HOW
// TO SHOW IT. That first one is why this package exists at all — a preset makes
// this application emit SNMP traffic to the operator's own equipment, at OIDs
// the preset chose.
//
// The shape of the answer is not new here. pkg/notify/template.go faced the
// same question for message templates and answered it with a FROZEN VOCABULARY
// rather than a language: a fixed set of names that can be listed, validated at
// save, and cannot reach a field nobody chose to expose. This follows that
// precedent deliberately. A preset picks a widget KIND from a list this package
// owns; it cannot describe one.
//
// What this package does NOT do, on purpose:
//
//   - It does not cap the work a preset asks for as a policy. Sixty OIDs
//     against a chassis on the same switch cost less than five over a satellite
//     link, so the OID count does not predict the cost and a limit drawn on it
//     would refuse legitimate presets while admitting expensive ones. The
//     guardrail is the MEASURED cycle time, which lives with the scheduler.
//     The bound below is a sanity check against a malformed file, set where no
//     real preset will ever meet it.
//   - It does not resolve OID names. A preset carries numeric OIDs, because a
//     numeric OID can be checked for what it is without consulting the MIB
//     tree — which would mean taking pkg/mib's exclusive gosmi lock while
//     validating a file, and would make what a preset polls depend on which
//     MIBs happen to be loaded.
package preset

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// FormatVersion is the shape this package writes and understands.
//
// Carried in the file so a preset written today can be recognised — and
// refused with a sentence rather than a parse error — by a version that has
// moved on.
const FormatVersion = 1

// Bounds. These are sanity checks against a malformed or hostile file, not the
// cost guardrail: see the package comment.
const (
	// MaxOIDsPerPreset is far past any real dashboard. A chassis view of a
	// large switch is tens of OIDs, not hundreds.
	MaxOIDsPerPreset = 500
	// MaxWidgets bounds the panel, and the render loop that draws it.
	MaxWidgets = 40
	// MinIntervalSec floors the declared cadence, and it is a POLICY rather
	// than a restatement of something downstream. The scheduler's own floor is
	// minInterval = 250 ms (pkg/monitor/scheduler.go), which exists to stop a
	// corrupt row spinning a goroutine; this one is the only real cap on how
	// much traffic a file somebody else wrote can make this application emit.
	MinIntervalSec = 5
	// MaxIntervalSec is a day: past that, a dashboard is a report.
	MaxIntervalSec = 86400
	// MaxTextLen caps author-supplied LABELS: the preset's name, a widget
	// title, a unit, a state's word. Each of those lands in a panel, and one
	// the length of a novel is a layout attack whatever the intent.
	MaxTextLen = 120
	// MaxDescriptionLen caps the one field that is PROSE rather than a label.
	//
	// It exists because 120 was applied to it and should not have been: the
	// reasoning above is about a title in a panel, and a description is a
	// paragraph in the library's detail view, where explaining that the
	// storage indexes are per device takes more than a dozen words. Every
	// example preset that ships was refused by the label bound before this
	// existed, which is the clearest possible evidence that the bound was
	// being asked to do two jobs.
	//
	// The control-character rule is unchanged and applies to both: that half is
	// about what travels into a log line and a syslog collector, not layout.
	MaxDescriptionLen = 600
)

// Widget kinds. This IS the vocabulary: a preset chooses from here and cannot
// describe a widget of its own.
const (
	// KindValue shows the latest reading, formatted for its type.
	KindValue = "value"
	// KindRate shows a counter's per-second rate, corrected for wraps.
	KindRate = "rate"
	// KindChart plots readings over time.
	KindChart = "chart"
	// KindStatus shows one reading as a state, mapped through the widget's own
	// labels — ifOperStatus 1 as "up" rather than as 1.
	KindStatus = "status"
	// KindGrid shows one cell per OID, all sharing the widget's label map:
	// the port panel of a switch, where forty-eight readings are one thing to
	// look at rather than forty-eight things.
	KindGrid = "grid"
	// KindMap draws a picture whose parts are readings — a rack elevation, a
	// site plan — from a frozen shape vocabulary over an optional background
	// the OPERATOR supplies. See map.go, and in particular why it is not SVG.
	KindMap = "map"
)

// There is deliberately no "table" kind.
//
// A conceptual table is a WALK, and the poll path a preset feeds is GET-only:
// PollOIDs goes to snmp.GetMany, which sends what it is given as varbinds, and
// a table's OID GET'd answers noSuchObject. The pivot also needs the column
// definitions and the INDEX rules out of the MIB (pkg/mib/table.go), which a
// flat list of numeric OIDs cannot supply. KindGrid is what that want actually
// reduces to here: the instances named explicitly, which is also what makes
// the cost of the preset knowable before it runs.

// WidgetDoc describes one kind for the settings UI.
//
// Description and Unit are i18n KEY SUFFIXES, not prose — the UI looks up
// preset.widget.<name>. Keeping the English out of Go is what stops the
// vocabulary from being the one part of the application that never translates,
// which is the same reason notify.VariableDoc does it.
type WidgetDoc struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
	// MaxOIDs is how many readings this kind can show. A value tile shows one;
	// a chart shows several. Zero means no limit beyond the preset's own.
	MaxOIDs int `json:"maxOids"`
}

var widgetKinds = []WidgetDoc{
	{Kind: KindValue, MaxOIDs: 1},
	{Kind: KindRate, MaxOIDs: 1},
	{Kind: KindStatus, MaxOIDs: 1},
	{Kind: KindChart, MaxOIDs: 8},
	{Kind: KindGrid, MaxOIDs: 96},
	// The same bound as a grid: a map is a wall of readings that happens to be
	// arranged by hand rather than in rows.
	{Kind: KindMap, MaxOIDs: 96},
}

// kindTakesLabels: a label map turns a number into a state, so it belongs to
// the two kinds that show a state and nowhere else. On any other kind it is a
// sign the author expected something this vocabulary does not do, and silence
// would leave them wondering why nothing happened.
func kindTakesLabels(kind string) bool {
	return kind == KindStatus || kind == KindGrid || kind == KindMap
}

// WidgetKinds serves the vocabulary to the UI.
//
// Generated from Go rather than mirrored in JavaScript: a hand-copied list is a
// list that drifts, and the first symptom would be a kind the editor offers and
// the validator rejects.
func WidgetKinds() []WidgetDoc {
	out := make([]WidgetDoc, len(widgetKinds))
	copy(out, widgetKinds)
	for i := range out {
		out[i].Description = "preset.widget." + out[i].Kind
	}
	return out
}

func widgetDoc(kind string) (WidgetDoc, bool) {
	for _, w := range widgetKinds {
		if w.Kind == kind {
			return w, true
		}
	}
	return WidgetDoc{}, false
}

// Preset is one dashboard description.
type Preset struct {
	FormatVersion int    `json:"formatVersion"`
	Name          string `json:"name"`
	Author        string `json:"author,omitempty"`
	Description   string `json:"description,omitempty"`

	// Match says which devices this preset is FOR. It is advice to the person
	// binding it, never an automatic action: binding happens when a target is
	// added, by someone who chose this file.
	Match Match `json:"match,omitzero"`

	// IntervalSec is the cadence the preset asks for, and it is REQUIRED.
	//
	// Declared rather than inferred because the cost has to be arithmetic
	// before the first poll: the person binding this is told how many requests
	// an hour it will make, and that number cannot be computed from a file
	// that does not say how often.
	IntervalSec int `json:"intervalSec"`

	Widgets []Widget `json:"widgets"`
}

// Match is how a preset says what it is for.
type Match struct {
	// SysObjectIDPrefix matches the device's sysObjectID. A prefix, because a
	// vendor's enterprise arc identifies the family and the full value
	// identifies the model.
	SysObjectIDPrefix []string `json:"sysObjectIdPrefix,omitempty"`
	// Vendor is free text for the person reading the list.
	Vendor string `json:"vendor,omitempty"`
}

// Widget is one tile.
type Widget struct {
	Kind  string   `json:"kind"`
	Title string   `json:"title"`
	OIDs  []string `json:"oids"`
	// Unit is an i18n key suffix when it names one this application knows, and
	// is otherwise shown as written.
	Unit string `json:"unit,omitempty"`
	// Labels maps an integer reading to a name, for KindStatus and KindGrid.
	// Nothing else reads it.
	Labels map[string]string `json:"labels,omitempty"`
	// Discover says where this widget's instances come from, instead of
	// listing them. See discover.go: the walk happens ONCE, at bind time, and
	// what is stored afterwards is a plain widget.
	Discover *Discover `json:"discover,omitempty"`
	// OIDLabels names each discovered OID with what the walk found — the port's
	// own name rather than its index. Written by expansion, never by an author:
	// a hand-written preset titles its own widgets.
	OIDLabels map[string]string `json:"oidLabels,omitempty"`
	// Threshold is the band this widget's readings should stay inside. It is
	// materialised into the session's own threshold map at bind time and read
	// by the ordinary evaluator; see threshold.go for what it can and cannot
	// be put on.
	Threshold *Threshold `json:"threshold,omitempty"`
	// Map is the drawing a map widget shows. Only a map widget may carry one,
	// and every map widget must. See map.go.
	Map *Map `json:"map,omitempty"`
	// Layout is where this widget goes on the twelve-column grid. Optional and
	// per widget: a preset that places nothing keeps the reflowing list of
	// cards, and one that places some widgets lets the browser find room for
	// the rest. See layout.go.
	Layout *Layout `json:"layout,omitempty"`
}

// Error is one reason a preset was refused.
//
// Field is the path to what is wrong (`widgets[2].oids[0]`) so the UI can point
// at it, and Message is an i18n key suffix with its arguments, for the same
// reason WidgetDoc.Description is.
type Error struct {
	Field   string            `json:"field"`
	Message string            `json:"message"`
	Args    map[string]string `json:"args,omitempty"`
}

func (e Error) Error() string { return e.Field + ": " + e.Message }

func errf(field, msg string, args map[string]string) Error {
	return Error{Field: field, Message: "preset.err." + msg, Args: args}
}

// numericOID is a dotted decimal OID, optionally leading with a dot.
//
// Deliberately not a name: see the package comment. Also deliberately not
// permissive about what follows — an OID with a trailing dot or a doubled
// separator is a typo that would otherwise reach gosnmp and fail somewhere far
// from the file that caused it.
var numericOID = regexp.MustCompile(`^\.?\d+(\.\d+)+$`)

// maxOIDDepth and maxSubIdentifier are what an OID can BE on the wire.
//
// The regexp above says the shape; these say the range. gosnmp marshals a
// sub-identifier as a uint32 and an object identifier is at most 128 of them
// (RFC 2578 7.1.3), so a value past either is not a large OID — it is not an
// OID, and refusing it here is the difference between a message naming the
// file and a failure somewhere inside the encoder.
const (
	maxOIDDepth      = 128
	maxSubIdentifier = 4294967295
)

// checkOID validates one OID string completely.
func checkOID(field, oid string) []Error {
	if !numericOID.MatchString(oid) {
		return []Error{errf(field, "notAnOid", map[string]string{"value": clip(oid)})}
	}
	arcs := strings.Split(strings.TrimPrefix(oid, "."), ".")
	if len(arcs) > maxOIDDepth {
		return []Error{errf(field, "oidTooDeep", map[string]string{
			"found": fmt.Sprint(len(arcs)), "max": fmt.Sprint(maxOIDDepth),
		})}
	}
	for _, a := range arcs {
		// Parsed rather than compared as a string: "0000000000004" is four.
		n, err := strconv.ParseUint(a, 10, 64)
		if err != nil || n > maxSubIdentifier {
			return []Error{errf(field, "oidOutOfRange", map[string]string{"value": clip(oid)})}
		}
	}
	// The root has three children and always did: ccitt, iso, joint-iso-ccitt.
	if arcs[0] != "0" && arcs[0] != "1" && arcs[0] != "2" {
		return []Error{errf(field, "notAnOid", map[string]string{"value": clip(oid)})}
	}
	return nil
}

// Validate checks a preset completely and returns every problem, not the first.
//
// Everything, because a preset is edited in a text editor by someone who is not
// looking at this code: reporting one error per attempt turns a five-minute fix
// into five round trips.
func Validate(p Preset) []Error {
	var errs []Error

	if p.FormatVersion < 1 {
		// Below one, not equal to zero: a negative version fell through both
		// branches and was accepted as though it had been read.
		errs = append(errs, errf("formatVersion", "missing", nil))
	} else if p.FormatVersion > FormatVersion {
		errs = append(errs, errf("formatVersion", "tooNew", map[string]string{
			"found": fmt.Sprint(p.FormatVersion), "supported": fmt.Sprint(FormatVersion),
		}))
	}

	if strings.TrimSpace(p.Name) == "" {
		errs = append(errs, errf("name", "missing", nil))
	}
	errs = append(errs, checkText("name", p.Name)...)
	errs = append(errs, checkText("author", p.Author)...)
	errs = append(errs, checkProse("description", p.Description)...)
	errs = append(errs, checkText("match.vendor", p.Match.Vendor)...)

	if p.IntervalSec == 0 {
		// Not defaulted. A preset that does not say how often it polls cannot
		// have its cost stated before it runs, which is the whole point of
		// naming the cost at bind time.
		errs = append(errs, errf("intervalSec", "missing", nil))
	} else if p.IntervalSec < MinIntervalSec || p.IntervalSec > MaxIntervalSec {
		errs = append(errs, errf("intervalSec", "outOfRange", map[string]string{
			"min": fmt.Sprint(MinIntervalSec), "max": fmt.Sprint(MaxIntervalSec),
		}))
	}

	for i, prefix := range p.Match.SysObjectIDPrefix {
		errs = append(errs, checkOID(fmt.Sprintf("match.sysObjectIdPrefix[%d]", i), prefix)...)
	}

	if len(p.Widgets) == 0 {
		errs = append(errs, errf("widgets", "empty", nil))
	}
	if len(p.Widgets) > MaxWidgets {
		errs = append(errs, errf("widgets", "tooMany", map[string]string{
			"found": fmt.Sprint(len(p.Widgets)), "max": fmt.Sprint(MaxWidgets),
		}))
	}

	total := 0
	for i, w := range p.Widgets {
		at := fmt.Sprintf("widgets[%d]", i)
		errs = append(errs, checkText(at+".title", w.Title)...)
		errs = append(errs, checkText(at+".unit", w.Unit)...)

		doc, known := widgetDoc(w.Kind)
		if !known {
			errs = append(errs, errf(at+".kind", "unknownKind", map[string]string{"kind": clip(w.Kind)}))
		}
		if len(w.OIDs) == 0 {
			errs = append(errs, errf(at+".oids", "empty", nil))
		}
		if known && doc.MaxOIDs > 0 && len(w.OIDs) > doc.MaxOIDs {
			errs = append(errs, errf(at+".oids", "tooManyForKind", map[string]string{
				"kind": w.Kind, "found": fmt.Sprint(len(w.OIDs)), "max": fmt.Sprint(doc.MaxOIDs),
			}))
		}
		for j, oid := range w.OIDs {
			// A template is checked as the OID it becomes: substituting a
			// single instance keeps every other rule — the arcs, the depth,
			// the root — while allowing the one thing that is not a digit.
			probe := strings.ReplaceAll(oid, InstancePlaceholder, "1")
			errs = append(errs, checkOID(fmt.Sprintf("%s.oids[%d]", at, j), probe)...)
		}
		errs = append(errs, checkDiscovery(at, w)...)
		errs = append(errs, checkLayout(at+".layout", w.Layout)...)
		errs = append(errs, checkThreshold(at+".threshold", w.Kind, w.Threshold)...)
		errs = append(errs, checkMap(at, w)...)
		// A discovering widget contributes its BOUND, not its template count:
		// the sanity limit has to hold after the walk, and the walk happens on
		// equipment nobody has seen yet.
		if w.Discover != nil {
			total += len(w.OIDs) * discoverInstanceLimit(w)
		} else {
			total += len(w.OIDs)
		}

		if len(w.Labels) > 0 && !kindTakesLabels(w.Kind) {
			errs = append(errs, errf(at+".labels", "labelsOnlyForStatus", map[string]string{"kind": clip(w.Kind)}))
		}
		for k, v := range w.Labels {
			// The KEY as well as the value. It is author-supplied text that is
			// interpolated into Error.Field and rendered as the name of the
			// state it maps, so a newline or a hundred characters in it travel
			// exactly as far as one in the value would.
			if _, err := strconv.ParseInt(k, 10, 64); err != nil {
				errs = append(errs, errf(at+".labels", "labelKey", map[string]string{"key": clip(k)}))
				continue
			}
			errs = append(errs, checkText(at+".labels."+k, v)...)
		}
	}

	// After the loop, because both of these are relationships BETWEEN widgets
	// rather than properties of either one.
	errs = append(errs, checkLayoutOverlaps(p.Widgets)...)
	errs = append(errs, checkThresholdConflicts(p.Widgets)...)

	if total > MaxOIDsPerPreset {
		errs = append(errs, errf("widgets", "tooManyOids", map[string]string{
			"found": fmt.Sprint(total), "max": fmt.Sprint(MaxOIDsPerPreset),
		}))
	}

	return errs
}

// Cost is what binding this preset will do, in numbers that can be stated
// before anything is polled.
//
// Arithmetic, not a prediction: it says how much will be ASKED, never how long
// it will take. What it takes is measured once it runs, and that measurement is
// what the guardrail acts on — sixty OIDs against a chassis on the same switch
// cost less than five over a satellite link, so a number derived from the count
// alone would be a guess dressed as a fact.
type Cost struct {
	OIDs        int `json:"oids"`
	Widgets     int `json:"widgets"`
	IntervalSec int `json:"intervalSec"`
	// PollsPerDay and VarbindsPerDay are stated over a DAY, not an hour.
	//
	// Not a presentation choice: MaxIntervalSec is a day, so an hourly base is
	// integer-divided to ZERO for every cadence slower than 3600 s — a preset
	// polling twice a day reported "0 requests, 0 varbinds" on the one screen
	// whose job is to say what it will cost. A day is the coarsest cadence the
	// format admits, so the count is at least one for every valid preset.
	PollsPerDay int `json:"pollsPerDay"`
	// Discovered is how many of the OIDs counted above come from a BOUND rather
	// than from a list.
	//
	// When it is not zero, every figure here is an UPPER BOUND: a discovering
	// widget's real count is whatever the walk finds on the equipment, and that
	// is not knowable from the file. The screen showing this says so, because
	// "at most 3 072 values a day" and "3 072 values a day" are different
	// statements and only one of them is true here.
	Discovered int `json:"discovered"`
	// VarbindsPerDay is the honest measure of what the device is asked for:
	// one request carries every OID, so counting rounds alone understates what
	// the agent does work for.
	//
	// There is no REQUEST count here on purpose. How many requests a round
	// takes depends on the transport's chunk size, which is pkg/snmp's to know
	// and not this package's — a copy of it here is a copy that drifts.
	VarbindsPerDay int `json:"varbindsPerDay"`
	// Watched is how many readings carry a band, and Alerting how many of those
	// route an episode to the notification sinks.
	//
	// Alerting is the one figure here that is not about this machine: a preset
	// with it set will, on a breach, send mail or POST to a webhook the operator
	// configured. That is the file reaching outside, so it is stated before
	// binding beside the request count rather than discovered afterwards.
	Watched  int `json:"watched"`
	Alerting int `json:"alerting"`
}

// secondsPerDay is the base Estimate divides. Equal to MaxIntervalSec, and
// that is the point: the slowest cadence the format admits still counts one.
const secondsPerDay = 86400

// Estimate reports what one target bound to this preset will ask for.
func Estimate(p Preset) Cost {
	c := Cost{Widgets: len(p.Widgets), IntervalSec: p.IntervalSec}
	// Two widgets showing the same OID are polled once: the scheduler asks for
	// a SET, and counting it twice would overstate the cost of exactly the
	// presets that are well written. The watched set is counted separately
	// because a band and a reading are different things to deduplicate — one
	// widget may watch an OID that another merely draws.
	seen, watched := map[string]bool{}, map[string]bool{}

	for _, w := range p.Widgets {
		per := 0
		if w.Discover != nil {
			// Not yet walked: the ceiling the preset agreed to, counted once
			// per template. Deduplication cannot apply — the OIDs do not exist
			// yet — so this is the only figure available before binding.
			per = discoverInstanceLimit(w)
		}
		for _, oid := range w.OIDs {
			n := 1
			key := normaliseOID(oid)
			if hasPlaceholder(oid) {
				if per == 0 {
					continue
				}
				n = per
				key = "" // a template is never the same reading twice
			}

			if key == "" || !seen[key] {
				seen[key] = true
				c.OIDs += n
				if key == "" {
					c.Discovered += n
				}
			}
			if w.Threshold != nil && (key == "" || !watched[key]) {
				watched[key] = true
				c.Watched += n
				if w.Threshold.Alerts() {
					c.Alerting += n
				}
			}
		}
	}

	if c.IntervalSec <= 0 {
		return c
	}
	c.PollsPerDay = secondsPerDay / c.IntervalSec
	c.VarbindsPerDay = c.PollsPerDay * c.OIDs
	return c
}

// PollOIDs is the deduplicated set the scheduler should ask for, in a stable
// order so a bound preset polls the same way on every run.
func PollOIDs(p Preset) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, w := range p.Widgets {
		for _, oid := range w.OIDs {
			// A template is not an OID. Expand() runs before this at bind time,
			// so reaching one here means somebody polled an unexpanded preset —
			// and "1.3.6.1.2.1.2.2.1.8.{#}" on the wire is a failure far from
			// its cause.
			if hasPlaceholder(oid) {
				continue
			}
			key := normaliseOID(oid)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// hasPlaceholder reports whether an OID is a discovery TEMPLATE rather than an
// address. One rule, because three places ask: validation substitutes an
// instance before checking it, PollOIDs refuses to put one on the wire, and a
// threshold cannot be keyed by an OID that does not exist yet.
func hasPlaceholder(oid string) bool { return strings.Contains(oid, InstancePlaceholder) }

// normaliseOID is the form an OID is keyed and polled by. ".1.3.6.1" and
// "1.3.6.1" are the same reading, and a map keyed by the raw string would hold
// two entries for it — so the leading dot comes off once, here.
func normaliseOID(oid string) string { return strings.TrimPrefix(oid, ".") }

// checkText bounds a LABEL. checkProse bounds the one field that is a
// paragraph; both refuse control characters, which is the half that is about
// what travels rather than what fits.
func checkText(field, s string) []Error { return checkBounded(field, s, MaxTextLen) }

func checkProse(field, s string) []Error { return checkBounded(field, s, MaxDescriptionLen) }

func checkBounded(field, s string, max int) []Error {
	if s == "" {
		return nil
	}
	var errs []Error
	// Runes, not bytes. MaxTextLen is about LAYOUT, which is a property of what
	// is displayed — and this application ships a zh locale, where a
	// forty-character title is a hundred and twenty bytes and would be refused
	// for being too long to fit in a panel it fits in comfortably.
	if n := utf8.RuneCountInString(s); n > max {
		errs = append(errs, errf(field, "tooLong", map[string]string{
			"found": fmt.Sprint(n), "max": fmt.Sprint(max),
		}))
	}
	// A control character in a title reaches a panel, a log line and — through
	// the event journal — a syslog collector. pkg/notify escapes them on the
	// way out; refusing them on the way IN means they never travel.
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			errs = append(errs, errf(field, "controlCharacter", nil))
			break
		}
	}
	return errs
}

// clip shortens a value for an error message, on a RUNE boundary.
//
// Slicing bytes splits a multi-byte rune, and the result travels: Error.Args
// crosses the bridge as JSON, where encoding/json rewrites the broken tail to
// U+FFFD — so the message naming the offending value would misquote it.
func clip(s string) string {
	const max = 40
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	n := 0
	for i := range s {
		if n == max {
			return s[:i] + "…"
		}
		n++
	}
	return s
}
