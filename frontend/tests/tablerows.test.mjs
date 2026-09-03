// Turning a walk into a conceptual table.
//
// This is the code that decides which cell belongs to which row. A mistake
// here produces a table that looks correct and is not, which is worse than an
// error, so the cases below are the ones that produce a plausible wrong table.
import {
  stripDot, matchColumn, buildTableData, withDecodedIndexes, sortRows,
  buildRowVarbinds, buildDestroyVarbinds,
} from '../src/operations/tableRows.js';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

// ifTable, two interfaces, three columns.
const IF_COLS = [
  { name: 'ifIndex', oid: '1.3.6.1.2.1.2.2.1.1', syntax: 'InterfaceIndex' },
  { name: 'ifDescr', oid: '1.3.6.1.2.1.2.2.1.2', syntax: 'DisplayString' },
  { name: 'ifSpeed', oid: '1.3.6.1.2.1.2.2.1.5', syntax: 'Gauge32' },
];
const IF_WALK = [
  { oid: '.1.3.6.1.2.1.2.2.1.1.1', value: 1, type: 'Integer' },
  { oid: '.1.3.6.1.2.1.2.2.1.1.10', value: 10, type: 'Integer' },
  { oid: '.1.3.6.1.2.1.2.2.1.2.1', value: 'eth0', type: 'OctetString' },
  { oid: '.1.3.6.1.2.1.2.2.1.2.10', value: 'eth9', type: 'OctetString' },
  { oid: '.1.3.6.1.2.1.2.2.1.5.1', value: 1000000000, type: 'Gauge32' },
];

// --- matching ---
check('a leading dot does not stop a match', !!matchColumn('.1.3.6.1.2.1.2.2.1.1.1', IF_COLS));
check('stripDot leaves a bare OID alone', stripDot('1.2.3') === '1.2.3');
check('an unrelated OID matches nothing', matchColumn('1.3.6.1.2.1.1.1.0', IF_COLS) === null);

// A column OID must not match a LONGER sibling: ifIndex (…1.1) must not claim
// a result under a hypothetical …1.11, which shares its text prefix.
check('a column does not claim its longer sibling',
  matchColumn('1.3.6.1.2.1.2.2.1.11.1', IF_COLS) === null,
  JSON.stringify(matchColumn('1.3.6.1.2.1.2.2.1.11.1', IF_COLS)));

// --- pivoting ---
{
  const t = buildTableData(IF_WALK, IF_COLS);
  check('one row per instance', t.rows.length === 2, `${t.rows.length}`);
  const byIndex = Object.fromEntries(t.rows.map((r) => [r.index, r]));
  check('cells land in the right row', byIndex['1'].cells['1.3.6.1.2.1.2.2.1.2'].value === 'eth0');
  check('a missing cell is simply absent', byIndex['10'].cells['1.3.6.1.2.1.2.2.1.5'] === undefined);
  check('the full OID is kept for each cell',
    byIndex['10'].cells['1.3.6.1.2.1.2.2.1.2'].fullOid === '.1.3.6.1.2.1.2.2.1.2.10');
  check('an empty walk yields no rows', buildTableData([], IF_COLS).rows.length === 0);
  check('a null walk does not throw', buildTableData(null, IF_COLS).rows.length === 0);
}

// --- decoded indexes ---
{
  const t = buildTableData(IF_WALK, IF_COLS);
  const decoded = [
    { raw: '1', parts: [{ name: 'ifIndex', display: '1', sort: 1, numeric: true }] },
    { raw: '10', parts: [{ name: 'ifIndex', display: '10', sort: 10, numeric: true }] },
  ];
  const d = withDecodedIndexes(t, decoded, [{ name: 'ifIndex', syntax: 'InterfaceIndex' }]);
  check('the index becomes named columns', d.indexColumns?.[0]?.name === 'ifIndex');
  check('each row carries its decoded parts',
    d.rows.every((r) => r.indexParts?.length === 1));
}

// The four-part case this was built for.
{
  const cols = [{ name: 'tcpConnState', oid: '1.3.6.1.2.1.6.13.1.1', syntax: 'INTEGER' }];
  const inst = '10.0.0.5.161.192.168.1.9.50000';
  const t = buildTableData([{ oid: `.1.3.6.1.2.1.6.13.1.1.${inst}`, value: 5 }], cols);
  const decoded = [{
    raw: inst,
    parts: [
      { name: 'tcpConnLocalAddress', display: '10.0.0.5' },
      { name: 'tcpConnLocalPort', display: '161', sort: 161, numeric: true },
      { name: 'tcpConnRemAddress', display: '192.168.1.9' },
      { name: 'tcpConnRemPort', display: '50000', sort: 50000, numeric: true },
    ],
  }];
  const d = withDecodedIndexes(t, decoded, [
    { name: 'tcpConnLocalAddress' }, { name: 'tcpConnLocalPort' },
    { name: 'tcpConnRemAddress' }, { name: 'tcpConnRemPort' },
  ]);
  check('a four-part index becomes four columns', d.indexColumns?.length === 4);
  check('the parts stay in order',
    d.rows[0].indexParts.map((p) => p.display).join('|') === '10.0.0.5|161|192.168.1.9|50000');
}

