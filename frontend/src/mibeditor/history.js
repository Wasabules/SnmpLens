/**
 * Undo history for the editor.
 *
 * The browser's own stack in a textarea steps back one character at a time and
 * loses the caret, which is not what anyone means by undo. Real editors group
 * an edit RUN into one step: a word, a line, a paste, a replace-all — so one
 * Ctrl+Z takes back one thing the person did, not one thing the keyboard did.
 *
 * Two rules make that work, and both matter:
 *
 *   - Entries are DIFFS, not snapshots. A 185 KB MIB with a two-hundred-deep
 *     snapshot stack is 37 MB of strings for a text editor; the common prefix
 *     and suffix are trimmed, so a keystroke costs a few bytes.
 *   - The caret is part of the entry. Undoing to the right text with the cursor
 *     somewhere else is half an undo.
 *
 * Because this replaces the native stack rather than sitting beside it, the
 * component must intercept Ctrl+Z and its friends. Two histories that disagree
 * are worse than a coarse one.
 */

/** How long a run of typing stays open. Beyond this, a new step begins. */
export const COALESCE_MS = 600;

/** How many steps to keep. Deep enough to recover from a bad idea. */
export const MAX_DEPTH = 300;

/** Characters that close a typing run, so undo steps back by word and by line. */
const BOUNDARY = /[\s{}(),;:"']/;

/**
 * The smallest edit that turns `before` into `after`.
 * @param {string} before
 * @param {string} after
 * @returns {{at: number, removed: string, inserted: string}}
 */
export function diff(before, after, hint = -1) {
  // An edit happens at the caret, so look there first. Without the hint both
  // scans walk the whole file: inserting one character shifts the entire tail,
  // and every shifted character still MATCHES, so the suffix scan compares all
  // 185 KB before it finds the edit. That was 1.7 ms per keystroke.
  //
  // With the hint the two boundary checks are string comparisons, which the
  // engine does as a memcmp rather than a character loop, and the exact diff is
  // then computed over a 512-byte window.
  if (hint >= 0) {
    const fast = hintedDiff(before, after, hint);
    if (fast) return fast;
  }
  return exactDiff(before, after);
}

/** The window searched around the caret before falling back to a full scan. */
const HINT_WINDOW = 256;

function hintedDiff(before, after, hint) {
  const delta = after.length - before.length;
  const lo = Math.max(0, Math.min(hint, before.length, after.length) - HINT_WINDOW);

  // Everything before the window must be untouched...
  if (before.substring(0, lo) !== after.substring(0, lo)) return null;

  const hiBefore = Math.min(before.length, hint + HINT_WINDOW);
  const hiAfter = hiBefore + delta;
  if (hiAfter < lo || hiAfter > after.length) return null;

  // ...and everything after it must be untouched too, allowing for the shift.
  if (before.substring(hiBefore) !== after.substring(hiAfter)) return null;

  const d = exactDiff(before.substring(lo, hiBefore), after.substring(lo, hiAfter));
  return { at: lo + d.at, removed: d.removed, inserted: d.inserted };
}

function exactDiff(before, after) {
  let start = 0;
  const max = Math.min(before.length, after.length);
  while (start < max && before[start] === after[start]) start++;

  let endBefore = before.length;
  let endAfter = after.length;
  while (endBefore > start && endAfter > start && before[endBefore - 1] === after[endAfter - 1]) {
    endBefore--;
    endAfter--;
  }
  return {
    at: start,
    removed: before.slice(start, endBefore),
    inserted: after.slice(start, endAfter),
  };
}

/** Whether an edit continues the run recorded in `last`. */
export function shouldCoalesce(last, next, now, atomic) {
  if (!last || atomic || last.atomic) return false;
  if (now - last.time > COALESCE_MS) return false;

  const lastIsInsert = last.removed === '' && last.inserted !== '';
  const nextIsInsert = next.removed === '' && next.inserted !== '';
  const lastIsDelete = last.inserted === '' && last.removed !== '';
  const nextIsDelete = next.inserted === '' && next.removed !== '';

  // A run of typing: the new text starts exactly where the last ended.
  if (lastIsInsert && nextIsInsert) {
    if (next.at !== last.at + last.inserted.length) return false;
    // A boundary character ENDS the run it belongs to, so the next keystroke
    // starts a fresh step. That is what makes one undo take back one word.
    return !BOUNDARY.test(last.inserted[last.inserted.length - 1]);
  }

  // A run of backspaces: each deletion ends where the last one started.
  if (lastIsDelete && nextIsDelete) {
    if (next.at + next.removed.length !== last.at) return false;
    return !BOUNDARY.test(next.removed[0]);
  }

  // Anything else — a replacement, a direction change — is its own step.
  return false;
}

/**
 * Create a history over an initial document.
 * @param {string} initial
 * @param {() => number} [clock] injectable for tests
 */
export function createHistory(initial = '', clock = () => Date.now()) {
  let base = initial;      // the document as of the bottom of the stack
  let entries = [];        // applied edits, oldest first
  let cursor = 0;          // how many of them are currently applied

  function reset(text) {
    base = text;
    entries = [];
    cursor = 0;
  }

  /**
   * Record that the document became `after`.
   *
   * @param {string} before the text as it was
   * @param {string} after the text as it now is
   * @param {{start: number, end: number}} selBefore
   * @param {{start: number, end: number}} selAfter
   * @param {boolean} [atomic] force its own step — snippets, replace-all, the
   *   IMPORTS fix: one deliberate action, one undo.
   */
  function record(before, after, selBefore, selAfter, atomic = false) {
    if (before === after) return;
    // The caret is where the edit is, which is what makes the diff cheap.
    const hint = Math.min(selBefore?.start ?? -1, selAfter?.start ?? Number.MAX_SAFE_INTEGER);
    const d = diff(before, after, Number.isFinite(hint) && hint >= 0 ? hint : -1);
    if (d.removed === '' && d.inserted === '') return;

    // Anything undone is unreachable once a new edit lands, exactly as it is
    // in every editor.
    if (cursor < entries.length) entries.length = cursor;

    const now = clock();
    const last = entries[entries.length - 1];

    if (shouldCoalesce(last, d, now, atomic)) {
      // Extend the open run rather than starting a step.
      if (last.removed === '') {
        last.inserted += d.inserted;
      } else {
        last.at = d.at;
        last.removed = d.removed + last.removed;
      }
      last.time = now;
      last.selAfter = selAfter;
      return;
    }

    entries.push({
      at: d.at, removed: d.removed, inserted: d.inserted,
      selBefore, selAfter, time: now, atomic,
    });
    cursor = entries.length;

    if (entries.length > MAX_DEPTH) {
      // Fold the oldest edit into the base, so the stack stays bounded without
      // losing the document.
      const oldest = entries.shift();
      base = base.slice(0, oldest.at) + oldest.inserted + base.slice(oldest.at + oldest.removed.length);
      cursor = entries.length;
    }
  }

  /**
   * Step back one edit.
   * @param {string} current
   * @returns {{text: string, selection: {start: number, end: number}}|null}
   */
  function undo(current) {
    if (cursor === 0) return null;
    const e = entries[cursor - 1];
    cursor--;
    return {
      text: current.slice(0, e.at) + e.removed + current.slice(e.at + e.inserted.length),
      selection: e.selBefore,
    };
  }

  /**
   * Step forward one edit.
   * @param {string} current
   */
  function redo(current) {
    if (cursor >= entries.length) return null;
    const e = entries[cursor];
    cursor++;
    return {
      text: current.slice(0, e.at) + e.inserted + current.slice(e.at + e.removed.length),
      selection: e.selAfter,
    };
  }

  /** Close the open run, so the next keystroke starts a fresh step. */
  function breakRun() {
    const last = entries[entries.length - 1];
    if (last) last.atomic = true;
  }

  return {
    record,
    undo,
    redo,
    reset,
    breakRun,
    get canUndo() { return cursor > 0; },
    get canRedo() { return cursor < entries.length; },
    get depth() { return entries.length; },
    get base() { return base; },
  };
}
