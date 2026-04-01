<script>
  import { createEventDispatcher } from 'svelte';
  import { _ } from 'svelte-i18n';

  export let entryA;
  export let entryB;

  const dispatch = createEventDispatcher();

  let showIdentical = false;

  // Build OID-keyed maps from WALK/GETBULK results
  function buildOidMap(entry) {
    const map = {};
    if (!entry.results) return map;
    for (const res of entry.results) {
      if (res.error || !res.result) continue;
      const values = res.result.type === 'WalkResponse' || res.result.type === 'GetBulkResponse'
        ? res.result.value
        : [res.result];
      if (!Array.isArray(values)) continue;
      for (const item of values) {
        const val = typeof item.value === 'string' ? item.value : JSON.stringify(item.value);
        map[item.oid] = { value: val, type: item.type };
      }
    }
    return map;
  }

  $: mapA = buildOidMap(entryA);
  $: mapB = buildOidMap(entryB);

  $: {
    const allOids = new Set([...Object.keys(mapA), ...Object.keys(mapB)]);
    const sortedOids = [...allOids].sort((a, b) => {
      const pa = a.split('.').map(Number);
      const pb = b.split('.').map(Number);
      for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
        const na = pa[i] ?? -1;
        const nb = pb[i] ?? -1;
        if (na !== nb) return na - nb;
      }
      return 0;
    });

    diffRows = sortedOids.map(oid => {
      const inA = oid in mapA;
      const inB = oid in mapB;
      let status;
      if (inA && inB) {
        status = mapA[oid].value === mapB[oid].value ? 'identical' : 'modified';
      } else if (inA) {
        status = 'removed';
      } else {
        status = 'added';
      }
      return {
        oid,
        valueA: inA ? mapA[oid].value : '',
        valueB: inB ? mapB[oid].value : '',
        typeA: inA ? mapA[oid].type : '',
        typeB: inB ? mapB[oid].type : '',
        status
      };
    });
  }

  let diffRows = [];

  $: visibleRows = showIdentical ? diffRows : diffRows.filter(r => r.status !== 'identical');

  $: stats = {
    added: diffRows.filter(r => r.status === 'added').length,
    removed: diffRows.filter(r => r.status === 'removed').length,
    modified: diffRows.filter(r => r.status === 'modified').length,
    identical: diffRows.filter(r => r.status === 'identical').length,
  };

  function exportDiffCSV() {
    const lines = ['Status,OID,Value A,Value B'];
    for (const row of visibleRows) {
      const escape = (s) => {
        if (s.includes(',') || s.includes('"') || s.includes('\n')) return '"' + s.replace(/"/g, '""') + '"';
        return s;
      };
      lines.push(`${row.status},${escape(row.oid)},${escape(row.valueA)},${escape(row.valueB)}`);
    }
    const blob = new Blob([lines.join('\n')], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `snmp-diff-${new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }

  function formatLabel(entry) {
    const time = new Date(entry.timestamp).toLocaleTimeString();
    return `${entry.operation} ${entry.oid} @ ${time}`;
  }
</script>

<div class="modal-overlay" on:mousedown={() => dispatch('close')}>
  <div class="modal" on:mousedown|stopPropagation>
    <div class="modal-header">
      <h3>{$_('diff.title')}</h3>
      <button class="close-btn" on:click={() => dispatch('close')}>&times;</button>
    </div>

    <div class="diff-labels">
      <div class="diff-label label-a">A: {formatLabel(entryA)}</div>
      <div class="diff-label label-b">B: {formatLabel(entryB)}</div>
    </div>

    <div class="diff-stats">
      <span class="stat added">{$_('diff.added', { values: { count: stats.added } })}</span>
      <span class="stat removed">{$_('diff.removed', { values: { count: stats.removed } })}</span>
      <span class="stat modified">{$_('diff.modified', { values: { count: stats.modified } })}</span>
      <span class="stat identical">{$_('diff.identical', { values: { count: stats.identical } })}</span>
    </div>

    <div class="diff-controls">
      <label class="toggle-label">
        <input type="checkbox" bind:checked={showIdentical} />
        {$_('diff.showIdentical')}
      </label>
      <button class="btn-export" on:click={exportDiffCSV}>{$_('diff.exportCsv')}</button>
    </div>

    <div class="diff-table-container">
      <table>
        <thead>
          <tr>
            <th class="col-status">{$_('diff.statusHeader')}</th>
            <th class="col-oid">{$_('diff.oidHeader')}</th>
            <th class="col-value">{$_('diff.valueAHeader')}</th>
            <th class="col-value">{$_('diff.valueBHeader')}</th>
          </tr>
        </thead>
        <tbody>
          {#each visibleRows as row}
            <tr class="diff-row {row.status}">
              <td class="status-cell">
                {#if row.status === 'added'}+
                {:else if row.status === 'removed'}-
                {:else if row.status === 'modified'}~
                {:else}=
                {/if}
              </td>
              <td class="oid-cell" title={row.oid}>{row.oid}</td>
              <td class="value-cell" title={row.valueA}>{row.valueA || '-'}</td>
              <td class="value-cell" title={row.valueB}>{row.valueB || '-'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <div class="diff-footer">
      {$_('diff.rowsShown', { values: { visible: visibleRows.length, total: diffRows.length } })}
    </div>
  </div>
</div>

<style>
  .modal-overlay {
    position: fixed;
    top: 0; left: 0; right: 0; bottom: 0;
    background-color: var(--backdrop-color-strong);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .modal {
    background-color: var(--bg-light-color);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    width: 90%;
    max-width: 900px;
    max-height: 85vh;
    display: flex;
    flex-direction: column;
  }

  .modal-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 15px 20px;
    border-bottom: 1px solid var(--border-color);
  }

  .modal-header h3 { margin: 0; }

  .close-btn {
    background: none;
    border: none;
    color: var(--text-color);
    font-size: 1.5rem;
    cursor: pointer;
  }

  .diff-labels {
    display: flex;
    gap: 15px;
    padding: 10px 20px;
    font-size: 0.85em;
  }

  .diff-label {
    flex: 1;
    padding: 6px 10px;
    border-radius: 4px;
    font-family: 'Courier New', monospace;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .label-a { background-color: var(--error-subtle-medium); border: 1px solid var(--error-border); }
  .label-b { background-color: var(--success-subtle-medium); border: 1px solid var(--success-border); }

  .diff-stats {
    display: flex;
    gap: 15px;
    padding: 8px 20px;
    font-size: 0.85em;
    font-weight: 600;
  }

  .stat.added { color: var(--success-color); }
  .stat.removed { color: var(--error-color); }
  .stat.modified { color: var(--warning-color); }
  .stat.identical { color: var(--text-muted); }

  .diff-controls {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 20px;
    border-bottom: 1px solid var(--border-color);
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.85em;
    cursor: pointer;
  }

  .btn-export {
    padding: 4px 10px;
    font-size: 0.8em;
    background-color: transparent;
    border: 1px solid var(--border-color);
    color: var(--text-dimmed);
    border-radius: 3px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .btn-export:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }

  .diff-table-container {
    flex: 1;
    overflow: auto;
    padding: 0;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85em;
  }

  thead {
    position: sticky;
    top: 0;
    background-color: var(--bg-lighter-color);
    z-index: 1;
  }

  th {
    text-align: left;
    padding: 8px 10px;
    border-bottom: 2px solid var(--border-color);
    font-weight: 600;
  }

  td {
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-color);
    max-width: 250px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .col-status { width: 50px; text-align: center; }
  .col-oid { width: 30%; }
  .col-value { width: 35%; }

  .status-cell { text-align: center; font-weight: 700; font-size: 1.1em; }

  .oid-cell {
    font-family: 'Courier New', monospace;
    font-size: 0.9em;
    color: var(--oid-color);
  }

  .value-cell {
    font-family: 'Courier New', monospace;
    font-size: 0.9em;
  }

  .diff-row.added { background-color: var(--success-subtle); }
  .diff-row.added .status-cell { color: var(--success-color); }
  .diff-row.removed { background-color: var(--error-subtle); }
  .diff-row.removed .status-cell { color: var(--error-color); }
  .diff-row.modified { background-color: var(--warning-subtle); }
  .diff-row.modified .status-cell { color: var(--warning-color); }
  .diff-row.identical .status-cell { color: var(--text-muted); }

  .diff-footer {
    padding: 8px 20px;
    font-size: 0.85em;
    color: var(--text-muted);
    text-align: center;
    border-top: 1px solid var(--border-color);
  }
</style>
