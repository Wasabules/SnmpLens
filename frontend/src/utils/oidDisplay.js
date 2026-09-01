import { findNodeByOid } from './mibTree';

/**
 * Turn a raw OID into something an operator can read.
 *
 * A monitored OID usually carries an instance index (`…ifInOctets` polled as
 * `1.3.6.1.2.1.2.2.1.10.1`), which matches no node on its own. So we walk up
 * segment by segment to the nearest known node and report the leftover as the
 * index — giving `ifInOctets.1` instead of a wall of digits.
 *
 * Everything comes from the already-loaded MIB tree (name, description,
 * syntax, access and, via parent links, the full path), so this is synchronous
 * and needs no backend round-trip.
 */

// Resolution walks the tree once per candidate prefix, so a deep OID could
// traverse it a dozen times per render. Cache per tree instance: a MIB reload
// hands us a new array, which naturally invalidates the old entries.
const cache = new WeakMap();

function cacheFor(nodes) {
  if (!nodes) return null;
  let byOid = cache.get(nodes);
  if (!byOid) {
    byOid = new Map();
    cache.set(nodes, byOid);
  }
  return byOid;
}

/** Full MIB path of a node, root first. */
export function buildMibPath(node) {
  const parts = [];
  let current = node;
  while (current) {
    parts.unshift(current.name);
    current = current.parent;
  }
  return parts.join(' › ');
}

/**
 * @returns {{node: object|null, name: string, path: string, index: string, oid: string}}
 *          `name` falls back to the raw OID when no MIB defines it.
 */
export function resolveOidDisplay(oid, nodes) {
  const raw = String(oid || '');
  if (!raw) return { node: null, name: '', path: '', index: '', oid: raw };

  const store = cacheFor(nodes);
  if (store && store.has(raw)) return store.get(raw);

  const clean = raw.charAt(0) === '.' ? raw.slice(1) : raw;
  const parts = clean.split('.');
  let result = { node: null, name: clean, path: '', index: '', oid: raw };

  for (let end = parts.length; end > 0; end--) {
    const node = findNodeByOid(parts.slice(0, end).join('.'), nodes);
    if (!node) continue;

    const index = parts.slice(end).join('.');
    // Only an instantiable object (a scalar or a table column) can carry an
    // index. Without this, an unknown OID would keep walking up to a purely
    // structural node and display as "iso.9.9.9.9" — a name that looks
    // resolved but identifies nothing. Fall back to the raw OID instead.
    const instantiable = node.mibType === 'Scalar' || node.mibType === 'Column' || !!node.syntax;
    if (index && !instantiable) break;

    result = {
      node,
      name: node.name + (index ? '.' + index : ''),
      path: buildMibPath(node),
      index,
      oid: raw,
    };
    break;
  }

  if (store) store.set(raw, result);
  return result;
}

/** Short display name only (raw OID when unknown). */
export function oidName(oid, nodes) {
  return resolveOidDisplay(oid, nodes).name;
}

const MAX_DESCRIPTION = 260;

/**
 * Multi-line tooltip: name, raw OID, MIB path, then type/access and the
 * MIB description — the detail you would otherwise have to hunt for in the
 * tree.
 */
export function oidTooltip(oid, nodes) {
  const info = resolveOidDisplay(oid, nodes);
  const lines = [];

  if (info.node) lines.push(info.name);
  lines.push(info.oid);
  if (info.path) lines.push(info.path);

  const meta = [info.node?.syntax, info.node?.mibType, info.node?.access].filter(Boolean);
  if (meta.length) lines.push(meta.join(' · '));

  const description = (info.node?.description || '').trim();
  if (description) {
    lines.push('');
    lines.push(
      description.length > MAX_DESCRIPTION ? description.slice(0, MAX_DESCRIPTION).trimEnd() + '…' : description
    );
  }

  return lines.join('\n');
}
