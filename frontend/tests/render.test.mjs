// The editor's coloured mirror.
//
// This threw whenever the buffer became shorter than the line the view was
// scrolled to — a revert, a restore, an undo of a large paste, opening a
// smaller file while scrolled down. Because it runs inside a `$:` statement,
// the throw landed in Svelte's flush and stopped every reactive statement in
// the APPLICATION: the whole window went dead, not just the editor.
import { renderWindow } from '../src/mibeditor/render.js';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const lines = (text) => text.split('\n');

// The exact shape the panel computes, so the test drives what it drives.
const LINE_HEIGHT = 19;
const VIEW_MARGIN = 40;
const window_ = (scrollTop, count, viewportHeight = 600) => ({
  first: Math.max(0, Math.floor(scrollTop / LINE_HEIGHT) - VIEW_MARGIN),
  last: Math.min(count, Math.ceil((scrollTop + viewportHeight) / LINE_HEIGHT) + VIEW_MARGIN),
});

// --- the crash ---
{
  const short = 'one\ntwo\nthree';
  const l = lines(short);
  // Scrolled to line 3000 of a big file, and the buffer is now three lines.
  const { first, last } = window_(3000 * LINE_HEIGHT, l.length);
  let threw = null;
  let out = null;
  try {
    out = renderWindow(short, l, first, last);
  } catch (e) {
    threw = e;
  }
  check('a buffer shorter than the scroll position does not throw',
    threw === null, threw && `${threw.constructor.name}: ${threw.message}`);
  check('it still returns a string', typeof out === 'string');
}

// Every out-of-range combination a stale scrollTop can produce.
for (const [first, last] of [[999, 3], [3, 999], [999, 999], [-5, 2], [0, -1], [2, 1]]) {
  let threw = null;
  try {
    renderWindow('a\nb\nc', ['a', 'b', 'c'], first, last);
  } catch (e) {
    threw = e;
  }
  check(`first=${first} last=${last} does not throw`, threw === null,
    threw && threw.message);
}

// --- the height invariant, which scroll sync depends on ---
{
  const text = Array.from({ length: 500 }, (_, i) => `line ${i}`).join('\n');
  const l = lines(text);
  for (const scrollTop of [0, 100 * LINE_HEIGHT, 480 * LINE_HEIGHT]) {
    const { first, last } = window_(scrollTop, l.length);
    const out = renderWindow(text, l, first, last);
    const rendered = out.split('\n').length;
    check(`the mirror keeps the line count at scrollTop=${scrollTop}`,
      rendered === l.length, `${rendered} vs ${l.length}`);
  }
}

// --- the window actually contains the visible lines ---
{
  const text = Array.from({ length: 500 }, (_, i) => `objectNumber${i} OBJECT-TYPE`).join('\n');
  const l = lines(text);
  const { first, last } = window_(200 * LINE_HEIGHT, l.length);
  const out = renderWindow(text, l, first, last);
  check('a line inside the window is painted', out.includes('objectNumber200'));
  check('a line far outside it is not', !out.includes('objectNumber499'),
    out.includes('objectNumber499') ? 'line 499 was rendered' : '');
}

// --- an empty or one-line buffer ---
check('an empty buffer renders nothing', renderWindow('', [''], 0, 1) === '');
check('a one-line buffer renders', typeof renderWindow('x', ['x'], 0, 1) === 'string');

// --- markup in the source must not become markup in the mirror ---
{
  const nasty = 'a OBJECT-TYPE\n    DESCRIPTION "<script>alert(1)</script> & < >"\n';
  const out = renderWindow(nasty, lines(nasty), 0, 3);
  check('markup in a DESCRIPTION is escaped',
    !out.includes('<script>') && out.includes('&lt;script&gt;'),
    out.includes('<script>') ? 'a raw <script> reached the mirror' : '');
}

// --- a window opening INSIDE a multi-line string ---
{
  const text = [
    'x OBJECT-TYPE',
    '    DESCRIPTION "opening quote',
    ...Array.from({ length: 100 }, (_, i) => `    still inside the string ${i}`),
    '    closing quote"',
    'y OBJECT-TYPE',
  ].join('\n');
  const l = lines(text);
  // A window that starts in the middle of the string.
  const out = renderWindow(text, l, 50, 60);
  check('a window inside a string paints as a string',
    out.includes('class="str"'),
    'no string class — the rest of the file would be painted as code');
}

process.exit(failures ? 1 : 0);
