// The dependency graph, read as a tree.
//
// Go answers with a flat list of modules and their edges — one row per module
// however tangled the graph is. Turning that into "start at the file you opened
// and walk down through what it needs" is arithmetic, and it has two ways to go
// wrong that are not obvious: a cycle recurses forever, and a module everything
// imports redraws its whole subtree once per importer.
import {
  rootsOf, treeFrom, flattenTree, graphSummary, statusOf,
  MISSING, NOT_LOADED, LOADED,
} from '../src/utils/mibGraph.js';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const node = (module, imports = [], extra = {}) => ({
  module, file: module, present: true, loaded: true, imports, importedBy: [], ...extra,
});

/** Fill importedBy the way Go does, so the fixtures are the real shape. */
function link(nodes) {
  const by = new Map(nodes.map((n) => [n.module, n]));
  for (const n of nodes) {
    for (const dep of n.imports) {
      const d = by.get(dep);
      if (d) d.importedBy.push(n.module);
    }
  }
  return nodes;
}

const CORPUS = link([
  node('ACME-POE-MIB', ['ACME-SMI', 'SNMPv2-TC', 'IF-MIB']),
  node('ACME-PSU-MIB', ['ACME-SMI', 'SNMPv2-TC']),
  node('ACME-SMI', ['SNMPv2-SMI']),
  node('IF-MIB', ['SNMPv2-SMI', 'SNMPv2-TC']),
  node('SNMPv2-SMI', []),
  node('SNMPv2-TC', ['SNMPv2-SMI']),
]);

/* --- where a tree starts --------------------------------------------------- */
{
  const roots = rootsOf(CORPUS).map((n) => n.module).sort();
  check('the roots are the files somebody actually opened',
    JSON.stringify(roots) === JSON.stringify(['ACME-POE-MIB', 'ACME-PSU-MIB']), roots.join(', '));

  // SNMPv2-SMI is imported by everything and depends on nothing: it is the
  // bottom of the forest, never the top.
  check('a module everything imports is not a root', !roots.includes('SNMPv2-SMI'));

  // A graph where everything is imported by something has no root at all, and
  // an empty screen is the wrong answer.
  const cyclic = link([node('A-MIB', ['B-MIB']), node('B-MIB', ['A-MIB'])]);
  check('a graph with no root falls back to every present module',
    rootsOf(cyclic).length === 2, String(rootsOf(cyclic).length));

  check('it survives what the bridge really sends',
    rootsOf(undefined).length === 0 && rootsOf(null).length === 0);
}

/* --- the tree -------------------------------------------------------------- */
{
  const tree = treeFrom(CORPUS, 'ACME-POE-MIB');
  const flat = flattenTree(tree);
  const at = (m) => flat.find((x) => x.module === m);

  check('the root is the module asked for', tree.module === 'ACME-POE-MIB' && tree.depth === 0);
  check('its imports are its children',
    tree.children.map((c) => c.module).join(',') === 'ACME-SMI,SNMPv2-TC,IF-MIB',
    tree.children.map((c) => c.module).join(','));
  check('and theirs are one level deeper',
    at('SNMPv2-SMI')?.depth === 2, String(at('SNMPv2-SMI')?.depth));

  // Twelve files importing SNMPv2-SMI would otherwise redraw its subtree twelve
  // times: the same information at twelve times the height.
  const smiRows = flat.filter((x) => x.module === 'SNMPv2-SMI');
  check('a module already expanded appears again as a leaf', smiRows.length > 1,
    `${smiRows.length} rows`);
  check('and the repeats are marked rather than silently truncated',
    smiRows.slice(1).every((r) => r.repeated === true));
  check('the first one is not marked', smiRows[0].repeated === false);
}

/* --- a cycle must render, not hang ----------------------------------------- */
{
  // gosmi cannot survive an import cycle — it recurses until the stack ends —
  // so a directory containing one is a real thing this has to draw.
  const cyclic = link([
    node('A-MIB', ['B-MIB']),
    node('B-MIB', ['C-MIB']),
    node('C-MIB', ['A-MIB']),
  ]);

  let flat = [];
  let threw = null;
  try {
    flat = flattenTree(treeFrom(cyclic, 'A-MIB'));
  } catch (e) {
    threw = e.message;
  }
  check('a cycle does not recurse forever', threw === null, threw || '');
  check('and it is marked where it closes',
    flat.some((x) => x.module === 'A-MIB' && x.cycle === true),
    flat.map((x) => `${x.module}${x.cycle ? '*' : ''}`).join(' > '));
  check('the cycle is not descended into', flat.length === 4, String(flat.length));
}

/* --- what a module IS ------------------------------------------------------ */
{
  check('a module with no file is missing',
    statusOf({ present: false }) === MISSING);
  // Present and not loaded usually means switched off, which needs a checkbox
  // rather than a download — the same distinction the missing-import roll makes.
  check('present and not loaded is not the same as missing',
    statusOf({ present: true, loaded: false }) === NOT_LOADED);
  check('present and loaded is loaded',
    statusOf({ present: true, loaded: true }) === LOADED);
  check('an absent node is missing rather than a crash', statusOf(undefined) === MISSING);

  const withHole = link([
    node('ACME-POE-MIB', ['ACME-SMI']),
    { module: 'ACME-SMI', present: false, imports: [], importedBy: [] },
  ]);
  const tree = treeFrom(withHole, 'ACME-POE-MIB');
  check('a module nobody has is still a branch of the tree',
    tree.children[0].module === 'ACME-SMI' && tree.children[0].status === MISSING);
}

/* --- the summary ----------------------------------------------------------- */
{
  const s = graphSummary(link([
    node('A-MIB', ['B-MIB', 'C-MIB']),
    node('B-MIB', [], { loaded: false }),
    { module: 'C-MIB', present: false, imports: [], importedBy: [] },
  ]));
  check('the summary counts what is missing', s.missing === 1, JSON.stringify(s));
  check('and what is present but switched off', s.notLoaded === 1, JSON.stringify(s));
  check('and how many trees there are', s.roots === 1, JSON.stringify(s));
  check('an empty graph summarises to zeroes',
    graphSummary([]).modules === 0 && graphSummary(null).roots === 0);
}

/* --- prove the checks can fail --------------------------------------------- */
check('the detector: a root really is a module nothing imports',
  rootsOf(link([node('X-MIB', ['Y-MIB']), node('Y-MIB', [])])).map((n) => n.module).join() === 'X-MIB');

process.exit(failures ? 1 : 0);
