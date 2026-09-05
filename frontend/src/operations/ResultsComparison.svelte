<script>
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';
  import { formatValueWithEnum as _formatValueWithEnum } from '../utils/mibTree';
  import { escapeCSV, downloadFile } from '../utils/csv';
  import { notificationStore } from '../stores/notifications';
  import { anonMode, anonymizeIp } from '../utils/anonymize';
  import { targetLabels } from '../stores/targetLabels';
  import { displayTarget, targetTitle } from '../utils/targets';

  export let bulkResults = [];
  export let oidInfoCache = {};
  export let mode = 'enhanced'; // 'enhanced' (delta view) | 'legacy' (matrix view)

  let compareSortKey = 'oid'; // 'oid' | 'delta' | 'percent'
  let compareSortAsc = true;

  // `cache` is a parameter rather than a read of the prop: this is called from
  // the markup, and a rendered expression that does not name what it depends on
  // is not re-evaluated when that arrives.
  function formatValueWithEnum(value, oid, snmpType, cache) {
    return _formatValueWithEnum(value, cache[oid], snmpType);
  }

  // ---- Legacy matrix comparison ----
  function buildComparisonData(results) {
    const targets = [];
    const oidSet = new Set();
    const targetData = {};
    for (const res of results) {
      if (res.error || !Array.isArray(res.result?.value)) continue;
      targets.push(res.target);
      targetData[res.target] = {};
      for (const item of res.result.value) {
        oidSet.add(item.oid);
        targetData[res.target][item.oid] = item.value;
      }
    }
    return { targets, oids: [...oidSet].sort(), targetData };
  }

  function valuesDiffer(oid, targets, targetData) {
    const values = targets.map((t) => targetData[t]?.[oid]);
    const first = values[0];
    return values.some((v) => JSON.stringify(v) !== JSON.stringify(first));
  }

  function exportComparisonCSV() {
    const comp = buildComparisonData(bulkResults);
    if (comp.oids.length === 0) return;
    const lines = [['OID', 'Name', ...comp.targets].map(escapeCSV).join(',')];
    for (const oid of comp.oids) {
      const name = oidInfoCache[oid]?.name || '';
      const values = comp.targets.map((t) => {
        const v = comp.targetData[t]?.[oid];
        return v !== undefined ? formatValueWithEnum(v, oid, undefined, oidInfoCache) : '';
      });
      lines.push([oid, name, ...values].map(escapeCSV).join(','));
    }
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
    downloadFile(lines.join('\n'), `snmp-comparison-${timestamp}.csv`, 'text/csv');
    notificationStore.add(get(_)('results.exportedComparison'), 'success');
  }

  // ---- Enhanced delta comparison ----
  function buildEnhancedComparisonData(results, infoCache) {
    const targets = [...new Set(results.filter((r) => !r.error).map((r) => r.target))];
    if (targets.length < 2) return { targets: [], rows: [] };

    const oidMap = {};
    for (const res of results) {
      if (res.error) continue;
      const items = res.result?.type === 'WalkResponse' || res.result?.type === 'GetBulkResponse'
        ? (Array.isArray(res.result?.value) ? res.result.value : [])
        : (res.result ? [res.result] : []);
      for (const item of items) {
        if (!oidMap[item.oid]) oidMap[item.oid] = {};
        oidMap[item.oid][res.target] = {
          value: typeof item.value === 'string' ? item.value : (item.value != null ? String(item.value) : ''),
          type: item.type,
          numValue: parseFloat(item.value),
        };
      }
    }

    const rows = Object.entries(oidMap).map(([oid, targetValues]) => {
      const info = infoCache[oid];
      const name = info?.name || '';
      const values = {};
      let isNumeric = true;
      const numericValues = [];

      for (const t of targets) {
        const tv = targetValues[t];
        values[t] = tv || null;
        if (tv && !isNaN(tv.numValue)) numericValues.push(tv.numValue);
        else if (tv) isNumeric = false;
      }

      let delta = null;
      let percentDiff = null;
      let status = 'identical';

      if (targets.length === 2) {
        const vA = values[targets[0]];
        const vB = values[targets[1]];
        if (!vA || !vB) {
          status = 'missing';
        } else if (isNumeric && numericValues.length === 2) {
          delta = Math.abs(numericValues[0] - numericValues[1]);
          percentDiff = numericValues[0] !== 0
            ? (delta / Math.abs(numericValues[0])) * 100
            : (numericValues[1] !== 0 ? 100 : 0);
          status = delta === 0 ? 'identical' : 'different';
        } else {
          status = vA.value === vB.value ? 'identical' : 'different';
        }
      } else {
        const allValues = targets.map((t) => values[t]?.value).filter((v) => v != null);
        const allSame = allValues.every((v) => v === allValues[0]);
        status = allValues.length < targets.length ? 'missing' : (allSame ? 'identical' : 'different');
        if (isNumeric && numericValues.length >= 2) {
          const min = Math.min(...numericValues);
          const max = Math.max(...numericValues);
          delta = max - min;
          percentDiff = min !== 0 ? (delta / Math.abs(min)) * 100 : (max !== 0 ? 100 : 0);
        }
      }

      return { oid, name, values, isNumeric, delta, percentDiff, status };
    });

    return { targets, rows };
  }

  $: comparisonData = mode === 'enhanced'
    ? buildEnhancedComparisonData(bulkResults, oidInfoCache)
    : { targets: [], rows: [] };

  $: sortedComparisonRows = (() => {
    if (!comparisonData.rows.length) return [];
    const rows = [...comparisonData.rows];
    rows.sort((a, b) => {
      let cmp = 0;
      if (compareSortKey === 'delta') cmp = (a.delta ?? -1) - (b.delta ?? -1);
      else if (compareSortKey === 'percent') cmp = (a.percentDiff ?? -1) - (b.percentDiff ?? -1);
      else cmp = a.oid.localeCompare(b.oid);
      return compareSortAsc ? cmp : -cmp;
    });
    return rows;
  })();

  function toggleCompareSort(key) {
    if (compareSortKey === key) compareSortAsc = !compareSortAsc;
    else { compareSortKey = key; compareSortAsc = true; }
  }

  function exportEnhancedComparisonCSV() {
    const { targets, rows } = comparisonData;
    const escape = (s) => {
      s = String(s ?? '');
      if (s.includes(',') || s.includes('"') || s.includes('\n')) return '"' + s.replace(/"/g, '""') + '"';
      return s;
    };
    const header = ['OID', 'Name', ...targets, 'Delta', '% Diff', 'Status'].join(',');
    const lines = [header, ...rows.map((r) => [
      escape(r.oid), escape(r.name),
      ...targets.map((t) => escape(r.values[t]?.value ?? '')),
      r.delta != null ? r.delta.toFixed(2) : '',
      r.percentDiff != null ? r.percentDiff.toFixed(1) + '%' : '',
      r.status,
    ].join(','))];
    downloadFile(lines.join('\n'), 'comparison.csv', 'text/csv');
  }
</script>

{#if mode === 'enhanced'}
  {#if comparisonData.rows.length > 0}
    <div class="comparison-section">
      <div class="comparison-header">
        <h4>{$_('results.compareTitle')} ({comparisonData.rows.length} OIDs)</h4>
        <button class="btn btn-small" on:click={exportEnhancedComparisonCSV}>{$_('results.exportComparison')}</button>
      </div>
      <div class="comparison-table-wrapper">
        <table class="comparison-table">
          <thead>
            <tr>
              <th class="sortable" on:click={() => toggleCompareSort('oid')}>
                OID {compareSortKey === 'oid' ? (compareSortAsc ? '▲' : '▼') : ''}
              </th>
              <th>Name</th>
              {#each comparisonData.targets as target}
                <th class="target-col" title={targetTitle(target, $anonMode)}>{displayTarget(target, $targetLabels, $anonMode)}</th>
              {/each}
              <th class="sortable" on:click={() => toggleCompareSort('delta')}>
                {$_('results.delta')} {compareSortKey === 'delta' ? (compareSortAsc ? '▲' : '▼') : ''}
              </th>
              <th class="sortable" on:click={() => toggleCompareSort('percent')}>
                {$_('results.percentDiff')} {compareSortKey === 'percent' ? (compareSortAsc ? '▲' : '▼') : ''}
              </th>
            </tr>
          </thead>
          <tbody>
            {#each sortedComparisonRows as row}
              <tr class="compare-row {row.status}">
                <td class="oid-cell">{row.oid}</td>
                <td class="name-cell">{row.name}</td>
                {#each comparisonData.targets as target}
                  <td class="value-cell" class:missing={!row.values[target]}>
                    {row.values[target]?.value ?? '—'}
                  </td>
                {/each}
                <td class="delta-cell">{row.delta != null ? row.delta.toFixed(2) : '—'}</td>
                <td class="percent-cell">{row.percentDiff != null ? row.percentDiff.toFixed(1) + '%' : '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  {/if}
{:else}
  {@const comp = buildComparisonData(bulkResults)}
  <div class="comparison-view">
    <div class="comparison-table-wrapper">
      <table>
        <thead>
          <tr>
            <th>{$_('common.oid')}</th>
            <th>{$_('common.name')}</th>
            {#each comp.targets as target}
              <th class="target-col" title={targetTitle(target, $anonMode)}>{displayTarget(target, $targetLabels, $anonMode)}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each comp.oids as oid}
            <tr class:diff-row={valuesDiffer(oid, comp.targets, comp.targetData)}>
              <td class="oid-cell" title={oid}>{oid}</td>
              <td class="name-cell">{oidInfoCache[oid]?.name || ''}</td>
              {#each comp.targets as target}
                {@const val = comp.targetData[target]?.[oid]}
                <td
                  class="comp-value-cell"
                  class:diff-cell={valuesDiffer(oid, comp.targets, comp.targetData) && val !== undefined}
                  title={val !== undefined ? String(val) : 'N/A'}
                >
                  {val !== undefined ? formatValueWithEnum(val, oid, undefined, oidInfoCache) : '-'}
                </td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <p class="table-info">{$_('results.compInfo', { values: { oids: comp.oids.length, targets: comp.targets.length } })}</p>
    <div class="comparison-actions">
      <button class="btn-export" on:click={exportComparisonCSV}>{$_('results.compCsv')}</button>
    </div>
  </div>
{/if}

<style>
  .comparison-view,
  .comparison-section {
    margin-top: 10px;
  }

  .comparison-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
  }

  .comparison-header h4 {
    margin: 0;
    font-size: 0.95em;
    color: var(--text-color);
  }

  .comparison-actions {
    margin-top: 8px;
    text-align: right;
  }

  .comparison-table-wrapper {
    max-height: 500px;
    overflow: auto;
    border: 1px solid var(--border-color);
    border-radius: 4px;
  }

  .comparison-table-wrapper table,
  .comparison-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85em;
  }

  .comparison-table-wrapper thead,
  .comparison-table th {
    position: sticky;
    top: 0;
    background-color: var(--bg-lighter-color);
    z-index: 1;
  }

  .comparison-table-wrapper th,
  .comparison-table th {
    text-align: left;
    padding: 6px 10px;
    border-bottom: 2px solid var(--border-color);
    font-weight: 600;
    white-space: nowrap;
  }

  .comparison-table-wrapper td,
  .comparison-table td {
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-color);
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .comparison-table th.sortable {
    cursor: pointer;
  }

  .comparison-table th.sortable:hover {
    color: var(--accent-color);
  }

  .oid-cell {
    font-family: 'Courier New', monospace;
    color: var(--oid-color);
    white-space: nowrap;
  }

  .name-cell {
    color: var(--name-color);
  }

  .comparison-table .value-cell {
    font-family: 'Courier New', monospace;
  }

  .comparison-table .value-cell.missing {
    color: var(--text-muted);
    font-style: italic;
  }

  .comparison-table .delta-cell,
  .comparison-table .percent-cell {
    font-family: 'Courier New', monospace;
    text-align: right;
  }

  .target-col {
    color: var(--accent-color);
  }

  .diff-row {
    background-color: var(--warning-subtle);
  }

  .diff-cell {
    color: var(--warning-color);
    font-weight: 600;
  }

  .comp-value-cell {
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .compare-row.identical { background-color: var(--success-subtle); }
  .compare-row.different { background-color: var(--warning-subtle); }
  .compare-row.missing { background-color: var(--hover-overlay); }
  .compare-row:hover { background-color: var(--hover-overlay-medium); }

  .table-info {
    font-size: 0.85em;
    color: var(--text-muted);
    margin-top: 8px;
    font-style: italic;
    text-align: center;
  }

  .btn.btn-small,
  .btn-export {
    padding: 4px 10px;
    font-size: 0.8em;
    background-color: transparent;
    border: 1px solid var(--border-color);
    color: var(--text-dimmed);
    border-radius: 3px;
    cursor: pointer;
    font-weight: 500;
    transition: all 0.2s;
  }

  .btn.btn-small:hover,
  .btn-export:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
    background-color: var(--accent-subtle-medium);
  }
</style>
