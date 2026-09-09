// Several equipments on one dashboard.
//
// The rule worth testing without a browser is which sessions may be MERGED.
// Binding is one session per target and stays that way; this is a view over
// several of them, and getting the grouping wrong shows one equipment's widgets
// with another's readings inside them — which looks entirely plausible.
import {
  groupKey,
  dashboardGroups,
  mergeGroup,
  dashboardView,
  statusBlocks,
} from '../src/utils/dashboardGroup.js';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const point = (target, oid, value, ts = '2026-01-01T00:00:00Z') => ({
  target, oid, value, timestamp: ts,
});

// Two switches bound from one preset. Different port counts on purpose: that is
// what discovery is for, and it is the case a naive merge gets wrong.
const switchSession = (id, target, ports, extra = {}) => ({
  id,
  name: 'Switch ports',
  targets: [target],
  oids: ['1.3.6.1.2.1.1.3.0', ...ports.map((p) => `1.3.6.1.2.1.2.2.1.8.${p}`)],
  results: [
    point(target, '1.3.6.1.2.1.1.3.0', 1000),
    ...ports.map((p) => point(target, `1.3.6.1.2.1.2.2.1.8.${p}`, 1)),
  ],
  thresholds: {},
  running: true,
  preset: {
    file: 'switch-ports.json',
    name: 'Switch ports',
    widgets: [
      { kind: 'value', title: 'Uptime', oids: ['1.3.6.1.2.1.1.3.0'] },
      {
        kind: 'grid',
        title: 'Ports',
        oids: ports.map((p) => `1.3.6.1.2.1.2.2.1.8.${p}`),
        labels: { 1: 'up', 2: 'down' },
        oidLabels: Object.fromEntries(
          ports.map((p) => [`1.3.6.1.2.1.2.2.1.8.${p}`, `Gi0/${p}`]),
        ),
      },
    ],
    ...extra,
  },
});

/* --- what may be grouped -------------------------------------------------- */
{
  const a = switchSession('a', '10.0.0.1', [1, 2, 3]);
  const b = switchSession('b', '10.0.0.2', [1, 2, 3, 4, 5, 6]);

  check(
    'two equipments bound from one preset are one dashboard',
    groupKey(a) === groupKey(b),
  );

  // The whole point of discovery: a 24-port switch and a 48-port chassis are
  // the same dashboard. Keying on the OIDs would split them.
  check(
    'a different port count does not split them',
    dashboardGroups([a, b]).length === 1,
    `${a.preset.widgets[1].oids.length} vs ${b.preset.widgets[1].oids.length} ports`,
  );

  // The snapshot rule biting: a session carries the preset AS IT WAS WHEN IT
  // WAS BOUND, so a file edited between two binds leaves two sessions naming
  // one file and drawing different things.
  const renamed = switchSession('c', '10.0.0.3', [1, 2]);
  renamed.preset.widgets[1].title = 'Access ports';
  check(
    'a preset edited between two binds is not merged with the old one',
    groupKey(renamed) !== groupKey(a),
  );
  const banded = switchSession('d', '10.0.0.4', [1, 2]);
  banded.preset.widgets[1].threshold = { max: 1 };
  check(
    'and neither is one whose band changed',
    groupKey(banded) !== groupKey(a),
    'a band decides what the widget MEANS',
  );

  check('a session with no snapshot has no group', groupKey({ id: 'x' }) === null);
  check('nor has one whose snapshot is empty', groupKey({ preset: { widgets: [] } }) === null);

  // One member is not a group: an "All 1" entry beside the session it contains
  // is two names for one thing.
  check('a lone session is not offered as a group', dashboardGroups([a]).length === 0);
  check('it survives what the bridge really sends', dashboardGroups(null).length === 0);
}

/* --- the merge ------------------------------------------------------------ */
{
  const a = switchSession('a', '10.0.0.1', [1, 2, 3]);
  const b = switchSession('b', '10.0.0.2', [1, 2, 3, 4, 5, 6]);
  const merged = mergeGroup([a, b]);

  check('the merged view holds both equipments',
    merged.targets.join(',') === '10.0.0.1,10.0.0.2', merged.targets.join(','));
  check('and every reading from both',
    merged.results.length === a.results.length + b.results.length,
    String(merged.results.length));

  // The union, not the first member's list: the port wall of a group has to
  // hold every port either equipment has, and one they share appears once.
  const ports = merged.preset.widgets[1].oids;
  check('the port wall is the union of both', ports.length === 6, ports.join(' '));
  check('a port they share is not doubled',
    new Set(ports).size === ports.length);
  check('the discovered names came with it',
    merged.preset.widgets[1].oidLabels['1.3.6.1.2.1.2.2.1.8.6'] === 'Gi0/6');

  // The vocabulary is the first member's, and it must be untouched: the merge
  // changes what is DRAWN, never what the widgets are.
  check('the widget vocabulary is unchanged',
    merged.preset.widgets[1].kind === 'grid' && merged.preset.widgets[1].title === 'Ports');

  check('a group is running when any member is',
    mergeGroup([{ ...a, running: false }, b]).running === true);
  check('one member merges to itself',
    mergeGroup([a]) === a, 'no copy, no synthetic id');
  check('nothing merges to null', mergeGroup([]) === null);
}

