// Every custom property a component reads has to be one the theme defines.
//
// This exists because six components were reading four properties that are
// defined NOWHERE, and every one of them looked fine. An unresolvable `var()`
// is invalid at computed-value time, so the property takes its INHERITED value:
// a `color` inherits the text colour around it and a `background-color` stays
// transparent, which is close enough to the intent that nobody looks twice. The
// dimmer grey those components asked for simply never happened, in either
// theme, since the day they were written.
//
// It stopped being cosmetic the first time one landed on `stroke`. Its
// inherited value is `none`, so a map's links were drawn with no stroke at all
// — measured in a capture: 577 magenta pixels with a literal colour, and not
// one pixel with `var(--text-secondary)`. Nothing in the build, the linter or
// svelte-check says a word about any of it.
//
// A `var()` WITH a fallback is deliberate and is allowed: `var(--font-mono,
// monospace)` is how a component asks for something the theme may not have.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../src/', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

function walk(dir, out = []) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.(svelte|css|js)$/.test(entry)) out.push(full);
  }
  return out;
}

const files = walk(root);

// What is DEFINED anywhere: the theme's own declarations, and the properties a
// component sets on an element itself. The second kind is how the dashboard
// passes a widget's placement to the stylesheet — `--x` is written by
// presetLayout.js and read by DashboardPanel — and it is defined exactly where
// it is used rather than in the theme.
const defined = new Set();
for (const file of files) {
  const src = readFileSync(file, 'utf8');
  for (const m of src.matchAll(/(^|[;{\s"'`])(--[a-z0-9-]+)\s*:/gi)) defined.add(m[2]);
}
check('the theme defines some properties', defined.size > 20, `${defined.size} found`);
check('and the ones every panel uses', ['--bg-color', '--text-color', '--border-color'].every((v) => defined.has(v)));

// What the components read without a fallback.
const missing = new Map();
for (const file of files) {
  const src = readFileSync(file, 'utf8');
  // `var(--name)` with nothing after the name but the closing bracket. A
  // fallback is somebody saying "this may not exist", and that is allowed.
  for (const m of src.matchAll(/var\(\s*(--[a-z0-9-]+)\s*\)/gi)) {
    const name = m[1];
    if (defined.has(name)) continue;
    const short = file.slice(root.length).replace(/\\/g, '/');
    if (!missing.has(name)) missing.set(name, new Set());
    missing.get(name).add(short);
  }
}

for (const [name, users] of [...missing].sort()) {
  check(
    `${name} is defined somewhere`,
    false,
    `read with no fallback by ${[...users].join(', ')}`,
  );
}
if (missing.size === 0) {
  check('every property read without a fallback is defined', true);
}

console.log(failures === 0 ? '\ntheme variables: all checks passed' : `\ntheme variables: ${failures} failure(s)`);
process.exit(failures === 0 ? 0 : 1);
