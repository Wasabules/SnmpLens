// A required prop that is never passed is silent.
//
// Svelte does not check props: `export let jsonPayload = false` in a child and
// no `jsonPayload=` at the call site compiles, renders, and quietly uses the
// default forever. That is how a webhook preview stayed in text mode after the
// payload had become JSON — the feature was written on both sides and simply
// never connected, and nothing in the build or the browser said so.
//
// So: every prop declared WITHOUT a default is required, and every place the
// component is used must pass it.
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

function scriptOf(source) {
  const open = source.indexOf('<script');
  if (open < 0) return '';
  const start = source.indexOf('>', open) + 1;
  const end = source.indexOf('</script>', start);
  return end < 0 ? '' : source.slice(start, end);
}

/** Props with no default: `export let x;` but not `export let x = …`. */
function requiredProps(code) {
  const out = [];
  const re = /^\s*export\s+let\s+([A-Za-z_$][\w$]*)\s*(=|;|$)/gm;
  let m;
  while ((m = re.exec(code)) !== null) {
    if (m[2] !== '=') out.push(m[1]);
  }
  return out;
}

/**
 * The attribute text of every `<Name …>` opening tag.
 *
 * Scanned rather than matched: an attribute can contain `>` — every arrow
 * function does — so stopping at the first one would silently truncate the
 * attributes and report a prop as missing when it is right there.
 */
function usages(source, name) {
  const out = [];
  const re = new RegExp(String.raw`<${name}(?=[\s/>])`, 'g');
  let m;
  while ((m = re.exec(source)) !== null) {
    let i = m.index + name.length + 1;
    let depth = 0;
    let quote = '';
    while (i < source.length) {
      const c = source[i];
      if (quote) {
        if (c === quote) quote = '';
      } else if (c === '"' || c === "'") {
        quote = c;
      } else if (c === '{') {
        depth++;
      } else if (c === '}') {
        depth--;
      } else if (c === '>' && depth === 0) {
        break;
      }
      i++;
    }
    out.push(source.slice(m.index + name.length + 1, i));
  }
  return out;
}

/** Whether `attrs` passes `prop`, in any of the three spellings Svelte allows. */
function passes(attrs, prop) {
  return (
    new RegExp(String.raw`(^|\s)(bind:)?${prop}\s*=`).test(attrs) ||
    // `bind:x` is shorthand for `bind:x={x}` and carries no `=`.
    new RegExp(String.raw`(^|\s)bind:${prop}(\s|/|$)`).test(attrs) ||
    new RegExp(String.raw`\{\s*${prop}\s*\}`).test(attrs)
  );
}

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const files = svelteFiles(root);
const sources = new Map(files.map((f) => [f, readFileSync(f, 'utf8')]));

const required = new Map();
for (const [file, source] of sources) {
  const props = requiredProps(scriptOf(source));
  if (props.length) required.set(basename(file, '.svelte'), props);
}
check('components declare required props', required.size > 0, `${required.size} components`);

const missing = [];
for (const [component, props] of required) {
  for (const [file, source] of sources) {
    if (basename(file, '.svelte') === component) continue;
    for (const attrs of usages(source, component)) {
      if (attrs.includes('{...')) continue; // spread: cannot tell statically
      for (const prop of props) {
        if (!passes(attrs, prop)) {
          missing.push(`${basename(file)} → <${component}> is missing "${prop}"`);
        }
      }
    }
  }
}
check('every required prop is passed at every call site', missing.length === 0, missing.join(' | '));

// An event handler that calls a browser global instead of a component
// function.
//
// `on:click={() => history('undo')}` compiles, renders, and throws
// "history is not a function" the moment it is clicked — `history` is
// window.history, the History object, which is not callable. The undo and redo
// toolbar buttons shipped that way and never worked once; nothing in the
// build, the tests or the console-free happy path saw it.
const CALLABLE_GLOBALS = [
  // Globals that exist on window and are NOT functions, so calling one is
  // always a bug. Shadowing any of them with a local is the usual cause.
  'history', 'location', 'navigator', 'screen', 'performance', 'crypto',
  'localStorage', 'sessionStorage', 'document', 'status', 'origin', 'name',
];

const shadowed = [];
for (const [file, source] of sources) {
  const code = scriptOf(source);
  const markup = source.slice(code ? source.indexOf(code) + code.length : 0);

  for (const g of CALLABLE_GLOBALS) {
    // A call to the bare name, in the markup, where no local of that name
    // exists in the script.
    const called = new RegExp(String.raw`[^.\w]${g}\s*\(`).test(markup);
    if (!called) continue;
    const declared = new RegExp(
      String.raw`(function\s+${g}|(?:const|let|var)\s+${g})`
    ).test(code);
    if (!declared) {
      shadowed.push(`${basename(file)}: on-markup call to \`${g}(…)\`, which is the browser global and not a function`);
    }
  }
}

check('no handler calls a browser global as a function',
  shadowed.length === 0, shadowed.join(' | '));

// Prove the detector works, or a passing run means nothing.
const child = `<script>export let sink; export let redact = false;</script>`;
check('the detector sees a required prop', requiredProps(scriptOf(child)).join() === 'sink');
check('the detector ignores one with a default', !requiredProps(scriptOf(child)).includes('redact'));
const good = usages('<TemplateEditor bind:sink={editingSink} />', 'TemplateEditor');
const bad = usages('<TemplateEditor redact={x} />', 'TemplateEditor');
check('the detector accepts a bound prop', passes(good[0], 'sink'));
check('the detector rejects the omission it was written for', !passes(bad[0], 'sink'));
check('the detector accepts the bind: shorthand',
  passes(usages('<GeneralSettings bind:settings />', 'GeneralSettings')[0], 'settings'));
check('the detector accepts the {x} shorthand',
  passes(usages('<TreeNode {searchTerm} />', 'TreeNode')[0], 'searchTerm'));
check('the detector is not fooled by a > inside an attribute',
  passes(usages('<Row on:click={() => go()} item={x} />', 'Row')[0], 'item'));
check('a prefix is not mistaken for the component',
  usages('<TreeNodeLabel item={x} />', 'TreeNode').length === 0);

process.exit(failures ? 1 : 0);
