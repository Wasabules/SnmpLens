/**
 * Turning a walk into a conceptual table.
 *
 * Extracted from the panel so it can be run without a browser: this is the
 * code that decides which cell belongs to which row, and getting it wrong
 * shows a table that looks right and isn't.
 *
 * The instance — the part of a result's OID after its column — is the row's
 * identity. It used to be kept as that raw string, which is why a four-part
 * INDEX like tcpConnTable's rendered as one opaque
 * "10.0.0.5.161.192.168.1.9.50000", a name-keyed table showed decimal bytes,
 * and sorting by index put 10 before 9. Splitting it needs the MIB, so the
 * decoding happens in Go (pkg/mib) and lands here as `decoded`.
 */

/** OIDs come back from gosnmp with a leading dot; MIB OIDs do not. */
export function stripDot(oid) {
  return oid && oid.charAt(0) === '.' ? oid.slice(1) : oid;
}

/**
 * Match one walk result to its column.
 * @returns {{col: object, instance: string}|null}
 */
export function matchColumn(oid, columnDefs) {
  const itemOid = stripDot(oid);
  for (const col of columnDefs) {
    const colOid = stripDot(col.oid);
    if (itemOid.startsWith(colOid + '.')) {
      return { col, instance: itemOid.substring(colOid.length + 1) };
    }
  }
  return null;
}

/**
 * Pivot walk results into rows keyed by their instance.
 *
 * @param {Array} walkResults
 * @param {Array} columnDefs
 * @returns {{columns: Array, rows: Array}}
 */
export function buildTableData(walkResults, columnDefs) {
  const columns = columnDefs.map((col) => ({
    name: col.name,
    oid: col.oid,
    syntax: col.syntax || '',
  }));

  const rowMap = new Map();
  for (const item of walkResults || []) {
    const hit = matchColumn(item.oid, columnDefs);
    if (!hit) continue;
    if (!rowMap.has(hit.instance)) rowMap.set(hit.instance, {});
    rowMap.get(hit.instance)[hit.col.oid] = {
      value: item.value,
      type: item.type,
      fullOid: item.oid,
    };
  }

  const rows = [...rowMap.entries()].map(([index, cells]) => ({ index, cells }));
  return { columns, rows };
}

/**
 * Attach decoded INDEX values to rows, keyed by instance.
 *
 * Falls back to the raw instance for any row the decoder could not split, so a
 * vendor table with no MIB loaded still renders — with one index column named
 * "Index", exactly as before.
 *
 * @param {{columns: Array, rows: Array}} table
 * @param {Array} decoded results from MibDecodeIndexes, in row order
 * @param {Array} indexParts the table's INDEX clause, from MibTable
 */
export function withDecodedIndexes(table, decoded, indexParts) {
  const byRaw = new Map();
  for (const d of decoded || []) byRaw.set(d.raw, d);

  const usable =
    Array.isArray(indexParts) &&
    indexParts.length > 0 &&
    (decoded || []).some((d) => !d.error && (d.parts || []).length > 0);

  const rows = table.rows.map((row) => {
    const d = byRaw.get(row.index);
    if (!usable || !d || d.error || !(d.parts || []).length) {
      return { ...row, indexParts: null, indexError: d?.error || '' };
    }
    return { ...row, indexParts: d.parts, indexError: '' };
  });

  return {
    ...table,
    rows,
    indexColumns: usable ? indexParts.map((p) => ({ name: p.name, syntax: p.syntax })) : null,
  };
}

/**
 * Sort rows by an index part, a column, or the raw instance.
 *
 * `sortCol` is '__index' for the whole instance, `__index:<n>` for one decoded
 * part, or a column OID.
 */
export function sortRows(rows, sortCol, ascending = true) {
  if (!sortCol) return rows;
  const out = [...rows];

  const key = (row) => {
    if (sortCol === '__index') return { text: row.index, num: partNumber(row, -1) };
    if (sortCol.startsWith('__index:')) {
      const i = Number(sortCol.slice('__index:'.length));
      const part = row.indexParts?.[i];
      return { text: part?.display ?? '', num: part?.numeric ? part.sort : NaN };
    }
    const v = row.cells[sortCol]?.value;
    return { text: v ?? '', num: Number(v) };
  };

  out.sort((a, b) => {
    const ka = key(a);
    const kb = key(b);
    let cmp;
    if (!Number.isNaN(ka.num) && !Number.isNaN(kb.num)) {
      cmp = ka.num - kb.num;
    } else {
      // Numeric-aware for the raw instance too: "10" after "9", which plain
      // string comparison gets backwards on every table.
      cmp = String(ka.text).localeCompare(String(kb.text), undefined, { numeric: true });
    }
    return ascending ? cmp : -cmp;
  });
  return out;
}

/** The sortable number for a whole instance: only when it is a single number. */
function partNumber(row, _i) {
  const n = Number(row.index);
  return Number.isNaN(n) ? NaN : n;
}

/**
 * Build the varbinds that create a conceptual row.
 *
 * RowStatus goes LAST. RFC 2579 lets an agent act on createAndGo the moment it
 * sees it, and a SET's varbinds are processed as one request but an agent that
 * validates in order will reject a row whose columns it has not read yet.
 *
 * @param {object} tableInfo from MibTable
 * @param {string} instance the encoded index, from MibEncodeIndex
 * @param {Object<string,string>} values column OID -> value
 * @param {number} status RowStatus value to write
 */
export function buildRowVarbinds(tableInfo, instance, values, status) {
  const vars = [];
  for (const col of tableInfo.columns) {
    // An index column's value is carried by the instance, not by a varbind:
    // writing it as well is how you create a row and then a second one.
    if (col.isIndex) continue;
    if (col.oid === tableInfo.rowStatusOid) continue;
    const v = values[col.oid];
    if (v === undefined || v === '') continue;
    vars.push({ oid: `${col.oid}.${instance}`, value: String(v), type: col.syntax || '' });
  }
  vars.push({
    oid: `${tableInfo.rowStatusOid}.${instance}`,
    value: String(status),
    type: 'RowStatus',
  });
  return vars;
}

/** The single varbind that destroys a row. */
export function buildDestroyVarbinds(tableInfo, instance) {
  return [
    { oid: `${tableInfo.rowStatusOid}.${instance}`, value: '6', type: 'RowStatus' },
  ];
}
