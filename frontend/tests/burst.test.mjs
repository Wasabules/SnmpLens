// Nothing a TRAP can trigger in the renderer may run at the rate the network
// chooses.
//
// The Go side is built to absorb a storm — the socket buffer was raised to
// 8 MiB and 2000 events a second are journalled without loss — and every one of
// them is emitted to the window. The renderer had no limit of any kind: one
// toast per trap, one OS notification per trap, and one bridge call per trap
// into GetOidDetails, which takes pkg/mib's package-level EXCLUSIVE gosmi
// mutex. So an unauthenticated remote sender chose how often the renderer
// acquired the lock every MIB operation in the product needs, and the window
// whose job is to show the flood was what stopped working.
//
// The unit tests are the smaller half of this file. The structural check is
// what stops it recurring: nothing fed by 'event:new' or 'newTrap' may append
// to a list without a ceiling.
import { readdirSync, readFileSync } from 'node:fs';
import { join, basename } from 'node:path';
import { createBurstGate, capNewestFirst } from '../src/utils/burst.js';

const root = new URL('../src/', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

function sources(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...sources(path));
    else if (entry.name.endsWith('.svelte') || entry.name.endsWith('.js')) out.push(path);
  }
  return out;
}

/* --- the gate --------------------------------------------------------------
 * Time is driven rather than waited for, so the burst under test is the shape
 * a real storm has: two thousand events inside one window. */
{
  let clock = 0;
  const summaries = [];
  let armed = null;
  const gate = createBurstGate({
    windowMs: 1000,
    onSummary: (n) => summaries.push(n),
    now: () => clock,
    schedule: (fn) => { armed = fn; return 1; },
    cancel: () => { armed = null; },
  });

  check('the first event goes through', gate.offer() === true);
  let admitted = 0;
  for (let i = 0; i < 2000; i++) if (gate.offer()) admitted++;
  check('everything else inside the window is suppressed', admitted === 0, `${admitted} admitted`);
  check('and it is counted, not discarded', gate.pending() === 2000, String(gate.pending()));

  // The window closes and the tail is reported. Without the trailing summary
  // the last events of a burst are simply never mentioned.
  clock = 1000;
  armed();
  check('the tail of the burst is reported once', summaries.length === 1 && summaries[0] === 2000,
    JSON.stringify(summaries));

  check('the next event leads a new window', gate.offer() === true);
  check('a window with nothing suppressed reports nothing', (() => {
    clock = 2000;
    armed();
    return summaries.length === 1;
  })(), JSON.stringify(summaries));

  // Two thousand traps a second, for ten seconds: at most one report per
  // window, which is what makes the toast list bounded no matter the rate.
  clock = 3000;
  const g2 = createBurstGate({
    windowMs: 1000, onSummary: () => {}, now: () => clock,
    schedule: () => 1, cancel: () => {},
  });
  let through = 0;
  for (let sec = 0; sec < 10; sec++) {
    clock = 3000 + sec * 1000;
    for (let i = 0; i < 2000; i++) if (g2.offer()) through++;
  }
  check('20 000 traps over ten windows produce at most ten reports', through === 10, String(through));
}

/* --- the cap --------------------------------------------------------------- */
{
  const list = Array.from({ length: 10 }, (_, i) => i);
  check('a short list is returned unchanged', capNewestFirst(list, 20) === list);
  check('a long one keeps the newest', JSON.stringify(capNewestFirst(list, 3)) === '[0,1,2]',
    JSON.stringify(capNewestFirst(list, 3)));
  check('exactly at the limit is not a copy', capNewestFirst(list, 10) === list);
}

/* --- and what the codebase must do with it --------------------------------
 * Every list fed by a Wails event a trap can raise needs a ceiling. Checked
 * per FILE, which is coarse — but the failure it catches is a file that
 * subscribes and never caps, which is what all four of them did. */
{
  const files = sources(root);
  const LIVE = /EventsOn\(\s*['"](newTrap|event:new)['"]/;
  const CAPPED = /capNewestFirst|MAX_LIVE_ITEMS|MAX_INCIDENTS|maxCount|MAX_TRAPS/;

  const uncapped = [];
  for (const file of files) {
    const src = readFileSync(file, 'utf8');
    if (!LIVE.test(src)) continue;
    if (!CAPPED.test(src)) uncapped.push(basename(file));
  }
  check('every list fed by a live trap event has a ceiling', uncapped.length === 0, uncapped.join(', '));

  // The toast list is the one that had no ceiling at all, and it is not fed by
  // an EventsOn of its own — it is fed by everything else.
  const toasts = readFileSync(join(root, 'stores', 'notifications.js'), 'utf8');
  check('the toast list has a ceiling', /MAX_TOASTS/.test(toasts));

  // The OID lookup on the trap path is behind the gate. It is the expensive
  // one: GetOidDetails takes pkg/mib's exclusive gosmi mutex.
  const trapStore = readFileSync(join(root, 'stores', 'trapStore.js'), 'utf8');
  // lastIndexOf, not indexOf: GetOidDetails also appears in the import at the
  // top of the file, and comparing against that position made this check pass
  // on nothing.
  const gateAt = trapStore.indexOf('nativeTrapGate.offer()');
  const lookupAt = trapStore.lastIndexOf('GetOidDetails(');
  check('the per-trap OID lookup is behind a gate',
    gateAt > 0 && lookupAt > 0 && gateAt < lookupAt, `gate@${gateAt} lookup@${lookupAt}`);

  /* --- prove the checks can fail ----------------------------------------- */
  const wouldFail = `EventsOn('event:new', (ev) => { items = [ev, ...items]; });`;
  check('the detector: an uncapped live list is visible',
    LIVE.test(wouldFail) && !CAPPED.test(wouldFail));
  check('the detector: a capped one is not',
    CAPPED.test(`EventsOn('newTrap', (t) => { list = capNewestFirst([t, ...list], 100); });`));
}

process.exit(failures ? 1 : 0);
