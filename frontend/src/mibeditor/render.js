import { highlight, stringStateAt } from './tokenize.js';

/**
 * The coloured mirror for the visible window of a file.
 *
 * Extracted from the panel so it can be run without a browser. It threw
 * there — `allLines[i].length` on an undefined element — whenever the buffer
 * became shorter than the line the view was scrolled to, which happens on a
 * revert, a restore, an undo of a large paste, or opening a smaller file while
 * scrolled down. Being inside a `$:` statement, the throw landed in Svelte's
 * flush and stopped every reactive statement in the APPLICATION, not just this
 * component's: the window went dead with no way back.
 *
 * Unrendered lines are stood in for by blank lines, so the mirror keeps the
 * textarea's height and scroll sync still works.
 *
 * @param {string} text the whole buffer
 * @param {string[]} allLines text.split('\n')
 * @param {number} first index of the first line to paint
 * @param {number} last index one past the last line to paint
 */
export function renderWindow(text, allLines, first, last) {
  if (!text) return '';

  // Clamped to what EXISTS. `first` comes from scrollTop, which the browser
  // has not corrected yet at the moment the buffer shrinks.
  first = Math.max(0, Math.min(first, allLines.length));
  last = Math.min(Math.max(last, first), allLines.length);

  let offset = 0;
  for (let i = 0; i < first; i++) offset += allLines[i].length + 1;

  const windowText = allLines.slice(first, last).join('\n');
  return '\n'.repeat(first)
    + highlight(windowText, stringStateAt(text, offset))
    + '\n'.repeat(Math.max(0, allLines.length - last));
}
