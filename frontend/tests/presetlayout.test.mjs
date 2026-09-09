// The grid a preset places its widgets on is ONE number, and it is written in
// three places: preset.LayoutColumns validates against it, presetLayout.js
// converts coordinates with it, and DashboardPanel's stylesheet declares the
// columns. Nothing connects them.
//
// Drift is silent in the worst direction. If Go accepted twelve columns and the
// stylesheet drew ten, a widget at x: 10 would be validated, bound, and then
// placed on a column that does not exist — CSS grid answers that by growing an
// implicit eleventh column, so the dashboard silently gains a column nobody
// declared and every row below it moves. No error anywhere.
//
// The rest is the off-by-one in the middle of the conversion: a preset counts
// from 0 and a grid line counts from 1.
import { readFileSync } from 'node:fs';
import {
  hasLayout,
  cellStyle,
  readingOrder,
  LAYOUT_COLUMNS,
  MAX_LAYOUT_ROW,
} from '../src/utils/presetLayout.js';

const root = new URL('../../', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');
const read = (p) => readFileSync(root + p, 'utf8');

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

/* --- the constant, in all three places ------------------------------------ */
{
  const go = read('pkg/preset/layout.go');
  const goColumns = Number(/LayoutColumns\s*=\s*(\d+)/.exec(go)?.[1]);
  const goRows = Number(/MaxLayoutRow\s*=\s*(\d+)/.exec(go)?.[1]);

  check('pkg/preset declares a column count', Number.isFinite(goColumns), String(goColumns));
  check('pkg/preset declares a row bound', Number.isFinite(goRows), String(goRows));
  check(
    'the renderer agrees on the column count',
    goColumns === LAYOUT_COLUMNS,
    `Go ${goColumns} vs JS ${LAYOUT_COLUMNS}`,
  );
  check(
    'the renderer agrees on the row bound',
    goRows === MAX_LAYOUT_ROW,
    `Go ${goRows} vs JS ${MAX_LAYOUT_ROW}`,
  );

  // And the stylesheet, which is where the columns are actually drawn.
  const panel = read('frontend/src/DashboardPanel.svelte');
  const declared = /grid-template-columns:\s*repeat\((\d+),/.exec(panel)?.[1];
  check(
    'the stylesheet draws that many columns',
    Number(declared) === goColumns,
    `CSS repeat(${declared})`,
  );
}

/* --- the placement must stay in the stylesheet ---------------------------- */
{
  // An inline `grid-column` wins over every rule, including the media query
  // that collapses the dashboard to one column — so the narrow layout would
  // silently keep its twelve columns. The panel sets custom properties only.
  const panel = read('frontend/src/DashboardPanel.svelte');
  const markup = panel.slice(panel.indexOf('<div class="widgets"'), panel.indexOf('<style'));
  check(
    'the panel places nothing inline',
    !/style="[^"]*grid-(column|row|area)\s*:/.test(markup),
    'an inline placement would defeat the responsive collapse',
  );
  check(
    'the collapse exists',
    /@media\s*\(max-width:\s*\d+px\)/.test(panel) && /order:\s*var\(--order\)/.test(panel),
    'one column below the breakpoint, in the author\'s sequence',
  );
}

/* --- the conversion ------------------------------------------------------- */
{
  const w = { layout: { x: 0, y: 0, w: 12, h: 2 } };
  check('x: 0 is grid line 1', cellStyle(w).includes('--x:1'), cellStyle(w));
  check('y: 0 is grid line 1', cellStyle(w).includes('--y:1'), cellStyle(w));
  check('a span is a span, not a line', cellStyle(w).includes('--w:12'), cellStyle(w));

  const mid = { layout: { x: 6, y: 3, w: 6 } };
  check('x: 6 is grid line 7', cellStyle(mid).includes('--x:7'), cellStyle(mid));
  check('y: 3 is grid line 4', cellStyle(mid).includes('--y:4'), cellStyle(mid));
  // An omitted height is one row. Reading it as 0 would give `span 0`, which is
  // invalid, and the browser's answer to an invalid span is to drop the whole
  // declaration and auto-place the widget somewhere else entirely.
  check('an omitted height is one row', cellStyle(mid).includes('--h:1'), cellStyle(mid));

  check('a widget that places itself nowhere gets nothing', cellStyle({}) === '', cellStyle({}));
  check('and neither does a missing widget', cellStyle(null) === '', String(cellStyle(null)));
}

/* --- what the collapsed column reads like --------------------------------- */
{
  // Declared in one order, placed in another: the author added the uptime tile
  // last and put it at the top, which is the ordinary way a preset is edited.
  const widgets = [
    { title: 'Traffic', layout: { x: 0, y: 2, w: 12 } },
    { title: 'Out', layout: { x: 6, y: 1, w: 6 } },
    { title: 'In', layout: { x: 0, y: 1, w: 6 } },
    { title: 'Uptime', layout: { x: 0, y: 0, w: 12 } },
  ];
  const order = readingOrder(widgets);
  const sorted = widgets
    .map((w, i) => ({ title: w.title, rank: order[i] }))
    .sort((a, b) => a.rank - b.rank)
    .map((e) => e.title);
  check(
    'the collapsed column reads top to bottom, then left to right',
    sorted.join(' ') === 'Uptime In Out Traffic',
    sorted.join(' '),
  );

  // A widget nobody placed comes after every one that is placed, however far
  // down the grid the placed ones go.
  const mixed = [
    { title: 'unplaced' },
    { title: 'bottom', layout: { x: 0, y: MAX_LAYOUT_ROW, w: 12 } },
  ];
  const ranks = readingOrder(mixed);
  check(
    'an unplaced widget comes last, even below a widget on the last row',
    ranks[0] > ranks[1],
    `${ranks[0]} vs ${ranks[1]}`,
  );
}

/* --- which arrangement applies -------------------------------------------- */
{
  check('a preset that places nothing keeps the old arrangement', !hasLayout([{ title: 'a' }]));
  check('one placed widget is enough', hasLayout([{ title: 'a' }, { layout: { x: 0, y: 0, w: 4 } }]));
  check('and an empty preset places nothing', !hasLayout([]) && !hasLayout(undefined));
}

console.log(failures === 0 ? '\nlayout: all checks passed' : `\nlayout: ${failures} failure(s)`);
process.exit(failures === 0 ? 0 : 1);
