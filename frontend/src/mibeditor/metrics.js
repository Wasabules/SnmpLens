/**
 * Turning a (line, column) into pixels inside the editor.
 *
 * This is what makes squiggles, hover cards and an anchored completion popup
 * possible at all. It works here and would not in a plain textarea, because the
 * editor already renders a mirror whose layout is character-identical to the
 * text — that invariant is unit-tested — and both layers are monospace with a
 * fixed line height. So a position is arithmetic rather than measurement.
 *
 * The one thing arithmetic cannot assume is the width of a character, which
 * depends on the font the platform actually resolved. That is measured once.
 */

const TAB_SIZE = 4;

let cachedWidth = 0;
let cachedFont = '';

/**
 * Width of one character, measured against the element's resolved font.
 * @param {HTMLElement} el an element using the editor's font
 * @returns {number} pixels
 */
export function charWidth(el) {
  if (!el) return 7.2;
  const font = getComputedStyle(el).font;
  if (font === cachedFont && cachedWidth) return cachedWidth;

  const canvas = charWidth._canvas || (charWidth._canvas = document.createElement('canvas'));
  const ctx = canvas.getContext('2d');
  ctx.font = font;
  // Measure a run and divide: a single glyph rounds badly at small sizes.
  const width = ctx.measureText('0'.repeat(100)).width / 100;

  cachedFont = font;
  cachedWidth = width || 7.2;
  return cachedWidth;
}

/**
 * Visual column of a byte column on a line, expanding tabs.
 *
 * A MIB may contain literal tabs — the validator warns about them but does not
 * refuse them — and a tab advances to the next multiple of the tab size rather
 * than by one character.
 * @param {string} lineText
 * @param {number} column 1-based
 * @returns {number} 0-based visual column
 */
export function visualColumn(lineText, column) {
  let visual = 0;
  const upto = Math.max(0, Math.min((column || 1) - 1, lineText.length));
  for (let i = 0; i < upto; i++) {
    if (lineText[i] === '\t') {
      visual += TAB_SIZE - (visual % TAB_SIZE);
    } else {
      visual += 1;
    }
  }
  return visual;
}

/**
 * Pixel position of a (line, column) relative to the scrolled content.
 * @param {string[]} lines
 * @param {number} line 1-based
 * @param {number} column 1-based
 * @param {number} cw character width
 * @param {number} lh line height
 */
export function positionOf(lines, line, column, cw, lh) {
  const text = lines[Math.max(0, (line || 1) - 1)] ?? '';
  return {
    x: visualColumn(text, column) * cw,
    y: ((line || 1) - 1) * lh,
  };
}

/**
 * The (line, column) at a character offset.
 * @param {string} text
 * @param {number} offset
 */
export function lineColumnAt(text, offset) {
  const upto = text.slice(0, Math.max(0, offset));
  const lines = upto.split('\n');
  return { line: lines.length, column: lines[lines.length - 1].length + 1 };
}

/**
 * The word around a character offset, and where it starts.
 *
 * Used by hover and by completion, which need the same answer: what is the
 * identifier the user is pointing at or typing.
 * @param {string} text
 * @param {number} offset
 */
export function wordAt(text, offset) {
  const isPart = (c) => /[A-Za-z0-9_-]/.test(c);
  let start = Math.max(0, Math.min(offset, text.length));
  let end = start;
  while (start > 0 && isPart(text[start - 1])) start--;
  while (end < text.length && isPart(text[end])) end++;
  return { word: text.slice(start, end), start, end };
}

/**
 * The offset at a pixel position inside the content box.
 * @param {string[]} lines
 * @param {string} text
 * @param {number} px relative to the content, scroll already added
 * @param {number} py
 * @param {number} cw
 * @param {number} lh
 * @returns {number|null} character offset, or null when past the end
 */
export function offsetAt(lines, text, px, py, cw, lh) {
  const line = Math.floor(py / lh);
  if (line < 0 || line >= lines.length) return null;

  const target = Math.round(px / cw);
  const lineText = lines[line];

  // Walk the line so tabs land on the right character.
  let visual = 0;
  let col = 0;
  while (col < lineText.length && visual < target) {
    visual += lineText[col] === '\t' ? TAB_SIZE - (visual % TAB_SIZE) : 1;
    col++;
  }

  let offset = 0;
  for (let i = 0; i < line; i++) offset += lines[i].length + 1;
  return offset + col;
}