// Without a MIB there is nothing to decode, and the table must still render.
{
  const t = buildTableData(IF_WALK, IF_COLS);
  const d = withDecodedIndexes(t, [{ raw: '1', error: 'not in any loaded MIB' }], []);
  check('an undecodable table falls back to the raw index', d.indexColumns === null);
  check('the rows survive', d.rows.length === 2);
  check('the raw instance is still there', d.rows.every((r) => typeof r.index === 'string'));
}

// --- sorting ---
{
  const t = buildTableData(IF_WALK, IF_COLS);
  const asc = sortRows(t.rows, '__index', true).map((r) => r.index);
  check('the raw index sorts numerically, not as text',
    asc.join(',') === '1,10', asc.join(','));

  const desc = sortRows(t.rows, '__index', false).map((r) => r.index);
  check('descending reverses it', desc.join(',') === '10,1');

  const byDescr = sortRows(t.rows, '1.3.6.1.2.1.2.2.1.2', true).map((r) => r.cells['1.3.6.1.2.1.2.2.1.2'].value);
  check('sorting by a column works', byDescr.join(',') === 'eth0,eth9');
}

// Sorting by a decoded part is the point of decoding: ports as numbers.
{
  const rows = [
    { index: 'a', cells: {}, indexParts: [{ display: '9', sort: 9, numeric: true }] },
    { index: 'b', cells: {}, indexParts: [{ display: '10', sort: 10, numeric: true }] },
  ];
  const sorted = sortRows(rows, '__index:0', true).map((r) => r.indexParts[0].display);
  check('a numeric index part sorts as a number', sorted.join(',') === '9,10', sorted.join(','));

  const addrs = [
    { index: 'a', cells: {}, indexParts: [{ display: '10.0.0.2' }] },
    { index: 'b', cells: {}, indexParts: [{ display: '10.0.0.10' }] },
  ];
  const s2 = sortRows(addrs, '__index:0', true).map((r) => r.indexParts[0].display);
  check('a non-numeric part sorts naturally', s2.join(',') === '10.0.0.2,10.0.0.10', s2.join(','));
}

// --- row creation ---
const TABLE = {
  oid: '1.3.6.1.2.1.99.1',
  rowStatusOid: '1.3.6.1.2.1.99.1.1.4',
  columns: [
    { name: 'k', oid: '1.3.6.1.2.1.99.1.1.1', syntax: 'Integer32', isIndex: true },
    { name: 'name', oid: '1.3.6.1.2.1.99.1.1.2', syntax: 'DisplayString' },
    { name: 'size', oid: '1.3.6.1.2.1.99.1.1.3', syntax: 'Unsigned32' },
    { name: 'status', oid: '1.3.6.1.2.1.99.1.1.4', syntax: 'RowStatus' },
  ],
};
{
  const vars = buildRowVarbinds(TABLE, '7', {
    '1.3.6.1.2.1.99.1.1.2': 'thing',
    '1.3.6.1.2.1.99.1.1.3': '3',
  }, 4);

  check('every varbind is instance-qualified',
    vars.every((v) => v.oid.endsWith('.7')), JSON.stringify(vars.map((v) => v.oid)));

  // Writing the index column as well creates a row and then addresses another.
  check('the index column is not written',
    !vars.some((v) => v.oid.startsWith('1.3.6.1.2.1.99.1.1.1')));

  check('RowStatus goes last',
    vars[vars.length - 1].oid === '1.3.6.1.2.1.99.1.1.4.7' &&
    vars[vars.length - 1].value === '4');

  check('an empty value is skipped rather than written as empty',
    buildRowVarbinds(TABLE, '7', { '1.3.6.1.2.1.99.1.1.2': '' }, 4).length === 1);

  check('the type travels with the value',
    vars.find((v) => v.oid.startsWith('1.3.6.1.2.1.99.1.1.2')).type === 'DisplayString');
}
{
  const vars = buildDestroyVarbinds(TABLE, '7');
  check('destroy is one varbind', vars.length === 1);
  check('destroy is RowStatus = 6',
    vars[0].oid === '1.3.6.1.2.1.99.1.1.4.7' && vars[0].value === '6');
}

process.exit(failures ? 1 : 0);
