// Svelte's own warnings, treated as failures where they mean a runtime crash.
//
// `{#if redact}` for a variable no longer declared compiles to a reference to
// nothing: Svelte emits `missing-declaration`, the build succeeds, and the
// branch throws ReferenceError the first time it is evaluated. That shipped in
// TemplateEditor when it moved from four props to one — the warning was there
// the whole time, in output nobody reads.
//
// Only the codes that mean "this will break at runtime" are fatal here. Style
// warnings are Svelte's opinion and are left alone.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, basename } from 'node:path';
import { compile } from 'svelte/compiler';

const FATAL = new Set([
  // A reference to something that does not exist in the component.
  'missing-declaration',
  // A store subscription on something that is not a store.
  'invalid-store-subscription',
]);

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
check('there are components to compile', files.length > 0, `${files.length} files`);

const problems = [];
let compiled = 0;

for (const file of files) {
  const source = readFileSync(file, 'utf8');
  let result;
  try {
    result = compile(source, { name: basename(file, '.svelte'), generate: 'dom' });
  } catch (e) {
    problems.push(`${basename(file)}: does not compile — ${e.message}`);
    continue;
  }
  compiled++;
  for (const w of result.warnings) {
    if (FATAL.has(w.code)) {
      problems.push(`${basename(file)}:${w.start?.line ?? '?'} [${w.code}] ${w.message}`);
    }
  }
}

check('every component compiles', compiled === files.length, `${compiled}/${files.length}`);
check('no component references something it does not declare',
  problems.length === 0, problems.join(' | '));

// Prove the detector works, or a passing run means nothing.
{
  const broken = compile('<script>export let sink;</script>{#if redact}x{/if}', { name: 'Probe' });
  const saw = broken.warnings.some((w) => w.code === 'missing-declaration');
  check('the detector sees an undeclared reference', saw,
    broken.warnings.map((w) => w.code).join(','));

  const fine = compile('<script>export let redact;</script>{#if redact}x{/if}', { name: 'Probe' });
  check('and accepts a declared one',
    !fine.warnings.some((w) => w.code === 'missing-declaration'));
}

process.exit(failures ? 1 : 0);
