<script>
  import { historyStore } from '../stores/historyStore';
  import { mibStore } from '../stores/mibStore';
  import { findMibNameByOid } from '../utils/mibTree';
  import { _ } from 'svelte-i18n';

  // Get display name for history entry (MIB name or OID)
  function getHistoryDisplayName(entry) {
    const mibName = findMibNameByOid(entry.oid, $mibStore.tree);
    return mibName || entry.oid;
  }

  // Extract value from history entry results
  function getHistoryValue(entry) {
    if (!entry.results || entry.results.length === 0) return null;

    // For GET/SET/GETNEXT operations, get the first result
    if (entry.operation === 'GET' || entry.operation === 'SET' || entry.operation === 'GETNEXT') {
      const firstResult = entry.results[0];
      if (firstResult?.result?.value !== undefined) {
        return firstResult.result.value;
      }
    }

    // For WALK/GETBULK operations, show count
    if ((entry.operation === 'WALK' || entry.operation === 'GETBULK') && entry.totalResults) {
      return $_('recentHistory.nResults', { values: { count: entry.totalResults } });
    }

    return null;
  }
</script>

<!-- Recent History -->
<div class="history-section">
  <div class="history-header">
    <h4>📜 {$_('recentHistory.title')}</h4>
    <span class="history-count">{$_('recentHistory.recent', { values: { count: $historyStore.slice(0, 5).length } })}</span>
  </div>
  {#if $historyStore.length > 0}
    <div class="history-list">
      {#each $historyStore.slice(0, 5) as entry}
        <div class="history-item" class:success={entry.success} class:error={!entry.success}>
          <div class="history-item-header">
            <span class="history-operation">{entry.operation}</span>
            <span class="history-status">{entry.success ? '✅' : '❌'}</span>
            <span class="history-time">{new Date(entry.timestamp).toLocaleTimeString()}</span>
          </div>
          <div class="history-item-main">
            <span class="history-mib-name" title={entry.oid}>{getHistoryDisplayName(entry)}</span>
            {#if getHistoryValue(entry) !== null}
              <span class="history-value">→ {getHistoryValue(entry)}</span>
            {/if}
          </div>
          <div class="history-item-details">
            <span class="history-targets">🎯 {entry.targets.length} target(s)</span>
            {#if entry.duration}
              <span class="history-duration">⏱️ {entry.duration}ms</span>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {:else}
    <p class="no-history">{$_('recentHistory.empty')}</p>
  {/if}
</div>

<style>
  .history-section {
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 12px;
    margin-top: 15px;
  }

  .history-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 10px;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--border-color);
  }

  .history-header h4 {
    margin: 0;
    font-size: 1em;
    color: var(--text-color);
  }

  .history-count {
    font-size: 0.85em;
    color: var(--text-muted);
    background-color: var(--bg-color);
    padding: 3px 8px;
    border-radius: 12px;
  }

  .history-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: 250px;
    overflow-y: auto;
  }

  .history-item {
    padding: 10px;
    border-radius: 4px;
    border: 1px solid var(--border-color);
    background-color: var(--bg-color);
    transition: all 0.2s;
  }

  .history-item:hover {
    background-color: var(--hover-overlay);
  }

  .history-item.success {
    border-left: 3px solid var(--success-color);
  }

  .history-item.error {
    border-left: 3px solid var(--error-color);
  }

  .history-item-header {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 6px;
  }

  .history-operation {
    font-weight: 600;
    color: var(--accent-color);
    font-size: 0.9em;
    padding: 2px 8px;
    background-color: var(--accent-subtle-strong);
    border-radius: 3px;
  }

  .history-status {
    font-size: 1em;
  }

  .history-time {
    font-size: 0.85em;
    color: var(--text-muted);
    margin-left: auto;
  }

  .history-item-main {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 6px;
    flex-wrap: wrap;
  }

  .history-mib-name {
    font-family: 'Courier New', monospace;
    color: var(--oid-color);
    font-weight: 500;
    font-size: 0.95em;
    background-color: var(--oid-subtle);
    padding: 3px 8px;
    border-radius: 3px;
    cursor: help;
  }

  .history-value {
    font-weight: 600;
    color: var(--accent-color);
    background-color: var(--favorites-subtle-strong);
    padding: 3px 10px;
    border-radius: 3px;
    border: 1px solid var(--favorites-border);
    font-size: 0.9em;
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .history-item-details {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    font-size: 0.85em;
  }

  .history-targets,
  .history-duration {
    color: var(--text-dimmed);
    background-color: var(--hover-overlay);
    padding: 2px 6px;
    border-radius: 3px;
  }

  .no-history {
    text-align: center;
    color: var(--text-muted);
    font-style: italic;
    padding: 20px;
  }

  /* Scrollbar for history list */
  .history-list::-webkit-scrollbar {
    width: 6px;
  }

  .history-list::-webkit-scrollbar-track {
    background: var(--bg-color);
    border-radius: 3px;
  }

  .history-list::-webkit-scrollbar-thumb {
    background: var(--border-color);
    border-radius: 3px;
  }

  .history-list::-webkit-scrollbar-thumb:hover {
    background: var(--bg-disabled-hover);
  }
</style>