/* --- what the dashboard draws --------------------------------------------- */
{
  const a = switchSession('a', '10.0.0.1', [1, 2]);
  const b = switchSession('b', '10.0.0.2', [1, 2, 3]);
  const sessions = [a, b];
  const group = dashboardGroups(sessions)[0];

  const asGroup = dashboardView(sessions, group.id, a);
  check('choosing the group draws all of it',
    asGroup.members.length === 2 && asGroup.session.groupOf === 2);

  const asOne = dashboardView(sessions, 'b', a);
  check('choosing one equipment draws that one',
    asOne.session === b && asOne.members.length === 1);

  // A selection that no longer exists — the session was deleted, or the group
  // lost a member while the tab was on another panel — must fall back rather
  // than render nothing.
  check('a stale selection falls back',
    dashboardView(sessions, 'deleted', a).session === a);
  check('a stale GROUP selection falls back too',
    dashboardView([a], group.id, a).session === a,
    'the group is gone once it has one member');
  check('with nothing to fall back to, nothing is drawn',
    dashboardView([], null, null).session === null);
}

/* --- the wall of states, split by equipment ------------------------------- */
{
  // The failure this exists for: a cell is one OID, and every switch bound from
  // one preset answers the same OIDs. Keyed by OID alone the wall shows
  // whichever equipment answered most recently, in one cell, and nothing says
  // so — a wrong reading that looks exactly like a right one.
  const oids = ['o.1', 'o.2', 'o.3'];
  const points = [
    point('10.0.0.1', 'o.1', 1), point('10.0.0.1', 'o.2', 2),
    point('10.0.0.2', 'o.1', 1), point('10.0.0.2', 'o.2', 1), point('10.0.0.2', 'o.3', 1),
  ];

  const one = statusBlocks(points, ['10.0.0.1'], oids);
  check('one equipment is one unnamed block',
    one.length === 1 && one[0].target === '' && one[0].oids.length === 3);

  const two = statusBlocks(points, ['10.0.0.1', '10.0.0.2'], oids);
  check('two equipments are two named blocks',
    two.length === 2 && two[0].target === '10.0.0.1' && two[1].target === '10.0.0.2');
  check('each block holds only its own readings',
    two[0].points.every((p) => p.target === '10.0.0.1') &&
    two[1].points.every((p) => p.target === '10.0.0.2'));

  // The union gave three OIDs; the first switch has two ports. Showing it a
  // third empty cell would invent a port it does not have.
  check('an equipment shows only the ports it answered',
    two[0].oids.join(' ') === 'o.1 o.2', two[0].oids.join(' '));
  check('and the other shows all of its own', two[1].oids.length === 3);

  // Before the first round there is nothing to filter by, and an empty block
  // would make the wall appear a cell at a time as the answers arrived.
  const waiting = statusBlocks([], ['10.0.0.1', '10.0.0.2'], oids);
  check('an equipment that has not answered yet keeps the shape of the wall',
    waiting[0].oids.length === 3 && waiting[1].oids.length === 3);

  // A device that is down still has points — they carry an error — so it keeps
  // its cells, and they read as failures rather than vanishing from the wall.
  const down = statusBlocks(
    [...points, { target: '10.0.0.3', oid: 'o.1', value: null, error: 'timeout', timestamp: 'z' }],
    ['10.0.0.1', '10.0.0.3'], oids);
  check('a device that is down keeps its cells',
    down[1].oids.join(' ') === 'o.1' && down[1].points[0].error === 'timeout');

  check('it survives what the bridge really sends',
    statusBlocks(null, null, null).length === 1);
}

/* --- a block is scoped to its own widget ---------------------------------- */
{
  // Found in a capture, not here: `points` is the WHOLE session's results, so
  // an equipment that has answered the uptime scalar and none of the ports had
  // points — "has it answered anything" was true, its wall was filtered down to
  // the ports it had answered, which was none, and the card read "Nothing to
  // show." beside a full wall for the other switch.
  const ports = ['p.1', 'p.2', 'p.3'];
  const points = [
    point('10.0.0.1', 'p.1', 1), point('10.0.0.1', 'p.2', 1), point('10.0.0.1', 'p.3', 1),
    // The second switch answered something — just nothing this widget draws.
    point('10.0.0.2', 'uptime', 4200),
  ];
  const blocks = statusBlocks(points, ['10.0.0.1', '10.0.0.2'], ports);

  check('an equipment that has answered nothing of THIS widget keeps the wall',
    blocks[1].oids.length === 3, blocks[1].oids.join(' ') || '(empty)');
  check('and none of another widget\'s readings leak into it',
    blocks[1].points.length === 0);
  check('the one that did answer is unaffected',
    blocks[0].oids.length === 3 && blocks[0].points.length === 3);
}

console.log(failures === 0 ? '\ndashboard groups: all checks passed' : `\ndashboard groups: ${failures} failure(s)`);
process.exit(failures === 0 ? 0 : 1);
