/**
 * Find and replace over the editor buffer.
 *
 * Kept out of the component because it is the part that can be wrong in ways
 * nobody sees until a replace-all has already run over a 5,000-line MIB.
 */

/** A file can hold thousands of hits; drawing them all would help nobody. */
export const MAX_MATCHES = 2000;

/**
 * Offsets of every occurrence of `term`, case-insensitively.
 *
 * Matches never overlap: the scan continues past the end of each hit, so
 * searching "aa" in "aaaa" finds two, not three. That is what replace-all
 * needs, and what every editor does.
 *
 * @param {string} text
 * @param {string} term
 * @returns {number[]} character offsets, ascending
 */
export function findMatches(text, term, limit = MAX_MATCHES, caseSensitive = false) {
  if (!term) return [];
  const hay = String(text ?? '');
  const out = [];

  if (caseSensitive) {
    let at = hay.indexOf(term);
    while (at >= 0 && out.length < limit) {
      out.push(at);
      at = hay.indexOf(term, at + term.length);
    }
    return out;
  }

  const lower = hay.toLowerCase();
  const needle = term.toLowerCase();

  // The fast path is only safe while case folding preserves LENGTH.
  //
  // It does not always: U+0130 (İ) lowercases to two code units, so every
  // offset after one is shifted by a character — and applyReplaceAll then
  // applies them to the ORIGINAL text. Measured: replacing "foo" in a file
  // whose DESCRIPTION contained "İstanbul" produced "fbarOBJECT-TYPE", eating
  // a character and the space after it. A MIB written by a Turkish vendor is
  // not exotic.
  if (lower.length === hay.length) {
    let at = lower.indexOf(needle);
    while (at >= 0 && out.length < limit) {
      out.push(at);
      at = lower.indexOf(needle, at + needle.length);
    }
    return out;
  }

  // Otherwise compare in the original string, where the offsets are real.
  for (let i = 0; i + term.length <= hay.length && out.length < limit; ) {
    if (hay.substr(i, term.length).toLowerCase() === needle) {
      out.push(i);
      i += term.length;
    } else {
      i++;
    }
  }
  return out;
}

/**
 * Every occurrence, with no cap.
 *
 * The cap on findMatches exists so the editor does not draw ten thousand
 * highlight boxes. Replace-all must not inherit it: replacing the first two
 * thousand and reporting success would leave a file half-rewritten, which is
 * the one outcome worse than refusing.
 *
 * @param {string} text
 * @param {string} term
 * @returns {number[]}
 */
export function findAllMatches(text, term, caseSensitive = false) {
  return findMatches(text, term, Infinity, caseSensitive);
}

/**
 * The whole buffer with every match replaced.
 *
 * Built in one pass from the match offsets rather than by repeated string
 * replacement: a replacement that contains the search term would otherwise be
 * found again and replaced forever, and `String.replaceAll` would also rewrite
 * hits the user had scrolled past a cap.
 *
 * @param {string} text
 * @param {number[]} matches offsets from findMatches
 * @param {number} termLength
 * @param {string} replacement
 * @returns {string}
 */
export function applyReplaceAll(text, matches, termLength, replacement) {
  if (!matches.length || termLength <= 0) return text;
  let out = '';
  let last = 0;
  for (const at of matches) {
    out += text.slice(last, at) + replacement;
    last = at + termLength;
  }
  return out + text.slice(last);
}

/**
 * The next match index, wrapping in both directions.
 * @param {number} current
 * @param {number} total
 * @param {boolean} backwards
 */
export function nextIndex(current, total, backwards) {
  if (total <= 0) return 0;
  return ((current + (backwards ? -1 : 1)) % total + total) % total;
}
