// The dependency graph, read as a tree.
//
// Go answers with a flat list of modules and their edges, which is the right
// thing to send: it is one row per module however tangled the graph is. What
// somebody looking at a folder of vendor MIBs wants is the other shape — start
// at the file they actually opened and walk down through what it needs.
//
// Pure, and it stays pure: nothing here calls the bridge. The scan behind it
// reads file heads as text, so it is cheap to refresh, and the arranging is
// arithmetic over what came back.

/** A module named in an IMPORTS clause with no file behind it. */
export const MISSING = 'missing';
/** Present on disk but not currently loaded — usually switched off, not broken. */
export const NOT_LOADED = 'notloaded';
export const LOADED = 'loaded';

export function statusOf(node) {
  if (!node || !node.present) return MISSING;
  return node.loaded ? LOADED : NOT_LOADED;
}

/**
 * The roots of the forest: modules nothing else imports.
 *
 * Those are the files somebody actually opened — a vendor's POE MIB rather than
 * SNMPv2-SMI, which everything imports and nothing depends on the other way.
 * A graph where everything is imported by something (a cycle among the top
 * modules) would have no root at all, so it falls back to every present module,
 * which is a flat list rather than an empty screen.
 */
export function rootsOf(nodes) {
  const list = Array.isArray(nodes) ? nodes : [];
  const roots = list.filter((n) => n.present && (n.importedBy || []).length === 0);
  if (roots.length > 0) return roots;
  return list.filter((n) => n.present);
}

/**
 * One root expanded into a tree.
 *
 * Two rules keep it finite, and both are visible in the result rather than
 * silently applied:
 *
 * - a module already on the PATH is a cycle, marked and not descended into.
 *   gosmi cannot survive one — it recurses until the stack ends — so a MIB
 *   directory containing one is a real thing that has to render.
 * - a module already expanded somewhere else in this tree is shown as a leaf
 *   marked `repeated`. Twelve files importing SNMPv2-SMI would otherwise
 *   redraw its whole subtree twelve times, which is the same information at
 *   twelve times the height.
 */
export function treeFrom(nodes, rootModule, { maxDepth = 12 } = {}) {
  const byModule = new Map((Array.isArray(nodes) ? nodes : []).map((n) => [n.module, n]));
  const expanded = new Set();

  const build = (module, path, depth) => {
    const node = byModule.get(module) || { module, present: false, imports: [], importedBy: [] };
    const item = {
      module,
      file: node.file || '',
      status: statusOf(node),
      depth,
      cycle: path.has(module),
      repeated: !path.has(module) && expanded.has(module),
      children: [],
    };
    if (item.cycle || item.repeated || depth >= maxDepth) return item;

    expanded.add(module);
    const next = new Set(path);
    next.add(module);
    for (const dep of node.imports || []) {
      item.children.push(build(dep, next, depth + 1));
    }
    return item;
  };

  return build(rootModule, new Set(), 0);
}

/**
 * Flattened for rendering, because a recursive component in Svelte is a
 * component that has to import itself and this is a list with an indent.
 */
export function flattenTree(item, out = []) {
  if (!item) return out;
  out.push(item);
  for (const child of item.children || []) flattenTree(child, out);
  return out;
}

/**
 * What the whole graph amounts to, for the one line above the tree.
 */
export function graphSummary(nodes) {
  const list = Array.isArray(nodes) ? nodes : [];
  let missing = 0;
  let notLoaded = 0;
  for (const n of list) {
    const s = statusOf(n);
    if (s === MISSING) missing++;
    else if (s === NOT_LOADED) notLoaded++;
  }
  return { modules: list.length, missing, notLoaded, roots: rootsOf(list).length };
}
