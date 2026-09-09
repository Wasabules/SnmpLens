// Where a preset's widgets go, turned into something CSS grid can use.
//
// Pure and separate from the panel for the reason tableRows.js is: the rules
// here are arithmetic with an off-by-one in the middle of them (a preset counts
// from 0, a grid line counts from 1) and that is worth testing without a
// browser.
//
// The panel never places anything itself. It sets custom properties and lets
// the stylesheet do the placement, which is what allows a media query to
// UNDO it on a narrow window: an inline `grid-column` would win over any rule,
// and the dashboard would keep its twelve columns at 320 px.

// The width of the grid a preset places widgets on. Matches
// preset.LayoutColumns; the two are one number and the Go side owns it.
export const LAYOUT_COLUMNS = 12;

// The tallest row a preset may place a widget on, matching preset.MaxLayoutRow.
// Used only to put unplaced widgets after every placed one, which needs a
// number no placed widget can reach rather than one that is usually bigger.
export const MAX_LAYOUT_ROW = 64;

/** Whether anything in this preset places itself. */
export function hasLayout(widgets) {
  return (widgets || []).some((w) => w && w.layout);
}

/**
 * The custom properties one widget needs, or '' when it places itself nowhere.
 *
 * Grid lines are 1-based and a preset's coordinates are 0-based, which is the
 * whole of the conversion: a widget at x: 0 starts at line 1.
 */
export function cellStyle(widget) {
  const l = widget && widget.layout;
  if (!l) return '';
  const n = (v, fallback) => (Number.isFinite(Number(v)) ? Number(v) : fallback);
  const x = Math.max(0, n(l.x, 0));
  const y = Math.max(0, n(l.y, 0));
  const w = Math.max(1, n(l.w, 1));
  const h = Math.max(1, n(l.h, 1));
  return `--x:${x + 1};--w:${w};--y:${y + 1};--h:${h}`;
}

/**
 * The order the widgets read in when the grid collapses to one column.
 *
 * Below the breakpoint every widget is full width and the arrangement is gone,
 * so what is left is a sequence — and the sequence that means something is the
 * author's, top to bottom then left to right. Without this the collapsed
 * dashboard falls back to DECLARATION order, which is a different list whenever
 * an author added a widget at the end and placed it at the top.
 *
 * A widget that places itself nowhere comes after the ones that do, in
 * declaration order: it is the one the author did not arrange, so there is no
 * arrangement to honour.
 */
export function readingOrder(widgets) {
  const list = widgets || [];
  const unplacedBase = (MAX_LAYOUT_ROW + 1) * LAYOUT_COLUMNS;
  return list.map((w, i) => {
    const l = w && w.layout;
    if (!l) return unplacedBase + i;
    const y = Math.max(0, Number(l.y) || 0);
    const x = Math.max(0, Number(l.x) || 0);
    return y * LAYOUT_COLUMNS + x;
  });
}
