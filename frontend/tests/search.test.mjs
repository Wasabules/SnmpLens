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


// --- case folding must not move the offsets ---
//
// findMatches lowercased the whole haystack and searched THAT, then handed the
// offsets to applyReplaceAll, which applies them to the ORIGINAL text. Most
// case mappings preserve length; U+0130 (Turkish dotted capital I) lowercases
// to two code units, so every offset after one was shifted by a character.
// Measured: replacing "foo" in a file whose DESCRIPTION held "İstanbul"
// produced "fbarOBJECT-TYPE" — a character eaten and the space with it.
{
  const text = 'DESCRIPTION \"\u0130stanbul\"\nfoo OBJECT-TYPE\n';
  check('the fixture really has a length-changing character',
    'İ'.length === 2 || 'İ'.toLowerCase().length === 2);

  const m = findMatches(text, 'foo');
  check('the match offset points at the match',
    m.length === 1 && text.substr(m[0], 3) === 'foo',
    JSON.stringify([m, text.substr(m[0] ?? 0, 3)]));

  const out = applyReplaceAll(text, m, 3, 'bar');
  check('replace-all does not eat the neighbouring characters',
    out === 'DESCRIPTION \"\u0130stanbul\"\nbar OBJECT-TYPE\n', JSON.stringify(out));
}

// --- case sensitivity ---
//
// Counter32 and counter32 are different things in a MIB, and replacing one
// used to rewrite the other with no way to say otherwise.
{
  const text = 'Counter32 counter32 COUNTER32';
  check('insensitive finds all three', findMatches(text, 'counter32').length === 3);
  check('sensitive finds only the exact one',
    findMatches(text, 'counter32', 2000, true).length === 1);
  check('sensitive finds the one that is there',
    findMatches(text, 'Counter32', 2000, true)[0] === 0);
  check('findAllMatches carries the flag',
    findAllMatches(text, 'counter32', true).length === 1);
}

// --- the cap is a display cap, not a rewrite cap ---
{
  const text = 'ip '.repeat(3000);
  const shown = findMatches(text, 'ip');
  const all = findAllMatches(text, 'ip');
  check('the display list stops at the cap', shown.length === MAX_MATCHES, String(shown.length));
  check('replace-all sees every one', all.length === 3000, String(all.length));

  const out = applyReplaceAll(text, all, 2, 'x');
  check('and rewrites every one', out.indexOf('ip') < 0,
    out.indexOf('ip') >= 0 ? 'an ip survived at ' + out.indexOf('ip') : '');
}

// --- a replacement containing the search term ---
{
  const text = 'ip ip ip';
  const all = findAllMatches(text, 'ip');
  const out = applyReplaceAll(text, all, 2, 'ipAddress');
  check('a replacement containing the term is not replaced again',
    out === 'ipAddress ipAddress ipAddress', JSON.stringify(out));
}

process.exit(failures ? 1 : 0);
