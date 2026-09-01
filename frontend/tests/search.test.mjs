// Find and replace over the editor buffer.
//
// Replace-all runs over a whole MIB in one go, so the ways it can be wrong are
// the ways that damage a file: a replacement containing the search term looping
// forever, overlapping matches eating each other, or a cap silently rewriting
// only part of the document.
import { findMatches, findAllMatches, applyReplaceAll, nextIndex, MAX_MATCHES } from '../src/mibeditor/search.js';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

check('finds every occurrence', findMatches('a b a b a', 'a').length === 3);
check('is case-insensitive', findMatches('Counter32 counter32', 'COUNTER32').length === 2);
check('an empty term matches nothing', findMatches('anything', '').length === 0);
check('a term that is absent matches nothing', findMatches('abc', 'z').length === 0);

// Non-overlapping: "aa" in "aaaa" is two matches, not three.
check('matches do not overlap', findMatches('aaaa', 'aa').length === 2,
  String(findMatches('aaaa', 'aa').length));

const text = 'ifInOctets and ifInOctets again';
const m = findMatches(text, 'ifInOctets');
check('replace-all rewrites every hit',
  applyReplaceAll(text, m, 10, 'ifOutOctets') === 'ifOutOctets and ifOutOctets again');

// The loop trap: a replacement that contains the term must be written once.
const loopy = 'foo foo';
const lm = findMatches(loopy, 'foo');
check('a replacement containing the term does not recurse',
  applyReplaceAll(loopy, lm, 3, 'foobar') === 'foobar foobar',
  applyReplaceAll(loopy, lm, 3, 'foobar'));

// Replacing with the empty string is a deletion, and must not shift wrongly.
check('replacing with nothing deletes cleanly',
  applyReplaceAll('a-b-c', findMatches('a-b-c', '-'), 1, '') === 'abc');

// Case is preserved in the surrounding text even though matching ignores it.
const mixed = 'Counter32 counter32';
check('surrounding text keeps its case',
  applyReplaceAll(mixed, findMatches(mixed, 'counter32'), 9, 'X') === 'X X');

check('nothing to replace leaves the text alone',
  applyReplaceAll('abc', [], 3, 'z') === 'abc');
check('a zero-length term leaves the text alone',
  applyReplaceAll('abc', [0], 0, 'z') === 'abc');

// Cycling wraps in both directions, which is what Enter and Shift+Enter do.
check('next wraps forward', nextIndex(2, 3, false) === 0);
check('previous wraps backward', nextIndex(0, 3, true) === 2);
check('next advances', nextIndex(0, 3, false) === 1);
check('cycling with no matches stays at zero', nextIndex(0, 0, false) === 0);

// The cap must be a cap, not a corruption: a capped search must not lead to a
// replace-all that rewrites only the first N and looks complete.
const many = 'x'.repeat(MAX_MATCHES + 500);
const capped = findMatches(many, 'x');
check('the match list is capped', capped.length === MAX_MATCHES, String(capped.length));
const replaced = applyReplaceAll(many, capped, 1, 'y');
check('a capped replace-all leaves the remainder untouched rather than dropping it',
  replaced.length === many.length && replaced.endsWith('x'),
  `${replaced.length} vs ${many.length}`);

// Performance on a realistic file.
const big = 'ifInOctets OBJECT-TYPE\n'.repeat(8000); // ~180 KB
let t = process.hrtime.bigint();
const bigMatches = findMatches(big, 'OBJECT-TYPE');
const ms = Number(process.hrtime.bigint() - t) / 1e6;
check('searching a 180 KB file is instant', ms < 50, `${ms.toFixed(1)} ms for ${bigMatches.length} hits`);


// Replace-all must NOT inherit the display cap. Replacing the first two
// thousand and reporting success would leave a file half-rewritten.
const huge = 'x'.repeat(MAX_MATCHES + 500);
const uncapped = findAllMatches(huge, 'x');
check('findAllMatches ignores the display cap', uncapped.length === MAX_MATCHES + 500,
  String(uncapped.length));
const fully = applyReplaceAll(huge, uncapped, 1, 'y');
check('an uncapped replace-all rewrites the whole file',
  fully === 'y'.repeat(MAX_MATCHES + 500) && !fully.includes('x'));

process.exit(failures ? 1 : 0);
