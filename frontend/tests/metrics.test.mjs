import { readFileSync } from 'node:fs';
// Position arithmetic for the editor's overlays.
//
// Squiggles, hover cards and the completion popup all reduce to "where is
// (line, column) in pixels". Getting it wrong by one tab stop puts a red
// underline under the wrong word, which is worse than no underline at all.
import { visualColumn, positionOf, lineColumnAt, wordAt, offsetAt, lineStarts, lineColumnFrom } from '../src/mibeditor/metrics.js';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

// A MIB may contain literal tabs; a tab advances to the next stop, not by one.
check('a tab advances to the next stop', visualColumn('\tx', 2) === 4, String(visualColumn('\tx', 2)));
check('a tab mid-line aligns to the stop', visualColumn('ab\tc', 4) === 4, String(visualColumn('ab\tc', 4)));
check('spaces count one each', visualColumn('    x', 5) === 4);
check('column 1 is zero', visualColumn('anything', 1) === 0);
check('past the end clamps', visualColumn('ab', 99) === 2);

const lines = ['first line', '\tindented', 'third'];
const p = positionOf(lines, 2, 2, 10, 18);
check('y follows the line', p.y === 18, String(p.y));
check('x expands the tab', p.x === 40, String(p.x));

const text = 'alpha\nbeta gamma\ndelta';
check('lineColumnAt finds line 2', lineColumnAt(text, 6).line === 2);
check('lineColumnAt finds column 1', lineColumnAt(text, 6).column === 1);
check('lineColumnAt mid-word', lineColumnAt(text, 8).column === 3);

check('wordAt in the middle', wordAt(text, 7).word === 'beta');
check('wordAt at a boundary', wordAt(text, 6).word === 'beta');
check('wordAt keeps hyphens', wordAt('MAX-ACCESS here', 2).word === 'MAX-ACCESS');
check('wordAt on whitespace is empty', wordAt('a  b', 2).word === '');
check('wordAt reports its start', wordAt(text, 7).start === 6);

// Round trip: a position turned into pixels and back must land on the same
// character, or clicking a diagnostic puts the caret in the wrong place.
const cw = 10;
const lh = 18;
for (const [line, column] of [[1, 1], [1, 5], [2, 3], [3, 2]]) {
  const px = positionOf(lines, line, column, cw, lh);
  const off = offsetAt(lines, lines.join('\n'), px.x, px.y, cw, lh);
  const back = lineColumnAt(lines.join('\n'), off);
  check(`round trip ${line}:${column}`, back.line === line && back.column === column,
    `got ${back.line}:${back.column}`);
}

check('a click below the last line returns null',
  offsetAt(lines, lines.join('\n'), 0, lh * 99, cw, lh) === null);


// --- resolving many offsets at once ---
//
// The find overlay called lineColumnAt twice per match for up to 2000 matches,
// and lineColumnAt slices the whole prefix and splits it. Measured on
// mibs/IP-MIB with the find bar open: 212 ms per keystroke, against 2.7 ms
// with it closed. Typing was unusable, and the same recompute fired on every
// scroll.
{
  const text = readFileSync(new URL('../../mibs/IP-MIB', import.meta.url), 'utf8');
  const lines = text.split('\n');
  const starts = lineStarts(lines);

  check('the index has one entry per line', starts.length === lines.length,
    `${starts.length} vs ${lines.length}`);
  check('the first line starts at 0', starts[0] === 0);

  // It must AGREE with the function it replaces, everywhere. A faster answer
  // that differs is not an optimisation.
  let mismatch = 0;
  for (let at = 0; at < text.length; at += 137) {
    const a = lineColumnAt(text, at);
    const b = lineColumnFrom(starts, at);
    if (a.line !== b.line || a.column !== b.column) mismatch++;
  }
  check('it agrees with lineColumnAt across the whole file', mismatch === 0,
    `${mismatch} disagreements`);

  // And at the awkward offsets.
  for (const at of [0, 1, text.length - 1, text.length, text.indexOf('\n'), text.indexOf('\n') + 1]) {
    const a = lineColumnAt(text, at);
    const b = lineColumnFrom(starts, at);
    check(`offset ${at} agrees`, a.line === b.line && a.column === b.column,
      `${JSON.stringify(a)} vs ${JSON.stringify(b)}`);
  }

  // Resolving every match must be fast enough to happen on a keystroke.
  const offsets = [];
  for (let i = text.indexOf('ip'); i >= 0 && offsets.length < 2000; i = text.indexOf('ip', i + 1)) {
    offsets.push(i);
  }
  const started = process.hrtime.bigint();
  for (let round = 0; round < 9; round++) {
    for (const at of offsets) lineColumnFrom(starts, at);
  }
  const ms = Number(process.hrtime.bigint() - started) / 1e6;
  check('2000 offsets resolved 9 times is a keystroke, not a pause', ms < 50,
    `${ms.toFixed(1)} ms for 9 rounds`);
}

// Negative and out-of-range offsets must not throw.
{
  const starts = lineStarts(['a', 'bb', 'ccc']);
  for (const at of [-5, 0, 99999]) {
    let threw = null;
    try { lineColumnFrom(starts, at); } catch (e) { threw = e; }
    check(`offset ${at} does not throw`, threw === null, threw && threw.message);
  }
  check('an empty index answers 1:1',
    JSON.stringify(lineColumnFrom([], 10)) === JSON.stringify({ line: 1, column: 1 }));
}

process.exit(failures ? 1 : 0);
