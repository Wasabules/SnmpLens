// Svelte lifecycle functions must be registered while the component is
// initialising — at the top level of <script>, not inside a callback.
//
// Called from inside onMount they throw "Function called outside component
// initialization", and the registration silently never happens. That is how a
// resize listener ended up accumulating on window: the panel is unmounted on
// every tab switch, onDestroy threw, and the listener stayed. Nothing in the
// build sees it, and the console error scrolls past.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const LIFECYCLE = ['onMount', 'onDestroy', 'beforeUpdate', 'afterUpdate', 'setContext', 'getContext'];

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

/** The <script> block, minus the markup and styles. */
function scriptOf(source) {
  const open = source.indexOf('<script');
  if (open < 0) return '';
  const start = source.indexOf('>', open) + 1;
  const end = source.indexOf('</script>', start);
  return end < 0 ? '' : source.slice(start, end);
}

/**
 * Brace depth at each character, ignoring braces inside strings, template
 * literals, comments and regex-looking slashes. Depth 0 is the top level of
 * the script, which is the only place a lifecycle call is valid.
 */
function depthAt(code) {
  const depth = new Array(code.length).fill(0);
  let d = 0;
  let i = 0;
  let inLine = false, inBlock = false, quote = '';

  while (i < code.length) {
    const c = code[i];
    const next = code[i + 1];

    if (inLine) {
      if (c === '\n') inLine = false;
    } else if (inBlock) {
      if (c === '*' && next === '/') { inBlock = false; i++; }
    } else if (quote) {
      if (c === '\\') i++;
      else if (c === quote) quote = '';
    } else if (c === '/' && next === '/') {
      inLine = true; i++;
    } else if (c === '/' && next === '*') {
      inBlock = true; i++;
    } else if (c === '"' || c === "'" || c === '`') {
      quote = c;
    } else if (c === '{' || c === '(') {
      d++;
    } else if (c === '}' || c === ')') {
      d--;
    }
    depth[i] = d;
    i++;
  }
  return depth;
}

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const files = svelteFiles(root);
check('there are components to check', files.length > 0, `${files.length} files`);

const offenders = [];
for (const file of files) {
  const source = readFileSync(file, 'utf8');
  const code = scriptOf(source);
  if (!code) continue;
  const depth = depthAt(code);

  for (const fn of LIFECYCLE) {
    const re = new RegExp(`\\b${fn}\\s*\\(`, 'g');
    let m;
    while ((m = re.exec(code)) !== null) {
      // Skip the import statement itself.
      const lineStart = code.lastIndexOf('\n', m.index) + 1;
      const line = code.slice(lineStart, code.indexOf('\n', m.index));
      if (line.trimStart().startsWith('import')) continue;
      if (line.includes('//')) continue; // mentioned in a comment

      if (depth[m.index] !== 0) {
        const lineNo = code.slice(0, m.index).split('\n').length;
        offenders.push(`${file.split(/[\\/]/).pop()}: ${fn} at script line ${lineNo} is nested ${depth[m.index]} level(s) deep`);
      }
    }
  }
}

check('every lifecycle call is at the top level of its component',
  offenders.length === 0, offenders.join(' | '));

// Prove the detector works, or a passing run means nothing.
const trap = `
  import { onMount, onDestroy } from 'svelte';
  onDestroy(() => cleanup());
  onMount(async () => {
    onDestroy(() => leak());
  });
`;
const trapDepth = depthAt(trap);
const good = trap.indexOf('onDestroy(() => cleanup())');
const bad = trap.indexOf('onDestroy(() => leak())');
check('the detector accepts a top-level registration', trapDepth[good] === 0);
check('the detector rejects one nested in onMount', trapDepth[bad] > 0, String(trapDepth[bad]));

process.exit(failures ? 1 : 0);
