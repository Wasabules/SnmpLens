package preset

import (
	"fmt"
	"sort"
	"strconv"
)

// Thresholds: what the preset's author thinks is too much.
//
// The first version deliberately carried none, with a comment saying that what
// counts as too much is the operator's judgement about their own network. That
// is true of a percentage on a link the author has never seen and false of
// almost everything else a preset is written for: a port that is not up(1), a
// UPS running on battery, a disk at 95%. Those are properties of the MIB, not
// of the network, and the person who wrote the preset knows them better than
// the person binding it.
//
// So a widget may carry a band, and binding materialises it into the session's
// own threshold map — the same column MonitorCreateSession fills, read by the
// same evaluator. Nothing new evaluates anything; a preset simply fills in a
// form the operator would otherwise fill in by hand, and can then edit.
//
// Two facts decide the whole shape of this, and both are measured rather than
// assumed:
//
//   - The evaluator compares Sample.Value, which is the RAW reading. Rate is
//     derived for the chart and the tile and is never evaluated. So a band on a
//     counter is a band on a number that only goes up: it fires on the first
//     sample and never resolves. A `rate` widget SAYS its OID is a counter, so
//     that one is refused here rather than discovered in production.
//   - AlertEnabled does not mean "notify"; it is the switch that decides
//     whether the band is EVALUATED AT ALL. `classify` returns nothing without
//     it, in Go and in the renderer alike, so a band with it off opens no
//     episode and records nothing — it is drawn on the chart as a reference
//     line and that is the whole of its effect. An omitted field decoding to
//     false would therefore make every threshold an author writes inert, with
//     the dashboard looking exactly as though it were armed. Hence a POINTER,
//     and hence Alerts() below: absent means yes.
//
// An evaluated band raises an incident, which the operator's own notification
// rules may then route to a sink. That is the preset reaching outside the
// machine, so it is COUNTED IN THE COST and stated before binding, beside the
// request count — the same gate the traffic goes through.

// MaxThresholdHoldSec bounds ForSeconds.
//
// A day, matching MaxIntervalSec: a hold longer than that is not a hold, it is
// a threshold that never fires, and the author meant something else.
const MaxThresholdHoldSec = 86400

// Threshold is a band one widget's readings should stay inside.
//
// The field names are storage.Thresholds', deliberately: the same vocabulary in
// the file, in the database and in the evaluator means one thing to learn and
// nothing to translate.
type Threshold struct {
	// Min and Max are the band. At least one is required — a band with no
	// bound is a threshold that cannot fire, which is not a default, it is a
	// mistake with no symptom.
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
	// ForSeconds is how long a reading must stay outside the band before it
	// counts. 0 fires on the first sample, which is right for a state and wrong
	// for anything that fluctuates.
	ForSeconds int `json:"forSeconds,omitempty"`
	// AlertEnabled decides whether the band is evaluated. ABSENT MEANS YES:
	// somebody writing a threshold means it to fire, and a plain bool would
	// make the ordinary preset — one that simply omits the field — carry a
	// band that is never compared to anything. Set to false explicitly, the
	// band is a reference line on the chart and nothing more.
	AlertEnabled *bool `json:"alertEnabled,omitempty"`
}

// Alerts reports whether this band is evaluated at all. See AlertEnabled.
func (t Threshold) Alerts() bool { return t.AlertEnabled == nil || *t.AlertEnabled }

// Equal reports whether two bands say the same thing.
//
// Needed because a threshold map is keyed by OID and two widgets may show the
// same reading: identical bands are one band, and different ones are a question
// only the author can answer.
func (t Threshold) Equal(other Threshold) bool {
	same := func(a, b *float64) bool {
		if a == nil || b == nil {
			return a == nil && b == nil
		}
		return *a == *b
	}
	return same(t.Min, other.Min) && same(t.Max, other.Max) &&
		t.ForSeconds == other.ForSeconds && t.Alerts() == other.Alerts()
}

// ThresholdsFor maps every OID a preset watches to the band it should stay in.
//
// Per WIDGET rather than per OID, because a band belongs to what the widget
// MEANS: "no port may be anything but up" is one statement about twenty-four
// readings, and after discovery it is one statement about however many the
// equipment turned out to have. Writing it per OID would make it unwriteable
// for a discovering preset — the OIDs do not exist yet.
func ThresholdsFor(p Preset) map[string]Threshold {
	out := map[string]Threshold{}
	for _, w := range p.Widgets {
		if w.Threshold == nil {
			continue
		}
		for _, oid := range w.OIDs {
			// A template is not an OID. Discovery replaces it before this is
			// ever asked, and a preset bound before that has nothing to watch
			// here.
			if hasPlaceholder(oid) {
				continue
			}
			out[normaliseOID(oid)] = *w.Threshold
		}
	}
	return out
}

// checkThreshold validates one band.
func checkThreshold(at string, kind string, t *Threshold) []Error {
	if t == nil {
		return nil
	}
	var errs []Error

	if t.Min == nil && t.Max == nil {
		errs = append(errs, errf(at, "thresholdWithoutBound", nil))
	}
	if t.Min != nil && t.Max != nil && *t.Min > *t.Max {
		errs = append(errs, errf(at, "thresholdInverted", map[string]string{
			"min": trimFloat(*t.Min), "max": trimFloat(*t.Max),
		}))
	}
	if t.ForSeconds < 0 || t.ForSeconds > MaxThresholdHoldSec {
		errs = append(errs, errf(at+".forSeconds", "outOfRange", map[string]string{
			"min": "0", "max": fmt.Sprint(MaxThresholdHoldSec),
		}))
	}

	// The one kind that cannot carry a band. `rate` says its OID is a counter,
	// the evaluator compares the RAW reading, and a counter only goes up: the
	// episode opens on the first sample and never closes. Refused here because
	// the author cannot see it — the tile beside it shows the rate they meant.
	if kind == KindRate {
		errs = append(errs, errf(at, "thresholdOnRate", map[string]string{"kind": kind}))
	}
	return errs
}

// checkThresholdConflicts refuses two different bands on one reading.
//
// The session holds ONE map keyed by OID, so two widgets watching the same OID
// with different bands is a question with no answer: whichever is written last
// would win silently, and the other widget would be drawn under a rule that is
// not being applied to it.
func checkThresholdConflicts(widgets []Widget) []Error {
	type claim struct {
		widget int
		band   Threshold
	}
	first := map[string]claim{}
	seen := map[string]bool{}
	var errs []Error

	for i, w := range widgets {
		if w.Threshold == nil {
			continue
		}
		for _, oid := range w.OIDs {
			if hasPlaceholder(oid) {
				continue
			}
			key := normaliseOID(oid)
			held, taken := first[key]
			if !taken {
				first[key] = claim{widget: i, band: *w.Threshold}
				continue
			}
			if held.band.Equal(*w.Threshold) {
				continue // one band said twice is one band
			}
			if seen[key] {
				continue // already reported for this reading
			}
			seen[key] = true
			errs = append(errs, errf(
				fmt.Sprintf("widgets[%d].threshold", i), "thresholdConflict",
				map[string]string{"oid": clip(key), "other": fmt.Sprint(held.widget + 1)}))
		}
	}
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Field < errs[j].Field })
	return errs
}

// trimFloat prints a bound the way an author wrote it, without a trailing zero
// nobody typed.
func trimFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
