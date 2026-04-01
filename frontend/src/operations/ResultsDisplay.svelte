<script>
  import { createEventDispatcher } from 'svelte';
  import { copyToClipboard } from '../utils/clipboard';
  import { escapeCSV, downloadFile } from '../utils/csv';
  import { formatValueWithEnum as _formatValueWithEnum, findTableParentNode } from '../utils/mibTree';
  import { notificationStore } from '../stores/notifications';
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';

  const dispatch = createEventDispatcher();

  /** @type {Array} */
  export let bulkResults = [];

  /** @type {string} */
  export let activeOperation = 'GET';

  /** @type {object|null} */
  export let selectedNode = null;

  /** @type {object} */
  export let oidInfoCache = {};

  /** @type {Array} */
  export let mibTree = [];

  // Internal state
  let tableViewEnabled = false;
  let sortColumn = null;
  let sortAscending = true;
  let comparisonViewEnabled = false;
  let compareEnabled = false;
  let compareSortKey = 'oid'; // 'oid', 'delta', 'percent'
  let compareSortAsc = true;

  // Reactive: reset table view when operation changes away from WALK/GETBULK
  $: if (activeOperation !== 'WALK' && activeOperation !== 'GETBULK') {
    tableViewEnabled = false;
  }

  // Can show comparison view: multi-target + WALK/GETBULK
  $: canShowComparison = (activeOperation === 'WALK' || activeOperation === 'GETBULK')
    && bulkResults.filter(r => !r.error && Array.isArray(r.result?.value)).length > 1;

  $: uniqueTargets = [...new Set(bulkResults.filter(r => !r.error).map(r => r.target))];
  $: canCompare = uniqueTargets.length >= 2;

  // Wrapper: resolves oidInfoCache entry then delegates to shared util
  function formatValueWithEnum(value, oid) {
    return _formatValueWithEnum(value, oidInfoCache[oid]);
  }

  // Build comparison data from multi-target WALK/GETBULK results (legacy)
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

    const oids = [...oidSet].sort();
    return { targets, oids, targetData };
  }

  // Check if values differ across targets for a given OID
  function valuesDiffer(oid, targets, targetData) {
    const values = targets.map(t => targetData[t]?.[oid]);
    const first = values[0];
    return values.some(v => JSON.stringify(v) !== JSON.stringify(first));
  }

  // Export comparison table as CSV (legacy)
  function exportComparisonCSV() {
    const comp = buildComparisonData(bulkResults);
    if (comp.oids.length === 0) return;
    const lines = [];
    lines.push(['OID', 'Name', ...comp.targets].map(escapeCSV).join(','));
    for (const oid of comp.oids) {
      const name = oidInfoCache[oid]?.name || '';
      const values = comp.targets.map(t => {
        const v = comp.targetData[t]?.[oid];
        return v !== undefined ? formatValueWithEnum(v, oid) : '';
      });
      lines.push([oid, name, ...values].map(escapeCSV).join(','));
    }
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
    downloadFile(lines.join('\n'), `snmp-comparison-${timestamp}.csv`, 'text/csv');
    notificationStore.add(get(_)('results.exportedComparison'), 'success');
  }

  // ============ ENHANCED COMPARISON VIEW ============

  function buildEnhancedComparisonData(results, infoCache) {
    const targets = [...new Set(results.filter(r => !r.error).map(r => r.target))];
    if (targets.length < 2) return { targets: [], rows: [] };

    // Build OID->target->value map
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
          numValue: parseFloat(item.value)
        };
      }
    }

    // Build rows
    const rows = Object.entries(oidMap).map(([oid, targetValues]) => {
      const info = infoCache[oid];
      const name = info?.name || '';
      const values = {};
      let isNumeric = true;
      const numericValues = [];

      for (const t of targets) {
        const tv = targetValues[t];
        values[t] = tv || null;
        if (tv && !isNaN(tv.numValue)) {
          numericValues.push(tv.numValue);
        } else if (tv) {
          isNumeric = false;
        }
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
        // 3+ targets: check if all values are identical
        const allValues = targets.map(t => values[t]?.value).filter(v => v != null);
        const allSame = allValues.every(v => v === allValues[0]);
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

  $: comparisonData = compareEnabled ? buildEnhancedComparisonData(bulkResults, oidInfoCache) : { targets: [], rows: [] };

  $: sortedComparisonRows = (() => {
    if (!comparisonData.rows.length) return [];
    const rows = [...comparisonData.rows];
    rows.sort((a, b) => {
      let cmp = 0;
      if (compareSortKey === 'delta') {
        cmp = (a.delta ?? -1) - (b.delta ?? -1);
      } else if (compareSortKey === 'percent') {
        cmp = (a.percentDiff ?? -1) - (b.percentDiff ?? -1);
      } else {
        cmp = a.oid.localeCompare(b.oid);
      }
      return compareSortAsc ? cmp : -cmp;
    });
    return rows;
  })();

  function toggleCompareSort(key) {
    if (compareSortKey === key) {
      compareSortAsc = !compareSortAsc;
    } else {
      compareSortKey = key;
      compareSortAsc = true;
    }
  }

  function exportEnhancedComparisonCSV() {
    const { targets, rows } = comparisonData;
    const escape = (s) => {
      s = String(s ?? '');
      if (s.includes(',') || s.includes('"') || s.includes('\n')) return '"' + s.replace(/"/g, '""') + '"';
      return s;
    };
    const header = ['OID', 'Name', ...targets, 'Delta', '% Diff', 'Status'].join(',');
    const lines = [header, ...rows.map(r => [
      escape(r.oid), escape(r.name),
      ...targets.map(t => escape(r.values[t]?.value ?? '')),
      r.delta != null ? r.delta.toFixed(2) : '',
      r.percentDiff != null ? r.percentDiff.toFixed(1) + '%' : '',
      r.status
    ].join(','))];
    const blob = new Blob([lines.join('\n')], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'comparison.csv';
    a.click();
    URL.revokeObjectURL(url);
  }

  // Export results as CSV
  function exportAsCSV() {
    if (bulkResults.length === 0) return;
    const lines = [];
    const isMulti = activeOperation === 'WALK' || activeOperation === 'GETBULK';

    if (isMulti) {
      lines.push('Target,OID,Type,Value');
      for (const res of bulkResults) {
        if (res.error) {
          lines.push(`${escapeCSV(res.target)},,,"Error: ${escapeCSV(res.error)}"`);
          continue;
        }
        if (Array.isArray(res.result?.value)) {
          for (const item of res.result.value) {
            lines.push(`${escapeCSV(res.target)},${escapeCSV(item.oid)},${escapeCSV(item.type)},${escapeCSV(typeof item.value === 'string' ? item.value : JSON.stringify(item.value))}`);
          }
        }
      }
    } else {
      lines.push('Target,OID,Type,Value,Error');
      for (const res of bulkResults) {
        if (res.error) {
          lines.push(`${escapeCSV(res.target)},,,,${escapeCSV(res.error)}`);
        } else {
          lines.push(`${escapeCSV(res.target)},${escapeCSV(res.result.oid)},${escapeCSV(res.result.type)},${escapeCSV(typeof res.result.value === 'string' ? res.result.value : JSON.stringify(res.result.value))},`);
        }
      }
    }

    const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
    downloadFile(lines.join('\n'), `snmp-${activeOperation.toLowerCase()}-${timestamp}.csv`, 'text/csv');
    notificationStore.add(get(_)('results.exportedCsv'), 'success');
  }

  // Export results as text
  function exportAsText() {
    if (bulkResults.length === 0) return;
    const lines = [];
    const isMulti = activeOperation === 'WALK' || activeOperation === 'GETBULK';

    for (const res of bulkResults) {
      lines.push(`--- Target: ${res.target} ---`);
      if (res.error) {
        lines.push(`  Error: ${res.error}`);
      } else if (isMulti && Array.isArray(res.result?.value)) {
        for (const item of res.result.value) {
          const val = typeof item.value === 'string' ? item.value : JSON.stringify(item.value);
          lines.push(`  ${item.oid} = ${item.type}: ${val}`);
        }
        lines.push(`  (${res.result.value.length} results)`);
      } else {
        const val = typeof res.result.value === 'string' ? res.result.value : JSON.stringify(res.result.value);
        lines.push(`  ${res.result.oid} = ${res.result.type}: ${val}`);
      }
      lines.push('');
    }

    const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
    downloadFile(lines.join('\n'), `snmp-${activeOperation.toLowerCase()}-${timestamp}.txt`, 'text/plain');
    notificationStore.add(get(_)('results.exportedTxt'), 'success');
  }

  // Export table view as CSV
  function exportTableAsCSV() {
    if (bulkResults.length === 0 || !effectiveTableNode) return;
    const colDefs = getTableColumnDefs(effectiveTableNode);
    if (colDefs.length === 0) return;

    // Use the first result's walk data
    const firstRes = bulkResults.find(r => !r.error && Array.isArray(r.result?.value));
    if (!firstRes) return;

    const tableData = buildTableData(firstRes.result.value, colDefs);
    const lines = [];

    // Header
    lines.push(['Index', ...tableData.columns.map(c => c.name)].map(escapeCSV).join(','));

    // Rows
    for (const row of tableData.rows) {
      const cells = [row.index, ...tableData.columns.map(col => {
        const cell = row.cells[col.oid];
        if (!cell) return '';
        return typeof cell.value === 'string' ? cell.value : JSON.stringify(cell.value);
      })];
      lines.push(cells.map(escapeCSV).join(','));
    }

    const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
    downloadFile(lines.join('\n'), `snmp-table-${timestamp}.csv`, 'text/csv');
    notificationStore.add(get(_)('results.exportedTable'), 'success');
  }

  // ============ TABLE VIEW FUNCTIONS ============

  // Get column definitions from the MIB tree for a Table or Row node
  function getTableColumnDefs(node) {
    if (!node) return [];
    let rowNode = node;
    if (node.mibType === 'Table') {
      rowNode = (node.children || []).find(c => c.mibType === 'Row');
      if (!rowNode) return [];
    }
    if (rowNode.mibType !== 'Row') return [];
    return (rowNode.children || [])
      .filter(c => c.mibType === 'Column')
      .sort((a, b) => {
        const aLast = parseInt(a.oid.split('.').pop());
        const bLast = parseInt(b.oid.split('.').pop());
        return aLast - bLast;
      });
  }

  // Check if table view is applicable
  function canShowTableView(node, results) {
    if (!node) return false;
    if (results.length === 0) return false;
    const nodeType = node.mibType;
    if (nodeType !== 'Table' && nodeType !== 'Row') return false;
    return getTableColumnDefs(node).length > 0;
  }

  // Reconstruct WALK results into a structured table
  function buildTableData(walkResults, columnDefs) {
    const columns = columnDefs.map(col => ({ name: col.name, oid: col.oid, syntax: col.syntax || '' }));
    const rowMap = {};

    for (const item of walkResults) {
      const itemOid = item.oid;
      let matchedCol = null;
      let instanceIdx = '';
      for (const col of columnDefs) {
        if (itemOid.startsWith(col.oid + '.')) {
          matchedCol = col;
          instanceIdx = itemOid.substring(col.oid.length + 1);
          break;
        }
      }
      if (!matchedCol) continue;

      if (!rowMap[instanceIdx]) {
        rowMap[instanceIdx] = {};
      }
      rowMap[instanceIdx][matchedCol.oid] = {
        value: item.value,
        type: item.type,
        fullOid: item.oid
      };
    }

    let rows = Object.entries(rowMap).map(([index, cells]) => ({ index, cells }));

    // Apply sorting
    if (sortColumn) {
      rows.sort((a, b) => {
        let aVal, bVal;
        if (sortColumn === '__index') {
          aVal = a.index;
          bVal = b.index;
        } else {
          aVal = a.cells[sortColumn]?.value ?? '';
          bVal = b.cells[sortColumn]?.value ?? '';
        }
        const aNum = Number(aVal);
        const bNum = Number(bVal);
        let cmp;
        if (!isNaN(aNum) && !isNaN(bNum)) {
          cmp = aNum - bNum;
        } else {
          cmp = String(aVal).localeCompare(String(bVal));
        }
        return sortAscending ? cmp : -cmp;
      });
    }

    return { columns, rows };
  }

  $: autoDetectedTableNode = (() => {
    // Only try auto-detection for WALK/GETBULK when selectedNode doesn't provide table structure
    if (activeOperation !== 'WALK' && activeOperation !== 'GETBULK') return null;
    if (selectedNode && canShowTableView(selectedNode, bulkResults)) return null;
    if (!bulkResults.length || !mibTree.length) return null;

    // Find first successful walk result with data
    const firstRes = bulkResults.find(r => !r.error && r.result?.type === 'WalkResponse' && Array.isArray(r.result?.value) && r.result.value.length > 0);
    if (!firstRes) return null;

    // Try to detect table from first few OIDs
    return findTableParentNode(firstRes.result.value[0].oid, mibTree);
  })();

  // Use detected table node as fallback for table view
  $: effectiveTableNode = (selectedNode && canShowTableView(selectedNode, bulkResults)) ? selectedNode : autoDetectedTableNode;

  // Auto-enable table view when a Table/Row node is detected
  $: if (effectiveTableNode && (activeOperation === 'WALK' || activeOperation === 'GETBULK') && bulkResults.length > 0) {
    if (effectiveTableNode.mibType === 'Table' || effectiveTableNode.mibType === 'Row') {
      tableViewEnabled = true;
    }
  }

  function handleColumnSort(colId) {
    if (sortColumn === colId) {
      sortAscending = !sortAscending;
    } else {
      sortColumn = colId;
      sortAscending = true;
    }
  }
</script>

{#if bulkResults.length > 0}
  <div class="results-container">
    <div class="results-header">
      <h4>{$_('results.title')}</h4>
      <div class="export-buttons">
        {#if canShowComparison}
          <button
            class="btn-view"
            class:active={comparisonViewEnabled}
            on:click={() => comparisonViewEnabled = !comparisonViewEnabled}
          >
            {$_('results.comparison')}
          </button>
        {/if}
        {#if canCompare}
          <button class="btn-view" class:active={compareEnabled} on:click={() => { compareEnabled = !compareEnabled; }}>
            {$_('results.compare')}
          </button>
        {/if}
        <button class="btn-export" on:click={exportAsCSV} title={$_('results.csv')}>{$_('results.csv')}</button>
        <button class="btn-export" on:click={exportAsText} title={$_('results.txt')}>{$_('results.txt')}</button>
        {#if tableViewEnabled && canShowTableView(effectiveTableNode, bulkResults)}
          <button class="btn-export" on:click={exportTableAsCSV} title={$_('results.tableCsv')}>{$_('results.tableCsv')}</button>
        {/if}
        {#if comparisonViewEnabled && canShowComparison}
          <button class="btn-export" on:click={exportComparisonCSV} title={$_('results.compCsv')}>{$_('results.compCsv')}</button>
        {/if}
        {#if compareEnabled && comparisonData.rows.length > 0}
          <button class="btn-export" on:click={exportEnhancedComparisonCSV} title={$_('results.exportComparison')}>{$_('results.exportComparison')}</button>
        {/if}
      </div>
    </div>

    {#if compareEnabled && comparisonData.rows.length > 0}
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
                  <th class="target-col">{target}</th>
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

    {#if comparisonViewEnabled && canShowComparison}
      {@const comp = buildComparisonData(bulkResults)}
      <div class="comparison-view">
        <div class="comparison-table-wrapper">
          <table>
            <thead>
              <tr>
                <th>{$_('common.oid')}</th>
                <th>{$_('common.name')}</th>
                {#each comp.targets as target}
                  <th class="target-col">{target}</th>
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
                      {val !== undefined ? formatValueWithEnum(val, oid) : '-'}
                    </td>
                  {/each}
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
        <p class="table-info">{$_('results.compInfo', { values: { oids: comp.oids.length, targets: comp.targets.length } })}</p>
      </div>
    {:else}
      {#each bulkResults as res}
        <div class="result" class:success={!res.error} class:error={res.error}>
          <p class="result-target">
            {res.target}
            {#if res.responseTimeMs}
              <span class="response-time-badge">{res.responseTimeMs}ms</span>
            {/if}
          </p>
          {#if res.error}
            <p><strong>{$_('common.error')}:</strong> {res.error}</p>
          {:else if (res.result.type === 'WalkResponse' || res.result.type === 'GetBulkResponse') && Array.isArray(res.result.value)}
          <!-- WALK/GETBULK results display -->
          <p><strong>{$_('results.baseOid')}</strong> {res.result.oid}</p>
          <p><strong>{$_('results.resultsFound', { values: { count: res.result.value.length } })}</strong></p>

          {#if canShowTableView(effectiveTableNode, bulkResults)}
            <div class="view-toggle">
              <button
                class="btn-view"
                class:active={!tableViewEnabled}
                on:click={() => tableViewEnabled = false}
              >
                {$_('results.rawView')}
              </button>
              <button
                class="btn-view"
                class:active={tableViewEnabled}
                on:click={() => { tableViewEnabled = true; sortColumn = null; }}
              >
                {$_('results.tableView')}
              </button>
            </div>
          {/if}

          {#if tableViewEnabled && canShowTableView(effectiveTableNode, bulkResults)}
            {@const colDefs = getTableColumnDefs(effectiveTableNode)}
            {@const tableData = buildTableData(res.result.value, colDefs)}
            <div class="table-view-results">
              <table>
                <thead>
                  <tr>
                    <th
                      class="sortable"
                      on:click={() => handleColumnSort('__index')}
                    >
                      {$_('results.index')} {sortColumn === '__index' ? (sortAscending ? '▲' : '▼') : ''}
                    </th>
                    {#each tableData.columns as col}
                      <th
                        class="sortable"
                        on:click={() => handleColumnSort(col.oid)}
                        title="{col.oid} ({col.syntax})"
                      >
                        {col.name}
                        {#if sortColumn === col.oid}
                          {sortAscending ? '▲' : '▼'}
                        {/if}
                      </th>
                    {/each}
                  </tr>
                </thead>
                <tbody>
                  {#each tableData.rows as row}
                    <tr>
                      <td class="index-cell">{row.index}</td>
                      {#each tableData.columns as col}
                        <td
                          class="table-value-cell clickable"
                          title={row.cells[col.oid]?.fullOid || ''}
                          on:click={() => row.cells[col.oid] && dispatch('walkResultClick', {oid: row.cells[col.oid].fullOid, value: row.cells[col.oid].value, type: row.cells[col.oid].type})}
                          on:keydown={(e) => e.key === 'Enter' && row.cells[col.oid] && dispatch('walkResultClick', {oid: row.cells[col.oid].fullOid, value: row.cells[col.oid].value, type: row.cells[col.oid].type})}
                        >
                          {row.cells[col.oid]?.value !== undefined ? formatValueWithEnum(row.cells[col.oid].value, row.cells[col.oid].fullOid || '') : '-'}
                        </td>
                      {/each}
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
            <p class="table-info">{$_('results.tableInfo', { values: { rows: tableData.rows.length, cols: tableData.columns.length } })}</p>
          {:else}
            <!-- Raw WALK results table -->
            <div class="walk-results">
              <table>
                <thead>
                  <tr>
                    <th>{$_('common.oid')}</th>
                    <th>{$_('common.type')}</th>
                    <th>{$_('common.value')}</th>
                    <th class="copy-col"></th>
                  </tr>
                </thead>
                <tbody>
                  {#each res.result.value as walkItem}
                    <tr
                      class="walk-result-row clickable"
                      on:click={() => dispatch('walkResultClick', walkItem)}
                      on:keydown={(e) => e.key === 'Enter' && dispatch('walkResultClick', walkItem)}
                      role="button"
                      tabindex="0"
                      title={$_('results.clickToUseOid')}
                    >
                      <td class="oid-cell" title={walkItem.oid}>
                        {#if oidInfoCache[walkItem.oid]?.name}
                          <span class="oid-name">{oidInfoCache[walkItem.oid].name}</span>
                        {/if}
                        {walkItem.oid}
                      </td>
                      <td>{walkItem.type}</td>
                      <td class="value-cell" title={JSON.stringify(walkItem.value)}>{formatValueWithEnum(walkItem.value, walkItem.oid)}</td>
                      <td class="copy-cell">
                        <button
                          class="btn-copy-small"
                          on:click|stopPropagation={() => copyToClipboard(String(walkItem.value), $_('common.value'))}
                          title={$_('common.copyValue')}
                        >📋</button>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        {:else}
          <!-- GET/SET results display -->
          <p class="result-line">
            <strong>{$_('common.oid')}:</strong>
            <span class="result-oid">{res.result.oid}</span>
            <button class="btn-copy-small" on:click={() => copyToClipboard(res.result.oid, $_('common.oid'))} title={$_('common.copyOid')}>📋</button>
          </p>
          <p><strong>{$_('common.type')}:</strong> {res.result.type}
            {#if oidInfoCache[res.result.oid]?.name}
              <span class="resolved-name">({oidInfoCache[res.result.oid].name})</span>
            {/if}
          </p>
          <p class="result-line">
            <strong>{$_('common.value')}:</strong>
            <span class="result-value">{formatValueWithEnum(res.result.value, res.result.oid)}</span>
            <button class="btn-copy-small" on:click={() => copyToClipboard(String(res.result.value), $_('common.value'))} title={$_('common.copyValue')}>📋</button>
          </p>
        {/if}
        </div>
      {/each}
    {/if}
  </div>
{/if}

<style>
  .results-container {
    margin-top: 20px;
  }

  .result {
    margin-top: 10px;
    padding: 12px;
    border-radius: 5px;
    border: 1px solid;
  }

  .result-target {
    font-weight: bold;
    margin-bottom: 8px;
  }

  .success {
    background-color: var(--success-subtle-medium);
    border-color: var(--success-color);
  }

  .error {
    background-color: var(--error-subtle-medium);
    border-color: var(--error-color);
    color: var(--error-color);
  }

  .walk-results {
    margin-top: 10px;
    max-height: 400px;
    overflow-y: auto;
    border: 1px solid var(--border-color);
    border-radius: 4px;
  }

  .walk-results table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.9em;
  }

  .walk-results thead {
    position: sticky;
    top: 0;
    background-color: var(--bg-lighter-color);
    z-index: 1;
  }

  .walk-results th {
    text-align: left;
    padding: 8px;
    border-bottom: 2px solid var(--border-color);
    font-weight: 600;
  }

  .walk-results td {
    padding: 6px 8px;
    border-bottom: 1px solid var(--border-color);
  }

  .walk-results tr:hover {
    background-color: var(--hover-overlay);
  }

  .response-time-badge {
    font-size: 0.8em;
    padding: 2px 8px;
    border-radius: 10px;
    margin-left: 8px;
    font-weight: 600;
    background-color: var(--accent-subtle-strong);
    color: var(--oid-color);
  }

  .oid-name {
    color: var(--name-color);
    font-size: 0.85em;
    margin-right: 6px;
    font-family: inherit;
  }

  .resolved-name {
    color: var(--name-color);
    font-size: 0.9em;
    margin-left: 6px;
  }

  .walk-results .oid-cell {
    font-family: 'Courier New', monospace;
    font-size: 0.85em;
    color: var(--oid-color);
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .walk-results .value-cell {
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Clickable WALK result rows */
  .walk-result-row.clickable {
    cursor: pointer;
    transition: background-color 0.15s ease;
  }

  .walk-result-row.clickable:hover {
    background-color: var(--accent-subtle-intense) !important;
  }

  .walk-result-row.clickable:focus {
    outline: 2px solid var(--accent-color);
    outline-offset: -2px;
  }

  .walk-result-row.clickable:hover .oid-cell {
    color: var(--accent-color);
    text-decoration: underline;
  }

  /* Copy buttons */
  .btn-copy-small {
    background: transparent;
    border: none;
    cursor: pointer;
    padding: 2px 4px;
    font-size: 0.85em;
    opacity: 0.5;
    transition: opacity 0.2s;
  }

  .btn-copy-small:hover {
    opacity: 1;
  }

  .result-line {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .result-oid, .result-value {
    font-family: 'Courier New', monospace;
  }

  .copy-col {
    width: 40px;
  }

  .copy-cell {
    text-align: center;
  }

  /* Comparison View */
  .comparison-view {
    margin-top: 10px;
  }

  .comparison-table-wrapper {
    max-height: 500px;
    overflow: auto;
    border: 1px solid var(--border-color);
    border-radius: 4px;
  }

  .comparison-table-wrapper table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85em;
  }

  .comparison-table-wrapper thead {
    position: sticky;
    top: 0;
    background-color: var(--bg-lighter-color);
    z-index: 1;
  }

  .comparison-table-wrapper th {
    text-align: left;
    padding: 8px 10px;
    border-bottom: 2px solid var(--border-color);
    font-weight: 600;
    white-space: nowrap;
  }

  .comparison-table-wrapper td {
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-color);
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .comparison-table-wrapper .oid-cell {
    font-family: 'Courier New', monospace;
    font-size: 0.85em;
    color: var(--oid-color);
    max-width: 250px;
  }

  .comparison-table-wrapper .name-cell {
    color: var(--name-color);
    font-size: 0.9em;
    max-width: 150px;
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

  /* Table View styles */
  .view-toggle {
    display: flex;
    gap: 4px;
    margin: 10px 0;
  }

  .btn-view {
    padding: 6px 14px;
    font-size: 0.85em;
    background-color: transparent;
    border: 1px solid var(--border-color);
    color: var(--text-muted);
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.2s;
    font-weight: 500;
  }

  .btn-view:hover {
    border-color: var(--accent-color);
    color: var(--text-color);
  }

  .btn-view.active {
    background-color: var(--accent-color);
    border-color: var(--accent-color);
    color: white;
  }

  .table-view-results {
    margin-top: 10px;
    max-height: 500px;
    overflow: auto;
    border: 1px solid var(--border-color);
    border-radius: 4px;
  }

  .table-view-results table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.9em;
  }

  .table-view-results thead {
    position: sticky;
    top: 0;
    background-color: var(--bg-lighter-color);
    z-index: 1;
  }

  .table-view-results th {
    text-align: left;
    padding: 8px;
    border-bottom: 2px solid var(--border-color);
    font-weight: 600;
    white-space: nowrap;
  }

  .table-view-results th.sortable {
    cursor: pointer;
    user-select: none;
  }

  .table-view-results th.sortable:hover {
    background-color: var(--accent-subtle-strong);
    color: var(--accent-color);
  }

  .table-view-results td {
    padding: 6px 8px;
    border-bottom: 1px solid var(--border-color);
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .table-view-results tr:hover {
    background-color: var(--hover-overlay);
  }

  .table-value-cell {
    cursor: pointer;
  }

  .table-value-cell:hover {
    background-color: var(--accent-subtle-strong);
    color: var(--accent-color);
  }

  .index-cell {
    font-family: 'Courier New', monospace;
    color: var(--oid-color);
    font-size: 0.85em;
    font-weight: 600;
  }

  .table-info {
    font-size: 0.85em;
    color: var(--text-muted);
    margin-top: 8px;
    font-style: italic;
    text-align: center;
  }

  /* Export buttons */
  .results-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 5px;
  }

  .results-header h4 {
    margin: 0;
  }

  .export-buttons {
    display: flex;
    gap: 6px;
  }

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

  .btn-export:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
    background-color: var(--accent-subtle-medium);
  }

  /* Enhanced Comparison View */
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

  .btn.btn-small {
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

  .btn.btn-small:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
    background-color: var(--accent-subtle-medium);
  }

  .comparison-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85em;
  }

  .comparison-table th {
    position: sticky;
    top: 0;
    background-color: var(--bg-lighter-color);
    padding: 6px 10px;
    text-align: left;
    border-bottom: 2px solid var(--border-color);
    font-weight: 600;
    white-space: nowrap;
  }

  .comparison-table th.sortable {
    cursor: pointer;
  }

  .comparison-table th.sortable:hover {
    color: var(--accent-color);
  }

  .comparison-table td {
    padding: 5px 10px;
    border-bottom: 1px solid var(--border-color);
  }

  .comparison-table .oid-cell {
    font-family: 'Courier New', monospace;
    color: var(--oid-color);
    white-space: nowrap;
  }

  .comparison-table .name-cell {
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

  .compare-row.identical {
    background-color: var(--success-subtle);
  }

  .compare-row.different {
    background-color: var(--warning-subtle);
  }

  .compare-row.missing {
    background-color: var(--hover-overlay);
  }

  .compare-row:hover {
    background-color: var(--hover-overlay-medium);
  }
</style>
