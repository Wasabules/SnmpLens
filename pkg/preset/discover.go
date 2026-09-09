package preset

import (
	"fmt"
	"sort"
	"strings"
)

// Discovery: a preset that works on the equipment in front of you.
//
// The presets that shipped first enumerated `…2.2.1.8.1` through `.24` by hand
// and their descriptions said "edit the instance numbers if yours does not
// index from 1". That is an admission: a preset written for a 24-port switch is
// a preset for exactly one shape of switch, and a library of those is not
// shareable.
//
// So a widget may say WHERE to find its instances instead of listing them. One
// walk, at BIND TIME, turns the templates into concrete OIDs which are then
// stored in the session exactly as a hand-written preset's are — so the poll
// path stays GET-only, the cost is arithmetic before the first tick, and the
// guardrail sees no difference. Nothing about discovery survives into polling.
//
// The walked COLUMN is the label source as well as the instance source: walking
// ifDescr gives you both "which interfaces exist" and "what they are called",
// which is why a discovered port grid reads "Gi0/1" rather than "8".

// InstancePlaceholder is what a template OID carries where the instance goes.
const InstancePlaceholder = "{#}"

// MaxDiscoveredPerWidget bounds one walk when the preset does not.
//
// A default rather than a requirement, because the number somebody would write
// is a guess about equipment they have not seen — and the honest bound is the
// one that stops a walk of a 40 000-row table from becoming a dashboard.
const MaxDiscoveredPerWidget = 128

// Discover says where a widget's instances come from.
type Discover struct {
	// Walk is the numeric OID of a COLUMN to walk. Its instances become the
	// widget's, and its values become their labels.
	Walk string `json:"walk"`
	// Max bounds what one walk may produce for this widget. Zero means
	// MaxDiscoveredPerWidget.
	Max int `json:"max,omitempty"`
}

// Instance is one row a walk found.
type Instance struct {
	// Suffix is what follows the walked column's OID — "1", or "1.3.6.1.2" for
	// a table indexed by an address.
	Suffix string `json:"suffix"`
	// Label is the walked value, which is what makes a discovered grid readable.
	Label string `json:"label"`
}

// HasDiscovery reports whether anything in this preset needs a walk before it
// can be polled.
func HasDiscovery(p Preset) bool {
	for _, w := range p.Widgets {
		if w.Discover != nil {
			return true
		}
	}
	return false
}

// discoverInstanceLimit is how many INSTANCES one widget may take.
//
// Two bounds meet here and the smaller wins, which is the whole of the rule:
//
//   - the WALK bound, declared or defaulted, which stops a forty-thousand-row
//     table from becoming a dashboard;
//   - the KIND's own MaxOIDs, which is what the widget can actually draw.
//
// Discovering ninety-six states for a grid that renders ninety-six of them is
// right; discovering a hundred and twenty-eight is polling thirty-two readings
// nobody sees. And a chart is capped at eight series by the palette, so a chart
// discovering an in and an out counter can show four ports — which is a real
// limitation and is better stated than worked around.
func discoverInstanceLimit(w Widget) int {
	if w.Discover == nil {
		return 0
	}
	limit := MaxDiscoveredPerWidget
	if w.Discover.Max > 0 {
		limit = w.Discover.Max
	}

	templates := 0
	for _, oid := range w.OIDs {
		if strings.Contains(oid, InstancePlaceholder) {
			templates++
		}
	}
	if templates == 0 {
		return 0
	}

	if doc, known := widgetDoc(w.Kind); known && doc.MaxOIDs > 0 {
		// The fixed OIDs of a mixed widget take their share of the kind's room
		// before the discovered ones get any.
		room := doc.MaxOIDs - (len(w.OIDs) - templates)
		if room < 0 {
			room = 0
		}
		if byKind := room / templates; byKind < limit {
			limit = byKind
		}
	}
	return limit
}

