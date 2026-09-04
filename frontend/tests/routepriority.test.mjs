// Route priority, as the settings page normalises it before saving.
//
// `Number(editingRoute.priority) || 100` turns a typed 0 into 100. Routes are
// evaluated in ASCENDING priority, so 0 is the natural way to say "evaluate this
// first" — and combined with `stop` it decides which destinations an event
// reaches at all. The highest-priority rule an operator can express was silently
// demoted to the default, and the list re-rendered showing 100 with no message.
//
// Measured against the real saveRoute before the fix: typed 0 -> stored 100,
// 1 -> 1, 50 -> 50, empty -> 100, "abc" -> 100.
import { readFileSync } from 'node:fs';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const source = readFileSync(
  new URL('../src/settings/NotifySettings.svelte', import.meta.url), 'utf8');

// The panel cannot be mounted without a DOM, so the normalisation is lifted out
// of the source verbatim and exercised. Lifting it means the test breaks if the
// expression changes shape, which is the point: this is the line that was wrong,
// and a test carrying its own copy would have gone on passing.
const start = source.indexOf('const raw = editingRoute.priority;');
const end = source.indexOf('await NotifySaveRoute(', start);
check('the priority normalisation is where the test expects it',
  start >= 0 && end > start,
  start < 0 ? 'saveRoute no longer normalises priority here — update this test' : '');

if (start < 0 || end <= start) process.exit(1);

const lifted = source.slice(start, end);
const normalise = new Function('editingRoute', lifted + '\nreturn editingRoute.priority;');

const cases = [
  [0, 0, 'zero is the highest priority an operator can express'],
  ['0', 0, 'and typing it in a text input gives a string'],
  [1, 1, ''],
  [50, 50, ''],
  [100, 100, ''],
  ['', 100, 'an empty field falls back to the default'],
  ['   ', 100, 'and so does whitespace'],
  [null, 100, 'a cleared value is not priority zero'],
  [undefined, 100, ''],
  ['abc', 100, 'a value that is not a number falls back'],
  [NaN, 100, ''],
  [-1, -1, 'a negative is a deliberate "before everything"'],
];

for (const [input, want, why] of cases) {
  const got = normalise({ priority: input });
  check(`priority ${JSON.stringify(input) ?? String(input)} -> ${want}${why ? ' (' + why + ')' : ''}`,
    got === want, `got ${JSON.stringify(got)}`);
}

// And prove the old expression fails these, or the test is not testing the bug.
{
  const old = (v) => Number(v) || 100;
  check('the detector: the old expression demotes 0', old(0) === 100);
  check('the detector: and it agreed on everything else', old(50) === 50 && old('') === 100);
}

process.exit(failures ? 1 : 0);
