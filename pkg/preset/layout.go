package preset

import (
	"fmt"
	"sort"
)

// Layout: where a widget goes, decided by whoever wrote the preset.
//
// Without this a preset describes what to poll and says nothing about the
// arrangement, so every dashboard is the same reflowed list of cards in
// declaration order. That is fine for four widgets and wrong for the thing a
// preset is FOR: an author who knows the equipment knows that the port wall
// belongs across the top and the two uplink counters belong side by side under
// it, and had no way to say so.
//
// It is a GRID, not coordinates. Twelve columns and rows that size themselves
// to their content, which is the arrangement every dashboard tool converged on
// for the same reason: it survives a window being resized, and it collapses to
// one column on a narrow one without the author writing a second layout.
//
// Deliberately NOT pixels, and deliberately not a free-form canvas. A preset is
// a file a stranger wrote; a coordinate system it can place things at exactly
// is one it can place things OUTSIDE, or on top of the footnote that says where
// the data came from.

const (
	// LayoutColumns is the width of the grid a preset places widgets on.
	LayoutColumns = 12
	// MaxLayoutRow bounds Y. A dashboard sixty-four rows tall is already past
	// what anyone scrolls; the bound exists so a file saying y = 1000000 is
	// refused here rather than becoming a million empty CSS rows.
	MaxLayoutRow = 64
	// MaxLayoutHeight bounds H, for the same reason.
	MaxLayoutHeight = 12
)

// Layout is where one widget sits on the grid.
//
// X and Y are zero-based; W and H are spans. H is optional and defaults to one
// row, because most widgets are one row tall and making every author write it
// buys nothing.
type Layout struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h,omitempty"`
}

// Height is H with its default applied.
func (l Layout) Height() int {
	if l.H <= 0 {
		return 1
	}
	return l.H
}

// HasLayout reports whether anything in this preset places itself.
//
// The renderer needs this because the two arrangements are different grids: a
// preset that places nothing keeps the reflowing list of cards it has always
// had, and switching that to a twelve-column grid would change every existing
// dashboard to make room for a feature they do not use.
func HasLayout(p Preset) bool {
	for _, w := range p.Widgets {
		if w.Layout != nil {
			return true
		}
	}
	return false
}

// checkLayout validates one placement.
func checkLayout(at string, l *Layout) []Error {
	if l == nil {
		return nil
	}
	var errs []Error
	rng := func(field string, v, min, max int) {
		if v < min || v > max {
			errs = append(errs, errf(field, "outOfRange", map[string]string{
				"min": fmt.Sprint(min), "max": fmt.Sprint(max),
			}))
		}
	}
	rng(at+".x", l.X, 0, LayoutColumns-1)
	rng(at+".y", l.Y, 0, MaxLayoutRow)
	rng(at+".w", l.W, 1, LayoutColumns)
	if l.H != 0 {
		rng(at+".h", l.H, 1, MaxLayoutHeight)
	}

	// Checked separately from the ranges, because "x is 10 and w is 6" is two
	// legal numbers and one widget hanging off the right of the grid — which
	// the browser answers by silently growing a thirteenth column and putting
	// it in the next row.
	if l.W >= 1 && l.X >= 0 && l.X+l.W > LayoutColumns {
		errs = append(errs, errf(at, "layoutOverflow", map[string]string{
			"x": fmt.Sprint(l.X), "w": fmt.Sprint(l.W),
			"columns": fmt.Sprint(LayoutColumns),
		}))
	}
	return errs
}

// checkLayoutOverlaps refuses two widgets placed on the same cells.
//
// Two cards drawn on top of each other is not a taste question and it is not
// something the renderer can resolve: CSS grid stacks them, so the one written
// second covers the one written first and the dashboard is missing a widget
// that is right there in the file. An overlap is always a mistake, and the only
// moment anyone can act on it is while reading the error list.
//
// Only PLACED widgets are compared. An unplaced one is auto-placed by the
// browser, which by definition skips cells that are already taken.
func checkLayoutOverlaps(widgets []Widget) []Error {
	type box struct {
		i              int
		x0, y0, x1, y1 int
	}
	var boxes []box
	for i, w := range widgets {
		l := w.Layout
		if l == nil || l.W < 1 || l.X < 0 || l.Y < 0 {
			continue // its own errors are reported by checkLayout
		}
		boxes = append(boxes, box{
			i: i, x0: l.X, y0: l.Y,
			x1: l.X + l.W, y1: l.Y + l.Height(),
		})
	}

	var errs []Error
	for a := range boxes {
		for b := a + 1; b < len(boxes); b++ {
			p, q := boxes[a], boxes[b]
			if p.x0 < q.x1 && q.x0 < p.x1 && p.y0 < q.y1 && q.y0 < p.y1 {
				// Reported against the LATER widget: the earlier one is where
				// the author started, and pointing at both says nothing about
				// which to move.
				errs = append(errs, errf(
					fmt.Sprintf("widgets[%d].layout", q.i), "layoutOverlap",
					map[string]string{"other": fmt.Sprint(p.i + 1)}))
			}
		}
	}
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Field < errs[j].Field })
	return errs
}
