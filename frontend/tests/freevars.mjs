/**
 * Names a module uses and never binds, found in the code Svelte generates.
 *
 * Svelte 3 answered this question itself: `{#if redact}` for a variable nothing
 * declared produced a `missing-declaration` warning. Svelte 5 removed it — no
 * compile option brings it back, verified against `dev`, `runes`,
 * `generate: 'server'` and every combination of them — and the reference simply
 * becomes a bare global read in the output:
 *
 *     if (inconnu) $$render(consequent);
 *
 * which throws ReferenceError the first time that branch is evaluated. That is
 * the failure the check exists for: it shipped once in TemplateEditor, in a
 * `{#if}` for a prop that had been renamed, and the warning sat unread in build
 * output for weeks.
 *
 * So the question is asked of the GENERATED code instead of the template. That
 * is the easier half of the problem by a wide margin — machine-produced code is
 * regular, where a template AST means handling each-block locals, await blocks,
 * const tags, snippet parameters and every other binding form Svelte invents.
 *
 * ONE DELIBERATE APPROXIMATION. This collects every name bound ANYWHERE in the
 * module rather than resolving scopes, so a name declared in one function and
 * used in another is accepted. That is the right trade here: an undeclared
 * template reference is declared in NO scope, which this catches exactly, and
 * the approximation can only ever cause a false NEGATIVE — never a false alarm
 * that would train someone to ignore the test.
 */

const GLOBALS = new Set([
  // Values and constructors a module may legitimately reach for.
  'globalThis', 'undefined', 'NaN', 'Infinity',
  'Object', 'Array', 'Function', 'Boolean', 'Number', 'String', 'Symbol',
  'BigInt', 'Math', 'JSON', 'Date', 'RegExp', 'Error', 'TypeError',
  'RangeError', 'SyntaxError', 'ReferenceError', 'EvalError', 'URIError',
  'Map', 'Set', 'WeakMap', 'WeakSet', 'Promise', 'Proxy', 'Reflect',
  'ArrayBuffer', 'DataView', 'Int8Array', 'Uint8Array', 'Uint8ClampedArray',
  'Int16Array', 'Uint16Array', 'Int32Array', 'Uint32Array', 'Float32Array',
  'Float64Array', 'BigInt64Array', 'BigUint64Array',
  'parseInt', 'parseFloat', 'isNaN', 'isFinite', 'encodeURI', 'decodeURI',
  'encodeURIComponent', 'decodeURIComponent', 'structuredClone', 'queueMicrotask',

  // The browser, which is where these components run.
  'window', 'document', 'navigator', 'location', 'history', 'screen',
  'console', 'localStorage', 'sessionStorage', 'indexedDB', 'caches',
  'fetch', 'Headers', 'Request', 'Response', 'FormData', 'URL', 'URLSearchParams',
  'Blob', 'File', 'FileReader', 'AbortController', 'AbortSignal',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval',
  'requestAnimationFrame', 'cancelAnimationFrame', 'requestIdleCallback',
  'getComputedStyle', 'matchMedia', 'CSS', 'IntersectionObserver', 'ResizeObserver',
  'MutationObserver', 'PerformanceObserver', 'performance', 'crypto',
  'Event', 'CustomEvent', 'KeyboardEvent', 'MouseEvent', 'PointerEvent',
  'DragEvent', 'ClipboardEvent', 'WebSocket', 'Worker', 'Image', 'Audio',
  'Node', 'Element', 'HTMLElement', 'HTMLInputElement', 'HTMLCanvasElement',
  'HTMLDialogElement', 'SVGElement', 'DocumentFragment', 'Range', 'Selection',
  'alert', 'confirm', 'prompt', 'atob', 'btoa', 'TextEncoder', 'TextDecoder',
]);

/** Every name introduced by a binding pattern. */
function boundBy(node, into) {
  if (!node || typeof node !== 'object') return;
  switch (node.type) {
    case 'Identifier':
      into.add(node.name);
      break;
    case 'ObjectPattern':
      for (const p of node.properties) {
        boundBy(p.type === 'RestElement' ? p.argument : p.value, into);
      }
      break;
    case 'ArrayPattern':
      for (const e of node.elements) boundBy(e, into);
      break;
    case 'AssignmentPattern':
      boundBy(node.left, into);
      break;
    case 'RestElement':
      boundBy(node.argument, into);
      break;
    default:
      break;
  }
}

/** Walk every node once, in one pass, collecting both sets. */
function collect(ast) {
  const bound = new Set();
  const used = new Set();

  // Positions where an Identifier is a NAME rather than a reference: an object
  // key, a non-computed member property, a label. Missing one of these is how a
  // checker like this starts reporting `length` as undeclared.
  const skip = new Set();

  const visit = (node) => {
    if (!node || typeof node !== 'object') return;

    if (Array.isArray(node)) {
      for (const child of node) visit(child);
      return;
    }
    if (typeof node.type !== 'string') return;

    switch (node.type) {
      case 'ImportDeclaration':
        for (const s of node.specifiers) bound.add(s.local.name);
        return; // the imported names are not references
      case 'VariableDeclarator':
        boundBy(node.id, bound);
        break;
      case 'FunctionDeclaration':
      case 'FunctionExpression':
      case 'ArrowFunctionExpression':
        if (node.id) bound.add(node.id.name);
        for (const p of node.params) boundBy(p, bound);
        break;
      case 'ClassDeclaration':
      case 'ClassExpression':
        if (node.id) bound.add(node.id.name);
        break;
      case 'CatchClause':
        if (node.param) boundBy(node.param, bound);
        break;
      case 'MemberExpression':
        if (!node.computed && node.property) skip.add(node.property);
        break;
      case 'Property':
      case 'MethodDefinition':
      case 'PropertyDefinition':
        if (!node.computed && node.key) skip.add(node.key);
        break;
      case 'LabeledStatement':
        skip.add(node.label);
        break;
      case 'BreakStatement':
      case 'ContinueStatement':
        if (node.label) skip.add(node.label);
        break;
      case 'ExportSpecifier':
        skip.add(node.exported);
        break;
      case 'Identifier':
        if (!skip.has(node)) used.add(node.name);
        return;
      default:
        break;
    }

    for (const key of Object.keys(node)) {
      if (key === 'type' || key === 'start' || key === 'end' || key === 'loc') continue;
      visit(node[key]);
    }
  };

  visit(ast);
  return { bound, used };
}

/**
 * @param {object} ast  an ESTree Program, from acorn
 * @param {Set<string>} [extraGlobals]
 * @returns {string[]} names used and never bound, sorted
 */
export function freeIdentifiers(ast, extraGlobals = new Set()) {
  const { bound, used } = collect(ast);
  return [...used]
    .filter((n) => !bound.has(n) && !GLOBALS.has(n) && !extraGlobals.has(n))
    .sort();
}
