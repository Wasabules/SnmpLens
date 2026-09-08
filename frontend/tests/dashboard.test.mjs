// What the dashboard shows, and the one failure it cannot see.
//
// The selection rules are pure and testable; the widget dispatch is not, so it
// is checked against the Go source instead. That second check is the load-
// bearing one: a widget kind added to pkg/preset that the panel does not name
// renders as NOTHING AT ALL — no error, no empty box, no console message — and
// the preset that uses it simply looks like it lost a panel.
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { dashboardSession, dashboardCandidates } from '../src/stores/dashboardStore.js';

const root = new URL('../../', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const bound = (id, widgets = [{ kind: 'value', title: 'x', oids: ['1.1'] }]) => ({
  id, targets: [`10.0.0.${id}`], oids: ['1.1'], interval: 30000, results: [],
  preset: { file: `${id}.json`, formatVersion: 1, widgets },
});
const handmade = (id) => ({ id, targets: ['10.0.0.9'], oids: ['1.1'], results: [], preset: null });

/* --- which sessions a dashboard can draw ---------------------------------- */
{
  const sessions = [handmade('h1'), bound('1'), handmade('h2'), bound('2')];
  const candidates = dashboardCandidates(sessions);

  check('only sessions bound from a preset are candidates',
    candidates.length === 2 && candidates.every((s) => !!s.preset),
    candidates.map((s) => s.id).join(', '));

  // A session whose snapshot could not be decoded polls fine and cannot be
  // drawn: Go leaves Preset nil, and a candidate with no widgets would render
  // an empty page with nothing saying why.
  check('a session with a snapshot but no widgets is not a candidate',
    dashboardCandidates([bound('3', [])]).length === 0);
  check('a session whose snapshot is not a list is not a candidate',
    dashboardCandidates([{ id: 'x', preset: { widgets: 'nope' } }]).length === 0);
}

/* --- which one is shown --------------------------------------------------- */
{
  const sessions = [handmade('h1'), bound('1'), bound('2')];

  check('the chosen session wins', dashboardSession(sessions, '2')?.id === '2');

  // The fallback is a BOUND session, never simply the first one: a hand-built
  // session has no layout, and landing on it would show an empty dashboard on
  // a machine that has several perfectly good ones.
  check('with no choice, the first bound session is shown',
    dashboardSession(sessions, null)?.id === '1', dashboardSession(sessions, null)?.id);

  // A selection that no longer exists — the session was deleted while the tab
  // was on another panel — must fall back rather than render nothing.
  check('a stale selection falls back instead of blanking the page',
    dashboardSession(sessions, 'deleted-session')?.id === '1');

  check('nothing to draw is null, not undefined',
    dashboardSession([handmade('h1')], null) === null);
  check('it survives what the bridge really sends',
    dashboardSession(undefined, null) === null && dashboardCandidates(null).length === 0);
}

/* --- every kind Go serves is drawn ---------------------------------------- */
{
  const presetDir = join(root, 'pkg', 'preset');
  const goSource = readdirSync(presetDir)
    .filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'))
    .map((f) => readFileSync(join(presetDir, f), 'utf8'))
    .join('\n');

  const kinds = [...goSource.matchAll(/Kind[A-Z][a-zA-Z]*\s*=\s*"([a-z]+)"/g)].map((m) => m[1]);
  check('the Go source yields widget kinds', kinds.length >= 4, kinds.join(', '));

  const panel = readFileSync(join(root, 'frontend', 'src', 'DashboardPanel.svelte'), 'utf8');
  for (const kind of kinds) {
    check(`the dashboard draws a "${kind}" widget`, panel.includes(`'${kind}'`),
      panel.includes(`'${kind}'`) ? '' : 'the dispatch does not name it, so it renders as nothing');
  }

  // And an unknown kind is SAID rather than skipped: a preset written for a
  // newer release is a thing that will happen, and a silently missing panel is
  // indistinguishable from a broken one.
  check('a kind this version does not know is reported',
    /dashboard\.unknownWidget/.test(panel));

  // The unit comes from the preset, not from the OID name. inferUnit matches
  // on names and a preset carries numeric OIDs only, so ifInOctets would render
  // as "1.2 G" where the monitor tab renders "9.8 Gbit/s".
  check('the panel carries the preset\'s own unit', /widget\.unit/.test(panel));
}

/* --- prove the checks can fail -------------------------------------------- */
check('the detector: an unnamed kind is caught', !'{#if widget.kind === }'.includes("'nosuchkind'"));

process.exit(failures ? 1 : 0);
