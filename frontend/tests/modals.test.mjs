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

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const files = svelteFiles(root);
check('there are components to check', files.length > 0, `${files.length} files`);

const swallowers = [];
const unclosable = [];

for (const file of files) {
  const source = readFileSync(file, 'utf8');
  const name = basename(file);

  // Only inside a modal. Stopping keydown is perfectly correct on a menu or a
  // field that handles its own keys — the bug is stopping it BELOW something
  // that still needs to see Escape.
  if (!/class="modal-backdrop"/.test(source)) continue;

  const windowEscape = /svelte:window[^>]*on:keydown/.test(source);
  const anyEscape = windowEscape || /key\s*===\s*['"]Escape['"]/.test(source);

  if (!anyEscape) unclosable.push(name);

  // A window-level handler is out of reach of stopPropagation, so the two are
  // only in conflict when Escape is expected to bubble.
  if (!windowEscape && /on:keydown\|stopPropagation/.test(source)) {
    swallowers.push(name);
  }
}

check('no modal stops the keydown its own Escape handler needs',
  swallowers.length === 0, swallowers.join(', '));
check('every modal can be closed with Escape', unclosable.length === 0,
  unclosable.join(', '));

// Prove the detector works, or a passing run means nothing.
check('the detector sees the modifier',
  /on:keydown\|stopPropagation/.test('<div on:click|stopPropagation on:keydown|stopPropagation>'));
check('the detector accepts stopping clicks only',
  !/on:keydown\|stopPropagation/.test('<div on:click|stopPropagation>'));
check('the detector accepts a window-level Escape',
  /svelte:window[^>]*on:keydown/.test('<svelte:window on:keydown={closeOnEscape} />'));
check('the detector ignores a component that is not a modal',
  !/class="modal-backdrop"/.test('<div class="menu" on:keydown|stopPropagation>'));

process.exit(failures ? 1 : 0);
