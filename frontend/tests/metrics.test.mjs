// Position arithmetic for the editor's overlays.
//
// Squiggles, hover cards and the completion popup all reduce to "where is
// (line, column) in pixels". Getting it wrong by one tab stop puts a red
// underline under the wrong word, which is worse than no underline at all.
import { visualColumn, positionOf, lineColumnAt, wordAt, offsetAt } from '../src/mibeditor/metrics.js';

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

process.exit(failures ? 1 : 0);
