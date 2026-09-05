// Every component compiles, and none of them reads a name nothing declares.
//
// `{#if redact}` for a variable no longer declared compiles to a reference to
// nothing, the build succeeds, and the branch throws ReferenceError the first
// time it is evaluated. That shipped in TemplateEditor when it moved from four
// props to one.
//
// Svelte 3 caught it for us with a `missing-declaration` warning. SVELTE 5
// REMOVED THAT WARNING, and no compile option brings it back — checked against
// `dev`, `runes`, `generate: 'server'` and every combination. Its companion
// `invalid-store-subscription` is gone too. So the check that mattered would
// have gone on passing while detecting nothing, which is worse than not having
// it: a green test that cannot fail teaches you to trust something that is no
// longer looking.
//
// The question is asked of the GENERATED code instead. An undeclared template
// reference survives compilation as a bare global read — literally
// `if (inconnu) $$render(consequent);` — and a free identifier in a module is
// something a parser can find. See tests/freevars.mjs for the one deliberate
// approximation.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, basename } from 'node:path';
import { compile } from 'svelte/compiler';
import { parse } from 'acorn';
import { freeIdentifiers } from './freevars.mjs';

// Names Svelte's own output reaches for that are neither imported nor declared
// in the module it emits.
const SVELTE_RUNTIME = new Set(['$$props', '$$restProps', '$$slots']);

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

/** The names a component's compiled output reads and never binds. */
function undeclaredIn(source, name) {
  const { js } = compile(source, { name, generate: 'client' });
  const ast = parse(js.code, { ecmaVersion: 'latest', sourceType: 'module' });
  return freeIdentifiers(ast, SVELTE_RUNTIME);
}

const files = svelteFiles(root);
check('there are components to compile', files.length > 0, `${files.length} files`);

const problems = [];
let compiled = 0;

for (const file of files) {
  const source = readFileSync(file, 'utf8');
  let free;
  try {
    free = undeclaredIn(source, basename(file, '.svelte'));
  } catch (e) {
    problems.push(`${basename(file)}: does not compile — ${e.message}`);
    continue;
  }
  compiled++;
  if (free.length) {
    problems.push(`${basename(file)}: reads ${free.join(', ')}`);
  }
}

check('every component compiles', compiled === files.length, `${compiled}/${files.length}`);
check('no component references something it does not declare',
  problems.length === 0, problems.join(' | '));

// Prove the detector works, or a passing run means nothing. This is the assertion
// that caught Svelte 5 removing the warning the old version depended on — it did
// exactly the job it was put there for.
{
  const broken = undeclaredIn('<script>export let sink;</script>{#if redact}x{/if}', 'Probe');
  check('the detector sees an undeclared reference', broken.includes('redact'),
    broken.join(',') || 'saw nothing');

  const declared = undeclaredIn('<script>export let redact;</script>{#if redact}x{/if}', 'Probe');
  check('and accepts a declared one', !declared.includes('redact'), declared.join(','));

  // A name declared in the script but only read in the template, which is the
  // shape most of this codebase is written in.
  const scriptOnly = undeclaredIn('<script>let n = 1;</script><p>{n}</p>', 'Probe');
  check('and accepts a script local read from the template',
    !scriptOnly.includes('n'), scriptOnly.join(','));

  // An each-block local, which exists only in the template. A checker that
  // missed this would flag half the components in this repository.
  const eachLocal = undeclaredIn(
    '<script>let rows = [];</script>{#each rows as row}<p>{row.id}</p>{/each}', 'Probe');
  check('and accepts an each-block local', eachLocal.length === 0, eachLocal.join(','));

  // A browser global, likewise.
  const global = undeclaredIn('<script>let x = window.innerWidth;</script><p>{x}</p>', 'Probe');
  check('and accepts a browser global', global.length === 0, global.join(','));
}

process.exit(failures ? 1 : 0);
