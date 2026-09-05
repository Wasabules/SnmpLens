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

// Iterator methods that call their callback with more than the element, and how
// many arguments they actually pass. `sort` is the odd one: two elements.
const ITERATORS = {
  map: 3, filter: 3, forEach: 3, find: 3, findIndex: 3, findLast: 3,
  some: 3, every: 3, flatMap: 3, sort: 2,
};

/**
 * Functions handed point-free to an iterator that will over-supply them.
 *
 * `list.map(sinkName)` reads as "the name of each", and is not: map calls back
 * with (element, index, array), so a two-parameter function receives the INDEX
 * as its second argument. `['1','2','3'].map(parseInt)` is the canonical
 * version; ours was `(r.sinkIds || []).map(sinkName)` after `sinkName` grew a
 * second parameter, which put a number where a Sink[] was expected and threw
 * `all.find is not a function` — the routing rules list stopped rendering, and
 * the site's screenshot published the empty state.
 *
 * Nothing else here could see it. The reactivity check is satisfied, correctly:
 * the function takes what it needs. The compiler is satisfied. It is an arity
 * mismatch, and a type checker is the other tool that would catch it.
 *
 * @param {string} source a .svelte file
 * @returns {Array<{fn: string, iterator: string, arity: number}>}
 */
export function overSuppliedCallbacks(source) {
  const parts = split(source);
  if (!parts) return [];
  const { ast, markup } = parts;

  const arity = new Map();
  for (const raw of ast.body) {
    const n = raw.type === 'ExportNamedDeclaration' && raw.declaration ? raw.declaration : raw;
    if (n.type === 'FunctionDeclaration' && n.id) arity.set(n.id.name, n.params.length);
    if (n.type === 'VariableDeclaration') {
      for (const d of n.declarations) {
        if (d.id.type !== 'Identifier' || !d.init) continue;
        if (d.init.type === 'ArrowFunctionExpression' || d.init.type === 'FunctionExpression') {
          arity.set(d.id.name, d.init.params.length);
        }
      }
    }
  }

  const found = [];
  // The whole file, not only rendered expressions: the trap is the same in a
  // handler, it just fails later.
  const text = markup + ast_source(source);
  for (const [method, supplied] of Object.entries(ITERATORS)) {
    const re = new RegExp('\\.' + method + '\\(\\s*([A-Za-z_$][A-Za-z0-9_$]*)\\s*\\)', 'g');
    for (const m of text.matchAll(re)) {
      const name = m[1];
      const n = arity.get(name);
      if (n !== undefined && n > 1 && n <= supplied) {
        found.push({ fn: name, iterator: method, arity: n });
      }
    }
  }
  return found;
}

/** The raw <script> text, for the scan above. */
function ast_source(source) {
  const m = source.match(/<script[^>]*>([\s\S]*?)<\/script(?:\s[^>]*)?>/i);
  return m ? m[1] : '';
}

/** Shared parse: the script AST and the markup after it. */
function split(source) {
  const m = source.match(/<script[^>]*>([\s\S]*?)<\/script(?:\s[^>]*)?>/i);
  if (!m) return null;
  try {
    return {
      ast: parse(m[1], { ecmaVersion: 'latest', sourceType: 'module' }),
      markup: source.slice(m.index + m[0].length),
    };
  } catch {
    return null;
  }
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
  // markup, so every name in it would look like a rendered reference. The end
  // tag is the real grammar rather than the literal seven characters —
  // `</script` then optional whitespace-and-anything then `>` — which matches
  // `</script >` and what a browser forgives, `</script foo>`, while still
  // refusing `</scriptfoo>`.
  const m = source.match(/<script[^>]*>([\s\S]*?)<\/script(?:\s[^>]*)?>/i);
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
    // The name, not `name(`. A function handed to `.map()` by reference —
    // `{list.map(sinkName)}` — is called from the markup exactly as much as one
    // written with parentheses, and it was invisible to the version that looked
    // for the call. That is not hypothetical: this check was added to stop a
    // rendered expression depending on state it does not name, and the very
    // change that satisfied it at one call site left the point-free one behind,
    // where `.map` then passed the ARRAY INDEX as the argument that had just
    // been added. The routing rules list stopped rendering.
    // An expression that is EXACTLY the name hands the function somewhere —
    // `onNodeClick={handleNodeClick}`, the Svelte 5 spelling of an event
    // handler — and is exempt for the same reason `on:click` is. Inside a
    // larger expression the function's RESULT is what gets rendered, and that
    // is the case this test is about.
    const used = new RegExp('(^|[^A-Za-z0-9_$.])' + name + '($|[^A-Za-z0-9_$])');
    if (!rendered.some((e) => e.trim() !== name && used.test(e))) continue;

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
