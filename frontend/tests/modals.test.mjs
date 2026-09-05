// `on:keydown|stopPropagation` swallows Escape.
//
// A modal is normally written as a backdrop that closes on Escape wrapping an
// inner box that stops CLICKS from reaching it. Writing the same modifier on
// keydown looks symmetrical and is not: a keydown starts at whatever has focus
// — an input inside the modal — and stopping it there means the backdrop's
// Escape handler never runs. The modal simply stops closing, with nothing in
// the build or the console to say so.
//
// The fix is always the same: stop clicks only, and put Escape on
// <svelte:window>, where focus and bubbling cannot reach it.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, basename } from 'node:path';

const root = new URL('../src/', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');

function svelteFiles(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) out.push(...svelteFiles(path));
    else if (name.endsWith('.svelte')) out.push(path);
  }
  return out;
}

/** Whether the file has a <svelte:window> carrying an on:keydown handler. */
function hasWindowKeydown(source) {
  let i = source.indexOf('<svelte:window');
  while (i >= 0) {
    // Scan to the tag's own '>', ignoring the ones inside {...} attribute
    // expressions — an arrow function is full of them.
    let j = i;
    let depth = 0;
    while (j < source.length) {
      const c = source[j];
      if (c === '{') depth++;
      else if (c === '}') depth--;
      else if (c === '>' && depth === 0) break;
      j++;
    }
    if (/on:keydown/.test(source.slice(i, j))) return true;
    i = source.indexOf('<svelte:window', j);
  }
  return false;
}

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const files = svelteFiles(root);
check('there are components to check', files.length > 0, `${files.length} files`);

const swallowers = [];
const unclosable = [];

// A dialog is recognised by its ROLE, not by the class its wrapper happens to
// carry. The name list this replaces held three of the six wrapper classes in
// use, so `set-confirm-backdrop` was never checked — and that dialog had no way
// out but the mouse until the accessibility pass found it. `role="dialog"` is
// the marker every one of them now carries, and it cannot drift the way a name
// list does: a new dialog gets checked because it IS one.
//
// It also excludes what is not a dialog. `drop-overlay` is the drag-and-drop
// indicator: no handlers, nothing to dismiss, it goes away when the drag ends.
// A name-matching rule would have demanded an Escape key for it.
const OVERLAY = /role="dialog"/;

for (const file of files) {
  const source = readFileSync(file, 'utf8');
  const name = basename(file);

  const stopsKeys = /on:keydown\|stopPropagation/.test(source);
  const windowEscape = hasWindowKeydown(source);
  // An Escape handler written as an attribute on an element, which therefore
  // depends on the event reaching that element.
  const bubblingEscape = /on:keydown=\{[^}]*Escape/.test(source);

  // The defect: something below stops the key an Escape handler is waiting
  // for. Detected by SHAPE, not by class name — the first version of this
  // test gated on `modal-backdrop` and so skipped every component that had
  // the bug, five of them, and passed.
  if (stopsKeys && bubblingEscape && !windowEscape) swallowers.push(name);

  // A dialog with no keyboard way out traps anyone not using a mouse.
  if (OVERLAY.test(source) && !windowEscape && !bubblingEscape) unclosable.push(name);
}

check('no dialog stops the keydown its own Escape handler waits for',
  swallowers.length === 0, swallowers.join(', '));
check('every dialog can be closed with Escape', unclosable.length === 0,
  unclosable.join(', '));

// The role is what the check keys on, so it has to actually be there. Eleven
// dialogs across ten files; a panel that loses the attribute stops being
// checked, silently, which is the failure mode this whole file exists to avoid.
const dialogFiles = files.filter((f) => OVERLAY.test(readFileSync(f, 'utf8')));
check('the dialogs are still marked as dialogs', dialogFiles.length >= 10,
  `${dialogFiles.length} files`);

// Prove the detector works, or a passing run means nothing.
check('the detector sees the modifier',
  /on:keydown\|stopPropagation/.test('<div on:click|stopPropagation on:keydown|stopPropagation>'));
check('the detector accepts stopping clicks only',
  !/on:keydown\|stopPropagation/.test('<div on:click|stopPropagation>'));
check('the detector accepts a window-level Escape',
  /svelte:window[^>]*on:keydown/.test('<svelte:window on:keydown={closeOnEscape} />'));
// The shape that was shipped broken five times over.
const brokenShape = `
  <div class="modal-overlay" on:keydown={(e) => e.key === 'Escape' && close()}>
    <div class="modal" on:click|stopPropagation on:keydown|stopPropagation></div>
  </div>`;
check('the detector rejects a bubbling Escape with a key-stopping child',
  /on:keydown\|stopPropagation/.test(brokenShape) &&
  /on:keydown=\{[^}]*Escape/.test(brokenShape) &&
  !hasWindowKeydown(brokenShape));

// Stopping keys is fine when Escape is handled where propagation cannot
// reach it, which is what ContextMenu does.
const okShape = `
  <svelte:window on:keydown={handleKeydown} />
  <div class="context-menu" on:click|stopPropagation on:keydown|stopPropagation></div>`;
check('the detector accepts stopping keys under a window-level Escape',
  hasWindowKeydown(okShape));

process.exit(failures ? 1 : 0);
