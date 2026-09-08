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
	"strings"
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
	// MinIntervalSec floors the declared cadence. Below this a preset is
	// asking the poll clock for something the scheduler will floor anyway.
	MinIntervalSec = 5
	// MaxIntervalSec is a day: past that, a dashboard is a report.
	MaxIntervalSec = 86400
	// MaxTextLen caps every author-supplied string. A preset's title lands in
	// a panel, and a title the length of a novel is a layout attack whatever
	// the intent.
	MaxTextLen = 120
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
	// KindTable shows a conceptual table, split by INDEX.
	KindTable = "table"
)

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
	{Kind: KindTable, MaxOIDs: 0},
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
	// Labels maps an integer reading to a name, for KindStatus. Nothing else
	// reads it.
	Labels map[string]string `json:"labels,omitempty"`
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

// Validate checks a preset completely and returns every problem, not the first.
//
// Everything, because a preset is edited in a text editor by someone who is not
// looking at this code: reporting one error per attempt turns a five-minute fix
// into five round trips.
func Validate(p Preset) []Error {
	var errs []Error

	if p.FormatVersion == 0 {
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
	errs = append(errs, checkText("description", p.Description)...)
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
		if !numericOID.MatchString(prefix) {
			errs = append(errs, errf(fmt.Sprintf("match.sysObjectIdPrefix[%d]", i), "notAnOid",
				map[string]string{"value": clip(prefix)}))
		}
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
			if !numericOID.MatchString(oid) {
				errs = append(errs, errf(fmt.Sprintf("%s.oids[%d]", at, j), "notAnOid",
					map[string]string{"value": clip(oid)}))
			}
		}
		total += len(w.OIDs)

		if w.Kind != KindStatus && len(w.Labels) > 0 {
			errs = append(errs, errf(at+".labels", "labelsOnlyForStatus", map[string]string{"kind": w.Kind}))
		}
		for k, v := range w.Labels {
			errs = append(errs, checkText(at+".labels."+k, v)...)
		}
	}

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
	OIDs            int `json:"oids"`
	Widgets         int `json:"widgets"`
	IntervalSec     int `json:"intervalSec"`
	RequestsPerHour int `json:"requestsPerHour"`
	// VarbindsPerHour is the honest measure of what the device is asked for:
	// one request carries every OID, so the request count alone understates it
	// and the varbind count is what the agent actually does work for.
	VarbindsPerHour int `json:"varbindsPerHour"`
}

// Estimate reports what one target bound to this preset will ask for.
func Estimate(p Preset) Cost {
	c := Cost{Widgets: len(p.Widgets), IntervalSec: p.IntervalSec}
	seen := map[string]bool{}
	for _, w := range p.Widgets {
		for _, oid := range w.OIDs {
			// Two widgets showing the same OID are polled once: the scheduler
			// asks for a set, and counting it twice would overstate the cost
			// of exactly the presets that are well written.
			key := strings.TrimPrefix(oid, ".")
			if seen[key] {
				continue
			}
			seen[key] = true
			c.OIDs++
		}
	}
	if c.IntervalSec <= 0 {
		return c
	}
	perHour := 3600 / c.IntervalSec
	c.RequestsPerHour = perHour
	c.VarbindsPerHour = perHour * c.OIDs
	return c
}

// PollOIDs is the deduplicated set the scheduler should ask for, in a stable
// order so a bound preset polls the same way on every run.
func PollOIDs(p Preset) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, w := range p.Widgets {
		for _, oid := range w.OIDs {
			key := strings.TrimPrefix(oid, ".")
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func checkText(field, s string) []Error {
	if s == "" {
		return nil
	}
	var errs []Error
	if len(s) > MaxTextLen {
		errs = append(errs, errf(field, "tooLong", map[string]string{
			"found": fmt.Sprint(len(s)), "max": fmt.Sprint(MaxTextLen),
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

func clip(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}
