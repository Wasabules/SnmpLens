// One plan decides every colour and every cap.
//
// There were four rules and they disagreed. MonitorChart.buildStacked ran a
// counter over the pairs it actually plotted; buildDatasets indexed targets
// within one OID; MetricTiles computed `oidIdx * targets.length + tIdx`, which
// equals the chart's counter only when every OID has the SAME number of
// targets; and ChannelsModal ran a fourth counter over a target list built by a
// different rule — it falls back to the session's configured targets for an OID
// with no data yet, where the chart skipped that OID entirely.
//
// So a swatch in the channel picker, a swatch on a tile and the line on the
// chart could each be a different colour for the same series. Both triggers are
// ordinary: an uneven number of targets per OID, or the moments before the
// first sample of one OID arrives.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { seriesPlan, seriesKey, MAX_SERIES, seriesColor } from '../src/utils/chartPalette.js';

const root = new URL('../src/', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const point = (target, oid) => ({ target, oid, timestamp: '2026-01-01T00:00:00Z', value: 1 });

/* --- the case the old formulas disagreed on ------------------------------- */
{
  // OID A on three targets, OID B on five: `oidIdx * targets.length + tIdx`
  // gives B's first series index 1*5+0 = 5, while a running counter gives 3.
  const session = {
    oid: 'A',
    oids: ['A', 'B'],
    targets: ['t1', 't2', 't3', 't4', 't5'],
    results: [
      point('t1', 'A'), point('t2', 'A'), point('t3', 'A'),
      point('t1', 'B'), point('t2', 'B'), point('t3', 'B'), point('t4', 'B'), point('t5', 'B'),
    ],
  };

  const plan = seriesPlan(session, { layout: 'stacked' });
  check('every (OID, target) pair is planned once', plan.series.length === 8, String(plan.series.length));

  const idx = (target, oid) => plan.series.find((s) => s.key === seriesKey(target, oid))?.colorIndex;
  check('the stacked counter runs across OIDs', idx('t1', 'B') === 3,
    `B/t1 = ${idx('t1', 'B')}, want 3 (after A's three)`);
  check('and it is not the old formula', idx('t1', 'B') !== 1 * 5 + 0);

  // Past the eighth slot the palette cannot keep colours distinct, so the plan
  // says so instead of silently reusing a hue.
  check('what the palette cannot take is reported', plan.dropped === 0, String(plan.dropped));
  const big = seriesPlan({ oids: ['A'], targets: [], results: Array.from({ length: 11 }, (_, i) => point('t' + i, 'A')) },
    { layout: 'stacked' });
  check('an over-long session reports how many were dropped',
    big.dropped === 11 - MAX_SERIES, `${big.dropped} dropped of ${big.series.length}`);
  check('and marks exactly those', big.series.filter((s) => s.capped).length === big.dropped);

  // Separate mode indexes within the OID, so two small multiples both start at
  // the first colour — which is what makes them readable side by side.
  const sep = seriesPlan(session, { layout: 'separate' });
  const sidx = (target, oid) => sep.series.find((s) => s.key === seriesKey(target, oid))?.colorIndex;
  check('separate mode restarts the colours per OID', sidx('t1', 'A') === 0 && sidx('t1', 'B') === 0);
  check('and keeps the target order inside one OID', sidx('t3', 'B') === 2);
}

/* --- the OID with no data yet --------------------------------------------- */
{
  // The chart used to skip an OID with no data entirely while the channel
  // picker reserved colours for its configured targets, so every series after
  // it was a colour out.
  const session = {
    oid: 'A', oids: ['A', 'B'], targets: ['t1', 't2'],
    results: [point('t1', 'A'), point('t2', 'A')], // nothing for B yet
  };
  const plan = seriesPlan(session, { layout: 'stacked' });

  check('an OID with no data still has its series planned', plan.series.length === 4,
    plan.series.map((s) => s.key).join(', '));
  check('and it uses the configured targets in order',
    plan.series[2].key === seriesKey('t1', 'B') && plan.series[3].key === seriesKey('t2', 'B'));
  check('so the colours after it do not shift when its data arrives',
    plan.series[2].colorIndex === 2);

  // The proof: once B's data arrives, the plan is identical.
  const later = seriesPlan({ ...session, results: [...session.results, point('t1', 'B'), point('t2', 'B')] },
    { layout: 'stacked' });
  check('the plan is the same before and after the first sample of an OID',
    JSON.stringify(plan.series) === JSON.stringify(later.series));
}

/* --- colour is positional -------------------------------------------------- */
{
  const session = { oid: 'A', oids: ['A'], targets: [], results: [point('t1', 'A'), point('t2', 'A'), point('t3', 'A')] };
  const plan = seriesPlan(session, { layout: 'separate' });
  const before = plan.series.map((s) => s.colorIndex);

  // Hiding is a rendering decision and the plan knows nothing about it, which
  // is exactly what makes hiding one curve unable to repaint the others.
  const again = seriesPlan(session, { layout: 'separate' });
  check('the plan does not depend on what is drawn',
    JSON.stringify(again.series.map((s) => s.colorIndex)) === JSON.stringify(before));
  check('and colours never cycle', seriesColor(0, true) !== seriesColor(1, true));
}

/* --- and nothing keeps its own copy of the rule ---------------------------- */
{
  const chart = readFileSync(join(root, 'monitor', 'MonitorChart.svelte'), 'utf8');
  const tiles = readFileSync(join(root, 'monitor', 'MetricTiles.svelte'), 'utf8');
  const modal = readFileSync(join(root, 'monitor', 'ChannelsModal.svelte'), 'utf8');

  // Comments are stripped first: this file explains the defects it replaced, so
  // a check that scanned the prose would fail on its own description of them.
  // `[^\r\n]` rather than `.` and `$`: the working tree is CRLF on Windows and
  // LF in git, so splitting on \n leaves a \r that `.` will not cross and `$`
  // sits after — the strip silently did nothing on one platform and everything
  // on the other, which is a check that passes in CI and fails on the machine
  // that wrote it.
  const code = (src) => src.replace(/\/\/[^\r\n]*/g, '');

  for (const [name, src] of [['MonitorChart', chart], ['MetricTiles', tiles], ['ChannelsModal', modal]]) {
    check(`${name} takes its colours from the plan`, /seriesPlan\(/.test(code(src)));
  }
  check('the old tile formula is gone', !/oidIdx\s*\*\s*targets\.length/.test(code(tiles)));
  check('the stacked notice is no longer hard-wired to zero',
    !/(?<!let )hiddenSeriesNotice\s*=\s*0;/.test(code(chart)),
    'buildStacked set it to 0 unconditionally, so the footnote could never appear');

  // Svelte 5 tracks what the EXPRESSION reads, so a dependency the statement
  // does not name is a dependency that never fires it. hiddenSet was missing:
  // toggling a channel updated the tiles and left the chart alone.
  const refresh = /\$: if \(chart && \(([^)]*)\)\) refresh\(\);/.exec(chart);
  check('the chart refreshes when a channel is toggled',
    !!refresh && /hiddenSet/.test(refresh[1]), refresh ? refresh[1] : 'statement not found');
}

/* --- prove the checks can fail -------------------------------------------- */
check('the detector: the old formula would be caught',
  /oidIdx\s*\*\s*targets\.length/.test('const colorIdx = oidIdx * targets.length + tIdx;'));

process.exit(failures ? 1 : 0);