// ExpandWidget turns one widget's templates into concrete OIDs.
//
// Order is the walk's, which is the agent's own order for the column — the
// order the ports are in on the device, not alphabetical and not numeric on a
// string. Templates expand INSTANCE-MAJOR: every OID of instance 1, then every
// OID of instance 2. For a grid that is one cell per port; for a chart it keeps
// a port's in and out counters next to each other rather than interleaving
// twenty-four ins with twenty-four outs.
//
// A widget with no Discover comes back unchanged, so a caller can run this over
// every widget without asking first.
func ExpandWidget(w Widget, found []Instance) Widget {
	if w.Discover == nil {
		return w
	}
	if limit := discoverInstanceLimit(w); len(found) > limit {
		found = found[:limit]
	}

	out := w
	out.Discover = nil // it has been done; what remains is a plain widget
	out.OIDs = make([]string, 0, len(w.OIDs)*len(found))
	labels := map[string]string{}

	for _, in := range found {
		for _, tmpl := range w.OIDs {
			if !strings.Contains(tmpl, InstancePlaceholder) {
				continue
			}
			oid := strings.ReplaceAll(tmpl, InstancePlaceholder, in.Suffix)
			out.OIDs = append(out.OIDs, oid)
			if in.Label != "" {
				labels[oid] = in.Label
			}
		}
	}

	// An OID that carried no placeholder is kept as it was: a widget may mix a
	// fixed reading with discovered ones.
	for _, tmpl := range w.OIDs {
		if !strings.Contains(tmpl, InstancePlaceholder) {
			out.OIDs = append(out.OIDs, tmpl)
		}
	}

	if len(labels) > 0 {
		out.OIDLabels = labels
	}
	return out
}

// Expand runs ExpandWidget over a preset, given what each widget's walk found.
//
// Keyed by widget INDEX because a widget has no identity of its own — a title
// is author text and two widgets may share one. The caller walked in the same
// order, so the index is the only thing that cannot drift.
func Expand(p Preset, found map[int][]Instance) Preset {
	out := p
	out.Widgets = make([]Widget, len(p.Widgets))
	for i, w := range p.Widgets {
		out.Widgets[i] = ExpandWidget(w, found[i])
	}
	return out
}

// DiscoveryWalks lists the columns that have to be walked, deduplicated.
//
// Two widgets discovering from the same column — a grid of states and a chart
// of counters, both indexed by interface — is the ordinary case, and it is one
// walk, not two.
func DiscoveryWalks(p Preset) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, w := range p.Widgets {
		if w.Discover == nil {
			continue
		}
		key := strings.TrimPrefix(w.Discover.Walk, ".")
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// InstancesFrom turns a walk's results into instances.
//
// The walked OIDs come back with the column's own prefix; what identifies a row
// is what FOLLOWS it. A result outside the column is ignored rather than
// mis-parsed: gosnmp's walk stops at the subtree, but a device answering
// something else must not become a row addressed at random.
func InstancesFrom(column string, oids []string, valueOf func(string) string) []Instance {
	prefix := strings.TrimPrefix(strings.TrimSpace(column), ".")
	if prefix == "" {
		return nil
	}
	out := []Instance{}
	for _, raw := range oids {
		oid := strings.TrimPrefix(strings.TrimSpace(raw), ".")
		if !strings.HasPrefix(oid, prefix+".") {
			continue
		}
		suffix := oid[len(prefix)+1:]
		if suffix == "" {
			continue
		}
		out = append(out, Instance{Suffix: suffix, Label: valueOf(raw)})
	}
	return out
}

// checkDiscovery validates a widget's discovery declaration.
func checkDiscovery(at string, w Widget) []Error {
	templates := 0
	for _, oid := range w.OIDs {
		if strings.Contains(oid, InstancePlaceholder) {
			templates++
		}
	}

	if w.Discover == nil {
		if templates > 0 {
			return []Error{errf(at+".oids", "placeholderWithoutDiscover", nil)}
		}
		return nil
	}

	var errs []Error
	errs = append(errs, checkOID(at+".discover.walk", w.Discover.Walk)...)
	if templates == 0 {
		// Otherwise the walk is paid for and nothing uses it, which reads as a
		// preset that discovers and then ignores what it found.
		errs = append(errs, errf(at+".oids", "discoverWithoutPlaceholder", map[string]string{
			"placeholder": InstancePlaceholder,
		}))
	}
	if w.Discover.Max < 0 || w.Discover.Max > MaxOIDsPerPreset {
		errs = append(errs, errf(at+".discover.max", "outOfRange", map[string]string{
			"min": "1", "max": fmt.Sprint(MaxOIDsPerPreset),
		}))
	}
	return errs
}
