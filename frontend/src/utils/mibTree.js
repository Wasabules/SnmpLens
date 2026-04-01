/**
 * Recursively find a MIB node by OID in the tree.
 * Strips trailing .0 for scalar comparison.
 * @param {string} oid
 * @param {Array} nodes
 * @returns {object|null}
 */
export function findNodeByOid(oid, nodes) {
  if (!nodes || nodes.length === 0) return null;

  for (const node of nodes) {
    const nodeOid = node.oid;
    const searchOid = oid.endsWith('.0') ? oid.slice(0, -2) : oid;

    if (nodeOid === searchOid || nodeOid === oid) {
      return node;
    }

    if (node.children && node.children.length > 0) {
      const found = findNodeByOid(oid, node.children);
      if (found) return found;
    }
  }
  return null;
}

/**
 * Find MIB name from OID in the tree.
 * @param {string} oid
 * @param {Array} nodes
 * @returns {string|null}
 */
export function findMibNameByOid(oid, nodes) {
  const node = findNodeByOid(oid, nodes);
  return node ? node.name : null;
}

/**
 * Format a value using enum decoding from an OID info object.
 * @param {*} value
 * @param {{ enumValues?: Record<string, number> }|null} info - node or oidInfoCache entry
 * @returns {string}
 */
export function formatValueWithEnum(value, info) {
  if (info && info.enumValues) {
    const numValue = Number(value);
    if (!isNaN(numValue)) {
      for (const [enumName, enumVal] of Object.entries(info.enumValues)) {
        if (enumVal === numValue) return `${enumName}(${numValue})`;
      }
    }
  }
  return typeof value === 'string' ? value : JSON.stringify(value);
}

/**
 * Check if a MIB node is writable.
 * @param {object|null} node
 * @returns {boolean}
 */
export function isNodeWritable(node) {
  if (!node || !node.access) return false;
  const access = node.access.toLowerCase();
  return access.includes('write') || access === 'readwrite' || access === 'read-write';
}

/**
 * Find the nearest Table ancestor for an OID by walking up the MIB tree.
 * Uses parent references set by mibStore.
 * @param {string} oid
 * @param {Array} nodes - Root nodes from mibStore.tree
 * @returns {object|null} Table node or null
 */
export function findTableParentNode(oid, nodes) {
  // Try progressively shorter OID prefixes to find a matching node
  const parts = oid.split('.');
  for (let len = parts.length; len >= 1; len--) {
    const prefix = parts.slice(0, len).join('.');
    const node = findNodeByOid(prefix, nodes);
    if (node) {
      // Walk up parent chain to find Table ancestor
      let current = node;
      while (current) {
        if (current.mibType === 'Table') return current;
        current = current.parent;
      }
    }
  }
  return null;
}
