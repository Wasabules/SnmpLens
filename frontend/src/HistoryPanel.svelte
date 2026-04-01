<script>
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';
  import { historyStore } from './stores/historyStore';
  import { notificationStore } from './stores/notifications';
  import { mibStore } from './stores/mibStore';
  import DiffModal from './DiffModal.svelte';
  import { findNodeByOid, findMibNameByOid, formatValueWithEnum } from './utils/mibTree';
  import { formatTimestamp, formatDuration } from './utils/formatting';
  import { anonMode, anonymizeIp } from './utils/anonymize';

  let searchTerm = '';
  let operationFilter = 'all'; // 'all', 'GET', 'SET', 'WALK'
  let statusFilter = 'all'; // 'all', 'success', 'error'
  let filteredHistory = [];
  let expandedIds = new Set();
  let showExportModal = false;
  let exportData = '';
  let diffMode = false;
  let diffSelectionA = null;
  let diffSelectionB = null;
  let showDiffModal = false;

  // Get display name (MIB name or OID)
  function getDisplayName(oid) {
    const mibName = findMibNameByOid(oid, $mibStore.tree);
    return mibName || oid;
  }

  // Get value from entry results
  function getDisplayValue(entry) {
    if (!entry.results || entry.results.length === 0) return null;

    if (entry.operation === 'GET' || entry.operation === 'SET') {
      const firstResult = entry.results[0];
      if (firstResult?.result?.value !== undefined) {
        const val = firstResult.result.value;
        const node = findNodeByOid(entry.oid, $mibStore.tree);
        return formatValueWithEnum(val, node);
      }
    }

    if ((entry.operation === 'WALK' || entry.operation === 'GETBULK') && entry.totalResults) {
      return `${entry.totalResults} results`;
    }

    return null;
  }

  // Reactive filtering
  $: {
    let history = $historyStore;
    
    // Filter by operation
    if (operationFilter !== 'all') {
      history = history.filter(entry => entry.operation === operationFilter);
    }
    
    // Filter by status
    if (statusFilter !== 'all') {
      if (statusFilter === 'success') {
        history = history.filter(entry => entry.success);
      } else if (statusFilter === 'error') {
        history = history.filter(entry => !entry.success);
      }
    }
    
    // Filter by search term
    if (searchTerm) {
      const lowerSearch = searchTerm.toLowerCase();
      history = history.filter(entry => {
        return (
          entry.oid?.toLowerCase().includes(lowerSearch) ||
          entry.targets?.some(t => t.toLowerCase().includes(lowerSearch)) ||
          entry.operation.toLowerCase().includes(lowerSearch) ||
          entry.error?.toLowerCase().includes(lowerSearch)
        );
      });
    }
    
    filteredHistory = history;
  }

  function toggleExpand(id) {
    if (expandedIds.has(id)) {
      expandedIds.delete(id);
    } else {
      expandedIds.add(id);
    }
    expandedIds = expandedIds; // Trigger reactivity
  }

  function handleClearHistory() {
    const t = get(_);
    if (confirm(t('history.clearConfirm'))) {
      historyStore.clear();
      notificationStore.add(t('history.cleared'), 'success');
    }
  }

  function handleDeleteEntry(id) {
    const t = get(_);
    historyStore.remove(id);
    notificationStore.add(t('history.entryRemoved'), 'success');
  }

  function handleExport() {
    exportData = historyStore.export();
    showExportModal = true;
  }

  function handleCopyExport() {
    const t = get(_);
    navigator.clipboard.writeText(exportData).then(() => {
      notificationStore.add(t('history.exportedClipboard'), 'success');
      showExportModal = false;
    }).catch(() => {
      notificationStore.add(t('clipboard.copyError'), 'error');
    });
  }

  function handleDownloadExport() {
    const t = get(_);
    const blob = new Blob([exportData], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `snmp-history-${new Date().toISOString().split('T')[0]}.json`;
    a.click();
    URL.revokeObjectURL(url);
    notificationStore.add(t('history.downloaded'), 'success');
    showExportModal = false;
  }

  function getOperationIcon(operation) {
    switch (operation) {
      case 'GET': return '📥';
      case 'SET': return '📤';
      case 'WALK': return '🚶';
      default: return '📋';
    }
  }

  function getStatusColor(success) {
    return success ? 'var(--success-color)' : 'var(--error-color)';
  }

  // Diff mode functions
  function isDiffEligible(entry) {
    return entry.operation === 'WALK' || entry.operation === 'GETBULK';
  }

  function toggleDiffSelection(entry) {
    if (!isDiffEligible(entry)) return;
    if (diffSelectionA?.id === entry.id) {
      diffSelectionA = null;
    } else if (diffSelectionB?.id === entry.id) {
      diffSelectionB = null;
    } else if (!diffSelectionA) {
      diffSelectionA = entry;
    } else if (!diffSelectionB) {
      diffSelectionB = entry;
    }
  }

  function isDiffSelected(entry) {
    return diffSelectionA?.id === entry.id || diffSelectionB?.id === entry.id;
  }

  function openDiff() {
    if (diffSelectionA && diffSelectionB) {
      showDiffModal = true;
    }
  }

  function exitDiffMode() {
    diffMode = false;
    diffSelectionA = null;
    diffSelectionB = null;
  }
</script>

<div class="panel">
  <div class="header">
    <h3>{$_('history.title', { values: { count: $historyStore.length } })}</h3>
    <div class="header-actions">
      <button class="btn tertiary" class:active-diff={diffMode} on:click={() => diffMode ? exitDiffMode() : (diffMode = true)}>
        {diffMode ? $_('history.exitDiff') : $_('history.diffMode')}
      </button>
      {#if diffMode && diffSelectionA && diffSelectionB}
        <button class="btn" on:click={openDiff}>{$_('history.compare')}</button>
      {/if}
      <button class="btn tertiary" on:click={handleExport}>
        {$_('common.export')}
      </button>
      <button class="btn danger" on:click={handleClearHistory}>
        {$_('history.clearAll')}
      </button>
    </div>
  </div>

  <div class="filters">
    <div class="filter-group">
      <input
        type="text"
        placeholder={$_('history.searchPlaceholder')}
        bind:value={searchTerm}
      />
    </div>
    <div class="filter-row">
      <div class="filter-item">
        <label for="hist-op-filter">{$_('history.operationFilter')}</label>
        <select id="hist-op-filter" bind:value={operationFilter}>
          <option value="all">{$_('history.all')}</option>
          <option value="GET">GET</option>
          <option value="SET">SET</option>
          <option value="GETNEXT">GETNEXT</option>
          <option value="GETBULK">GETBULK</option>
          <option value="WALK">WALK</option>
        </select>
      </div>
      <div class="filter-item">
        <label for="hist-status-filter">{$_('history.statusFilter')}</label>
        <select id="hist-status-filter" bind:value={statusFilter}>
          <option value="all">{$_('history.all')}</option>
          <option value="success">{$_('common.success')}</option>
          <option value="error">{$_('common.error')}</option>
        </select>
      </div>
      <div class="filter-item">
        <span class="result-count">{$_('history.nResults', { values: { count: filteredHistory.length } })}</span>
      </div>
    </div>
  </div>

  <div class="history-container">
    {#if filteredHistory.length === 0}
      <p class="no-history">
        {#if searchTerm || operationFilter !== 'all' || statusFilter !== 'all'}
          {$_('history.noMatchFilter')}
        {:else}
          {$_('history.empty')}
        {/if}
      </p>
    {:else}
      {#each filteredHistory as entry (entry.id)}
        <div class="history-entry" class:expanded={expandedIds.has(entry.id)} class:diff-selected={diffMode && isDiffSelected(entry)}>
          <div class="entry-header" on:click={() => diffMode ? toggleDiffSelection(entry) : toggleExpand(entry.id)} on:keydown={(e) => e.key === 'Enter' && (diffMode ? toggleDiffSelection(entry) : toggleExpand(entry.id))} role="button" tabindex="0">
            {#if diffMode}
              <span class="diff-checkbox" class:disabled={!isDiffEligible(entry)}>
                {#if isDiffSelected(entry)}
                  {diffSelectionA?.id === entry.id ? 'A' : 'B'}
                {:else if isDiffEligible(entry)}
                  [ ]
                {:else}
                  -
                {/if}
              </span>
            {/if}
            <span class="chevron">{expandedIds.has(entry.id) ? '▼' : '▶'}</span>
            <span class="operation-icon">{getOperationIcon(entry.operation)}</span>
            <span class="operation-type">{entry.operation}</span>
            <span class="timestamp">{formatTimestamp(entry.timestamp)}</span>
            <span class="mib-name" title={entry.oid}>{getDisplayName(entry.oid)}</span>
            {#if getDisplayValue(entry)}
              <span class="value-display" title={getDisplayValue(entry)}>→ {getDisplayValue(entry)}</span>
            {/if}
            <span class="targets-count">{entry.targets?.length || 0} target(s)</span>
            <span class="duration">{formatDuration(entry.duration)}</span>
            <span class="status" style="color: {getStatusColor(entry.success)}">
              {entry.success ? '✓' : '✗'}
            </span>
            <button 
              class="btn-icon delete-btn" 
              on:click|stopPropagation={() => handleDeleteEntry(entry.id)}
              title={$_('history.deleteEntry')}
            >
              🗑️
            </button>
          </div>
          
          {#if expandedIds.has(entry.id)}
            <div class="entry-details">
              <div class="detail-row">
                <strong>{$_('nodeDetails.name')}</strong> <code class="mib-name-detail">{getDisplayName(entry.oid)}</code>
              </div>
              <div class="detail-row">
                <strong>{$_('nodeDetails.oid')}</strong> <code>{entry.oid}</code>
              </div>
              <div class="detail-row">
                <strong>{$_('history.targets')}:</strong> <code>{$anonMode ? entry.targets?.map(t => anonymizeIp(t)).join(', ') : entry.targets?.join(', ')}</code>
              </div>
              <div class="detail-row">
                <strong>{$_('common.version')}:</strong> <span>{entry.version}</span>
              </div>

              {#if entry.operation === 'SET'}
                <div class="detail-row">
                  <strong>{$_('common.value')}:</strong> <code>{entry.value}</code>
                </div>
                <div class="detail-row">
                  <strong>{$_('common.type')}:</strong> <span>{entry.valueType}</span>
                </div>
              {/if}

              {#if entry.operation === 'WALK' && entry.totalResults}
                <div class="detail-row">
                  <strong>{$_('common.results')}:</strong> <span>{entry.totalResults}</span>
                </div>
              {/if}

              {#if entry.error}
                <div class="detail-row error-row">
                  <strong>{$_('history.error')}:</strong> <span>{entry.error}</span>
                </div>
              {/if}

              {#if entry.results && entry.results.length > 0}
                <div class="results-summary">
                  <strong>{$_('common.results')}:</strong>
                  {#each entry.results as result}
                    <div class="result-item" class:has-error={result.error}>
                      <div><strong>{$anonMode ? anonymizeIp(result.target) : result.target}</strong></div>
                      {#if result.error}
                        <div class="error-text">{result.error}</div>
                      {:else if result.result?.type === 'WalkResponse'}
                        <div>{result.result.value?.length || 0} items</div>
                      {:else if result.result}
                        <div>{result.result.type}: {JSON.stringify(result.result.value)}</div>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    {/if}
  </div>
</div>

{#if showDiffModal && diffSelectionA && diffSelectionB}
  <DiffModal entryA={diffSelectionA} entryB={diffSelectionB} on:close={() => showDiffModal = false} />
{/if}

{#if showExportModal}
  <div class="modal-overlay" on:click={() => showExportModal = false} on:keydown={(e) => e.key === 'Escape' && (showExportModal = false)} role="button" tabindex="-1">
    <div class="modal" on:click|stopPropagation on:keydown|stopPropagation role="dialog">
      <h3>{$_('history.exportTitle')}</h3>
      <textarea readonly>{exportData}</textarea>
      <div class="modal-actions">
        <button class="btn" on:click={handleCopyExport}>{$_('history.copyClipboard')}</button>
        <button class="btn" on:click={handleDownloadExport}>{$_('history.downloadFile')}</button>
        <button class="btn tertiary" on:click={() => showExportModal = false}>{$_('common.close')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .panel {
    max-height: calc(100vh - 120px);
    display: flex;
    flex-direction: column;
  }

  .header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 15px;
  }

  .header h3 {
    margin: 0;
    font-size: 1.2em;
  }

  .header-actions {
    display: flex;
    gap: 10px;
  }

  .filters {
    margin-bottom: 15px;
  }

  .filter-group {
    margin-bottom: 10px;
  }

  .filter-group input {
    width: 100%;
    padding: 8px 10px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    box-sizing: border-box;
  }

  .filter-row {
    display: flex;
    gap: 15px;
    align-items: center;
  }

  .filter-item {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .filter-item label {
    font-size: 0.9em;
    font-weight: 500;
  }

  .filter-item select {
    padding: 6px 10px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
  }

  .result-count {
    margin-left: auto;
    color: var(--text-dimmed);
    font-size: 0.9em;
  }

  .history-container {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    border: 1px solid var(--border-color);
    border-radius: 4px;
  }

  .no-history {
    color: var(--text-muted);
    text-align: center;
    padding: 40px 20px;
  }

  .history-entry {
    border-bottom: 1px solid var(--border-color);
    background-color: var(--bg-lighter-color);
  }

  .history-entry:hover {
    background-color: var(--hover-overlay);
  }

  .entry-header {
    display: grid;
    grid-template-columns: 20px 30px 60px 140px minmax(150px, 1fr) minmax(100px, 200px) 80px 80px 30px 30px;
    align-items: center;
    gap: 10px;
    padding: 12px;
    cursor: pointer;
    user-select: none;
  }

  .chevron {
    color: var(--text-muted);
    font-size: 0.8em;
  }

  .operation-icon {
    font-size: 1.2em;
  }

  .operation-type {
    font-weight: 600;
    color: var(--accent-color);
  }

  .timestamp {
    font-size: 0.85em;
    color: var(--text-dimmed);
  }

  .mib-name {
    font-family: 'Courier New', monospace;
    font-size: 0.85em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--oid-color);
    font-weight: 500;
  }

  .value-display {
    font-size: 0.85em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--accent-color);
    font-weight: 600;
    background-color: var(--favorites-subtle);
    padding: 3px 8px;
    border-radius: 3px;
    border: 1px solid var(--favorites-border);
  }

  .targets-count, .duration {
    font-size: 0.85em;
    color: var(--text-dimmed);
  }

  .status {
    font-weight: bold;
    font-size: 1.2em;
  }

  .btn-icon {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 1em;
    padding: 4px;
    opacity: 0.6;
    transition: opacity 0.2s;
  }

  .btn-icon:hover {
    opacity: 1;
  }

  .entry-details {
    padding: 15px;
    background-color: var(--bg-color);
    border-top: 1px solid var(--border-color);
    font-size: 0.9em;
  }

  .detail-row {
    margin-bottom: 8px;
    display: flex;
    gap: 10px;
  }

  .detail-row strong {
    min-width: 120px;
    color: var(--text-dimmed);
  }

  .detail-row code {
    background-color: var(--bg-lighter-color);
    padding: 2px 6px;
    border-radius: 3px;
    font-family: 'Courier New', monospace;
  }

  .detail-row code.mib-name-detail {
    color: var(--oid-color);
    font-weight: 600;
    background-color: var(--oid-subtle);
  }

  .error-row {
    color: var(--error-color);
  }

  .results-summary {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border-color);
  }

  .result-item {
    margin-top: 8px;
    padding: 8px;
    background-color: var(--bg-lighter-color);
    border-radius: 4px;
    font-size: 0.85em;
  }

  .result-item.has-error {
    border-left: 3px solid var(--error-color);
  }

  .error-text {
    color: var(--error-color);
    margin-top: 4px;
  }

  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
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
    padding: 20px;
    min-width: 600px;
    max-width: 80vw;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
  }

  .modal h3 {
    margin-top: 0;
  }

  .modal textarea {
    flex: 1;
    min-height: 300px;
    margin: 15px 0;
    padding: 10px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    font-family: 'Courier New', monospace;
    font-size: 0.85em;
    resize: vertical;
  }

  .modal-actions {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
  }

  /* Diff mode styles */
  .active-diff {
    border-color: var(--accent-color) !important;
    color: var(--accent-color) !important;
    background-color: var(--accent-subtle-medium) !important;
  }

  .diff-checkbox {
    font-family: 'Courier New', monospace;
    font-weight: 700;
    font-size: 0.9em;
    width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 3px;
    background-color: var(--bg-color);
    border: 1px solid var(--border-color);
    color: var(--accent-color);
  }

  .diff-checkbox.disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .history-entry.diff-selected {
    border-left: 3px solid var(--accent-color);
    background-color: var(--accent-subtle-medium) !important;
  }
</style>
