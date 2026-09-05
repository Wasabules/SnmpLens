/**
 * Rendered expressions that depend on state they do not name.
 *
 * `{getGroupTargetCount(group.id)}` where that function reads `targets` renders
 * once and then never again, because nothing links the expression to the
 * variable. Svelte 3 hid it: it recomputed every `{@const}` and re-evaluated
 * every block expression on any component update, so a function reaching for
 * instance state happened to be re-run often enough to look correct. SVELTE 5
 * TRACKS WHAT THE EXPRESSION ACTUALLY READS, and a read that happens inside a
 * callee is not a read of the expression.
 *
 * EIGHT of these were in the tree when the migration landed, and one was
 * visible: `ifTable` rendered a column headed "Index" holding the raw instance,
 * which is exactly what decoding an INDEX exists to replace. The rest are all "a
 * value that quietly stops updating", which nobody reports as a bug because the
 * first render is right — a group tab's count, a sink's name in the delivery
 * log, the validation errors under the template editor, an entry's MIB name in
 * the history, and the enum formatter that decides between `6` and
 * `ethernetCsmacd(6)` in three places.
 *
 * WHAT IS AND IS NOT REPORTED. Only functions called from a RENDERED expression:
 * the contents of the markup's braces, minus the value of an `on:` attribute and
 * minus anything containing an arrow. An event handler reading and writing
 * instance state is how a handler works and has nothing to do with this. That
 * distinction is what takes the count from 82 to 8.
 *
 * The check is deliberately shallow — it does not follow a call from one
 * function into another. One level is where the whole class lived, and a
 * transitive walk buys reach at the cost of the false positives that get a test
 * ignored.
 */
import { parse } from 'acorn';

/** Every name a binding pattern introduces. */
function boundBy(node, into) {
  if (!node || typeof node !== 'object') return;
  switch (node.type) {
    case 'Identifier': into.add(node.name); break;
    case 'ObjectPattern':
      for (const p of node.properties) boundBy(p.type === 'RestElement' ? p.argument : p.value, into);
      break;
    case 'ArrayPattern': for (const e of node.elements) boundBy(e, into); break;
    case 'AssignmentPattern': boundBy(node.left, into); break;
    case 'RestElement': boundBy(node.argument, into); break;
    default: break;
  }
}

/**
 * The brace expressions the markup RENDERS.
 *
 * Written with plain string scanning rather than a parser because the markup is
 * not JavaScript and Svelte's own AST would mean depending on compiler
 * internals. Braces are matched by depth so a nested object literal inside an
 * expression does not end it early.
 */
function renderedExpressions(markup) {
  const out = [];
  let i = 0;
  while (i < markup.length) {
    if (markup[i] !== '{') { i++; continue; }
    let depth = 0;
    let j = i;
    while (j < markup.length) {
      if (markup[j] === '{') depth++;
      else if (markup[j] === '}') { depth--; if (depth === 0) break; }
      j++;
    }
    if (j >= markup.length) break;
    const expr = markup.slice(i + 1, j);
    // `on:click={...}` and `on:mousedown|stopPropagation={...}`: an event
    // handler, not something rendered.
    const before = markup.slice(0, i).trimEnd();
    const isHandler = before.endsWith('=') && (() => {
      const attr = before.slice(0, -1).split(/\s/).pop() || '';
      return attr.startsWith('on:') || attr.startsWith('bind:');
    })();
    // An arrow means the body runs on an event, not during rendering.
    if (!isHandler && !expr.includes('=>')) out.push(expr);
    i = j + 1;
  }
  return out;
}

/**
 * @param {string} source a .svelte file
 * @returns {Array<{fn: string, reads: string[]}>}
 */
export function unnamedStateReads(source) {
  // Case-insensitive, and the markup is taken from the SAME match rather than a
  // second search for `</script>`. Two searches for the same thing are two
  // chances to disagree, and the case blindness was a real gap even here: a
  // component written `<SCRIPT>` would have had its whole script counted as
  // markup, so every name in it would look like a rendered reference. `\s*`
  // before the closing bracket for the same reason: `</script >` is legal HTML.
  const m = source.match(/<script[^>]*>([\s\S]*?)<\/script\s*>/i);
  if (!m) return [];
  const script = m[1];
  const markup = source.slice(m.index + m[0].length);

  let ast;
  try {
    ast = parse(script, { ecmaVersion: 'latest', sourceType: 'module' });
  } catch {
    return []; // tests/compile.test.mjs is what fails on a file that will not parse
  }

  // A top-level `let` is the component's mutable state. `const` is not: it can
  // never take a new value, so an expression reading one through a callee has
  // nothing to miss.
  const state = new Set();
  for (const raw of ast.body) {
    // `export let x` is an ExportNamedDeclaration WRAPPING the declaration.
    // Missing that unwrap is not a detail: props are the state most likely to
    // arrive after the first render, and skipping them hid the enum formatter
    // in ResultsDisplay, which reads the `oidInfoCache` prop and is called from
    // three places in the markup.
    const n = raw.type === 'ExportNamedDeclaration' && raw.declaration ? raw.declaration : raw;
    if (n.type === 'VariableDeclaration' && n.kind === 'let') {
      for (const d of n.declarations) boundBy(d.id, state);
    }
  }

  // Functions the markup could call: declarations, and consts holding a function.
  const fns = new Map();
  for (const raw of ast.body) {
    const n = raw.type === 'ExportNamedDeclaration' && raw.declaration ? raw.declaration : raw;
    if (n.type === 'FunctionDeclaration' && n.id) fns.set(n.id.name, n);
    if (n.type === 'VariableDeclaration') {
      for (const d of n.declarations) {
        if (d.id.type !== 'Identifier' || !d.init) continue;
        if (d.init.type === 'ArrowFunctionExpression' || d.init.type === 'FunctionExpression') {
          fns.set(d.id.name, d.init);
        }
      }
    }
  }

  const rendered = renderedExpressions(markup);
  const found = [];

  for (const [name, fn] of fns) {
    if (!rendered.some((e) => e.includes(name + '('))) continue;

    const scoped = new Set();
    for (const p of fn.params) boundBy(p, scoped);

    const reads = new Set();
    const visit = (node) => {
      if (!node || typeof node !== 'object') return;
      if (Array.isArray(node)) { for (const c of node) visit(c); return; }
      if (typeof node.type !== 'string') return;
      if (node.type === 'VariableDeclarator') boundBy(node.id, scoped);
      // `a.b` reads `a`, not `b`; `{ k: v }` reads `v`, not `k`.
      if (node.type === 'MemberExpression' && !node.computed) { visit(node.object); return; }
      if (node.type === 'Property' && !node.computed) { visit(node.value); return; }
      if (node.type === 'Identifier') { reads.add(node.name); return; }
      for (const k of Object.keys(node)) {
        if (k === 'type' || k === 'start' || k === 'end' || k === 'loc') continue;
        visit(node[k]);
      }
    };
    visit(fn.body);

    const leaked = [...reads].filter((r) => state.has(r) && !scoped.has(r)).sort();
    if (leaked.length) found.push({ fn: name, reads: leaked });
  }

  return found;
}
