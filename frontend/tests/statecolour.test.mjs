// One rule decides what a reading says and what colour it is.
//
// Two widgets show a state — the wall of cells and the map — and they must
// agree. The same port green on one and grey on the other, on the same
// dashboard, is what two copies of a regular expression produce the first time
// either of them gains a word. series.test.mjs exists for exactly this defect
// on chart colours, where there were four rules and they disagreed whenever
// OIDs had uneven target counts.
import { readFileSync } from 'node:fs';
import { latestFor, stateOf, stateText, stateKind } from '../src/utils/stateColour.js';

const root = new URL('../../', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');
const read = (p) => readFileSync(root + p, 'utf8');

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const at = (ts, value, error) => ({ oid: 'o', timestamp: ts, value, error });

/* --- one rule, and both widgets read it ----------------------------------- */
{
  const users = ['frontend/src/monitor/StatusTile.svelte', 'frontend/src/monitor/MapTile.svelte'];
  for (const path of users) {
    const src = read(path);
    check(
      `${path.split('/').pop()} reads the shared rule`,
      /from '\.\.\/utils\/stateColour'/.test(src),
    );
    // The words themselves, in a component, would be the second copy. The
    // check is for the word list rather than for any regex, because a
    // component legitimately has others.
    check(
      `${path.split('/').pop()} does not keep its own word list`,
      !/dormant|ethernetCsmacd|\bactive\|/.test(src.replace(/from '[^']+'/g, '')),
      'the states are named in one place',
    );
  }
}

/* --- what a reading is ---------------------------------------------------- */
{
  check('the latest point wins, not the last in the array',
    latestFor('o', [at('2026-01-01T00:00:02Z', 2), at('2026-01-01T00:00:01Z', 1)]).value === 2);
  check('a reading for another OID is not this one',
    latestFor('other', [at('2026-01-01T00:00:01Z', 1)]) === null);
  check('no points is null, not undefined', latestFor('o', []) === null);
  check('it survives what the bridge really sends', latestFor('o', undefined) === null);

  check('nothing yet is unknown', stateOf(null).kind === 'unknown');
  check('an error is failed, and says what it was',
    stateOf(at('t', null, 'timeout')).kind === 'failed' && stateOf(at('t', null, 'timeout')).text === 'timeout');
  check('a null value is unknown rather than zero',
    stateOf(at('t', null)).kind === 'unknown');
  // The key is the ROUNDED value: a counter that arrives as 1.0000001 is
  // state 1, and looking it up as "1.0000001" finds nothing.
  check('the state key is a whole number', stateOf(at('t', 1.0000001)).key === '1');
}

/* --- the word decides the colour ------------------------------------------ */
{
  const labels = { 1: 'up', 2: 'down', 3: 'testing', 7: 'lower layer down' };

  check('a named good state is good', stateKind(at('t', 1), labels) === 'good');
  check('a named bad state is bad', stateKind(at('t', 2), labels) === 'bad');
  check('a named uncertain state warns', stateKind(at('t', 3), labels) === 'warn');

  // Not a substring match: "lower layer down" contains "down" and is not the
  // word "down". Matching loosely would colour half a label map by accident.
  check('a phrase that merely contains a word is not that word',
    stateKind(at('t', 7), labels) === 'value', stateKind(at('t', 7), labels));

  // The number cannot mean anything on its own: 1 is up on ifOperStatus and
  // unknown on upsBatteryStatus.
  check('an unnamed number is neither good nor bad',
    stateKind(at('t', 9), labels) === 'value');
  check('and it is shown as itself rather than as a guess',
    stateText(at('t', 9), labels) === '9');
  check('a named state is shown in the author\'s words',
    stateText(at('t', 2), labels) === 'down');

  check('an error is failed whatever the labels say',
    stateKind(at('t', null, 'timeout'), labels) === 'failed');
  check('and no labels at all is not a crash',
    stateKind(at('t', 1), undefined) === 'value' && stateText(at('t', 1), undefined) === '1');
}

console.log(failures === 0 ? '\nstate colours: all checks passed' : `\nstate colours: ${failures} failure(s)`);
process.exit(failures === 0 ? 0 : 1);
