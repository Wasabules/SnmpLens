// No rendered expression depends on state it does not name.
//
// See tests/reactivedeps.mjs for what that means and why it is worth a test: it
// is a value that renders correctly once and then silently stops updating, which
// nobody reports because the first screen is right. The Svelte 5 migration
// turned eight of them from latent into live, and only one was visible.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, basename } from 'node:path';
import { unnamedStateReads } from './reactivedeps.mjs';

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

const problems = [];
const files = svelteFiles(root);
for (const file of files) {
  for (const hit of unnamedStateReads(readFileSync(file, 'utf8'))) {
    problems.push(`${basename(file)}: ${hit.fn}() reads ${hit.reads.join(', ')} without taking it`);
  }
}

check('every rendered expression names the state it depends on',
  problems.length === 0, problems.join(' | '));

// The detector detects. Both halves matter: the first proves it sees the shape,
// the second proves the fix this test asks for actually satisfies it — a check
// nothing can pass is as useless as one nothing can fail.
{
  const broken = `<script>
  let targets = [];
  function count(id) { return targets.filter((t) => t.g === id).length; }
</script>
<span>{count('a')}</span>`;
  const hits = unnamedStateReads(broken);
  check('the detector sees a rendered call reaching for state',
    hits.length === 1 && hits[0].fn === 'count' && hits[0].reads.includes('targets'),
    JSON.stringify(hits));

  const fixed = `<script>
  let targets = [];
  function count(id, list) { return list.filter((t) => t.g === id).length; }
</script>
<span>{count('a', targets)}</span>`;
  check('and accepts the same code once the value is passed in',
    unnamedStateReads(fixed).length === 0, JSON.stringify(unnamedStateReads(fixed)));

  // An event handler reading instance state is how a handler works. Reporting
  // those is what takes this from 4 findings to 82 and gets the test ignored.
  const handler = `<script>
  let open = false;
  function toggle() { open = !open; }
</script>
<button on:click={toggle}>x</button>`;
  check('and says nothing about an event handler',
    unnamedStateReads(handler).length === 0, JSON.stringify(unnamedStateReads(handler)));

  // `const` cannot be reassigned, so nothing is missed by reading one.
  const constant = `<script>
  const LIMIT = 10;
  function cap(n) { return Math.min(n, LIMIT); }
</script>
<span>{cap(3)}</span>`;
  check('and says nothing about a constant',
    unnamedStateReads(constant).length === 0, JSON.stringify(unnamedStateReads(constant)));

  // A PROP, which is the state most likely to arrive after the first render.
  // `export let x` wraps the declaration in an ExportNamedDeclaration, and not
  // unwrapping that hid half of these — including the one deciding between
  // `6` and `ethernetCsmacd(6)` in the results table.
  const prop = `<script>
  export let cache = {};
  function label(oid) { return cache[oid] || oid; }
</script>
<span>{label('1.3.6')}</span>`;
  check('the detector sees a prop read through a callee',
    unnamedStateReads(prop).length === 1, JSON.stringify(unnamedStateReads(prop)));

  // The shape that actually shipped: a value in `{@const}`.
  const atConst = `<script>
  let decoded = [];
  function build(rows) { return rows.concat(decoded); }
</script>
{#each [1] as r}{@const t = build([r])}<span>{t.length}</span>{/each}`;
  check('and sees it inside {@const}', unnamedStateReads(atConst).length === 1,
    JSON.stringify(unnamedStateReads(atConst)));
}

process.exit(failures ? 1 : 0);
