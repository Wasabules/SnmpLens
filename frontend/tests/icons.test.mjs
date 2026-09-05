// Every <Icon name="..."> names an icon that exists.
//
// `Icon.svelte` renders `{@html icons[name] || ''}`. A name that is not in the
// registry produces a 24x24 SVG with nothing in it — no error, no warning, no
// blank square, just a gap the width of an icon that reads as spacing. It
// shipped: the credential-custody banner in SnmpSettings asked for
// `shield-check`, which was never added, so the one line telling the user their
// passwords are held by the OS keychain rendered with no icon beside it.
//
// This is the check a `keyof typeof icons` would give a TypeScript codebase,
// and it costs one file instead of a migration.
import { readdirSync, readFileSync } from 'node:fs';
import { join, basename } from 'node:path';
import { icons } from '../src/icons.js';

const root = new URL('../src/', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');

// `withFileTypes` rather than a stat per entry: readdir already knows what each
// one is, so asking again is a second answer to a question that can change
// between the two — which is what CodeQL's js/file-system-race is about, and
// what tools/serve-site.mjs says at more length.
function svelteFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...svelteFiles(path));
    else if (entry.name.endsWith('.svelte')) out.push(path);
  }
  return out;
}

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

/** Literal icon names used in a component, and how many are computed. */
function iconNames(source) {
  const literal = [];
  let dynamic = 0;
  for (const tag of source.matchAll(/<Icon\b[^>]*>/g)) {
    const m = tag[0].match(/\bname\s*=\s*"([^"]*)"/);
    if (m) literal.push(m[1]);
    else if (/\bname\s*=\s*\{/.test(tag[0])) dynamic++;
  }
  return { literal, dynamic };
}

const files = svelteFiles(root);

// Every .svelte AND every .js under src/, because a name chosen by type is a
// string in a helper rather than an attribute value.
const allSources = [];
(function collect(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) collect(path);
    else if ((entry.name.endsWith('.svelte') || entry.name.endsWith('.js')) && entry.name !== 'icons.js') {
      allSources.push(readFileSync(path, 'utf8'));
    }
  }
})(root);

const missing = [];
let checked = 0;
let computed = 0;

for (const file of files) {
  const { literal, dynamic } = iconNames(readFileSync(file, 'utf8'));
  computed += dynamic;
  for (const name of literal) {
    checked++;
    if (!(name in icons)) missing.push(`${basename(file)}: "${name}"`);
  }
}

check('there are icons to check', checked > 0, `${checked} literal names, ${computed} computed`);
check('every literal icon name is in the registry', missing.length === 0, missing.join(', '));

// A registry entry nothing uses is not a defect — it is weight, and worth
// knowing about, but the icons file says up front that it holds only what is
// used, so a drift here means that sentence has stopped being true.
//
// Searched in the WHOLE source and not only inside an `<Icon>` tag: a name
// chosen by node type lives in the script, as a helper returning 'table' or
// 'file'. Looking only at the attribute reported five icons as orphans that six
// components use.
const everywhere = allSources.join('\n');
const orphans = Object.keys(icons).filter(
  (n) => !everywhere.includes("'" + n + "'") && !everywhere.includes('"' + n + '"'));
check('the registry holds only icons the interface uses', orphans.length === 0, orphans.join(', '));

// Prove the detector detects, or a green run means nothing.
{
  const probe = '<Icon name="not-a-real-icon" size={14} />';
  const { literal } = iconNames(probe);
  check('the detector reads a literal name', literal.length === 1 && literal[0] === 'not-a-real-icon');
  check('and that name is absent from the registry', !('not-a-real-icon' in icons));

  const dyn = iconNames('<Icon name={whatever} />');
  check('and counts a computed name rather than reading it',
    dyn.literal.length === 0 && dyn.dynamic === 1);

  // The one that shipped.
  check('shield-check, which was missing, is there now', 'shield-check' in icons);
}

process.exit(failures ? 1 : 0);
