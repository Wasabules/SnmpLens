<script>
  import { queriesStore } from '../stores/queriesStore';
  import { notificationStore } from '../stores/notifications';
  import { createEventDispatcher } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';

  export let activeOperation;
  export let snmpGetOid;
  export let snmpSetOid;
  export let snmpGetNextOid;
  export let snmpGetBulkOid;
  export let snmpWalkOid;
  export let snmpSetValue;
  export let snmpSetType;
  export let maxRepetitions;
  export let nonRepeaters;

  const dispatch = createEventDispatcher();

  let showSavedQueries = false;
  let saveQueryName = '';
  let showSaveInput = false;

  function saveCurrentQuery() {
    const oid = activeOperation === 'GET' ? snmpGetOid
      : activeOperation === 'SET' ? snmpSetOid
      : activeOperation === 'GETNEXT' ? snmpGetNextOid
      : activeOperation === 'GETBULK' ? snmpGetBulkOid
      : snmpWalkOid;

    const name = saveQueryName.trim() || `${activeOperation} ${oid}`;
    const query = {
      name,
      operation: activeOperation,
      oid,
      params: {}
    };
    if (activeOperation === 'SET') {
      query.params.value = snmpSetValue;
      query.params.type = snmpSetType;
    } else if (activeOperation === 'GETBULK') {
      query.params.maxRepetitions = maxRepetitions;
      query.params.nonRepeaters = nonRepeaters;
    }
    queriesStore.add(query);
    saveQueryName = '';
    showSaveInput = false;
    const t = get(_);
    notificationStore.add(t('savedQueries.saved', { values: { name } }), 'success');
  }

  function loadQuery(q) {
    dispatch('loadQuery', q);
    queriesStore.markUsed(q.id);
    showSavedQueries = false;
    const t = get(_);
    notificationStore.add(t('savedQueries.loaded', { values: { name: q.name } }), 'info');
  }
</script>

<!-- Saved Queries Bar -->
<div class="saved-queries-bar">
  <div class="queries-left">
    <button class="btn-queries" on:click={() => showSavedQueries = !showSavedQueries}>
      {showSavedQueries ? $_('savedQueries.hide') : $_('savedQueries.title')} ({$queriesStore.length})
    </button>
    {#if !showSaveInput}
      <button class="btn-save-query" on:click={() => showSaveInput = true} title={$_('savedQueries.save')}>
        {$_('savedQueries.save')}
      </button>
    {:else}
      <div class="save-query-input">
        <input
          type="text"
          bind:value={saveQueryName}
          placeholder={$_('savedQueries.namePlaceholder')}
          on:keydown={(e) => e.key === 'Enter' && saveCurrentQuery()}
        />
        <button class="btn-save-confirm" on:click={saveCurrentQuery}>{$_('common.save')}</button>
        <button class="btn-save-cancel" on:click={() => { showSaveInput = false; saveQueryName = ''; }}>{$_('common.cancel')}</button>
      </div>
    {/if}
  </div>
</div>

{#if showSavedQueries && $queriesStore.length > 0}
  <div class="saved-queries-list">
    {#each $queriesStore as q (q.id)}
      <div class="saved-query-item">
        <button class="query-load-btn" on:click={() => loadQuery(q)}>
          <span class="query-op">{q.operation}</span>
          <span class="query-name">{q.name}</span>
          <span class="query-oid">{q.oid}</span>
        </button>
        <button class="query-delete-btn" on:click={() => queriesStore.remove(q.id)} title={$_('savedQueries.deleteQuery')}>x</button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .saved-queries-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 10px;
    gap: 8px;
  }

  .queries-left {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .btn-queries {
    padding: 5px 12px;
    font-size: 0.85em;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    color: var(--text-color);
    border-radius: 4px;
    cursor: pointer;
    font-weight: 500;
  }

  .btn-queries:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }

  .btn-save-query {
    padding: 5px 10px;
    font-size: 0.85em;
    background-color: transparent;
    border: 1px dashed var(--border-color);
    color: var(--text-muted);
    border-radius: 4px;
    cursor: pointer;
  }

  .btn-save-query:hover {
    border-color: var(--success-color);
    color: var(--success-color);
  }

  .save-query-input {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .save-query-input input {
    padding: 4px 8px;
    font-size: 0.85em;
    width: 180px;
    flex-grow: 0;
  }

  .btn-save-confirm {
    padding: 4px 10px;
    font-size: 0.8em;
    background-color: var(--success-color);
    border: none;
    color: white;
    border-radius: 3px;
    cursor: pointer;
  }

  .btn-save-cancel {
    padding: 4px 10px;
    font-size: 0.8em;
    background-color: transparent;
    border: 1px solid var(--border-color);
    color: var(--text-muted);
    border-radius: 3px;
    cursor: pointer;
  }

  .saved-queries-list {
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 8px;
    margin-bottom: 10px;
    max-height: 200px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .saved-query-item {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .query-load-btn {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    background-color: var(--bg-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    cursor: pointer;
    text-align: left;
    font-size: 0.85em;
    transition: all 0.15s;
  }

  .query-load-btn:hover {
    border-color: var(--accent-color);
    background-color: var(--accent-subtle-medium);
  }

  .query-op {
    font-weight: 600;
    color: var(--accent-color);
    font-size: 0.8em;
    padding: 1px 6px;
    background-color: var(--accent-subtle-strong);
    border-radius: 3px;
    flex-shrink: 0;
  }

  .query-name {
    font-weight: 500;
    flex-shrink: 0;
  }

  .query-oid {
    font-family: 'Courier New', monospace;
    color: var(--text-muted);
    font-size: 0.85em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .query-delete-btn {
    padding: 4px 8px;
    font-size: 0.8em;
    background-color: transparent;
    border: 1px solid transparent;
    color: var(--text-dimmed);
    border-radius: 3px;
    cursor: pointer;
    flex-shrink: 0;
  }

  .query-delete-btn:hover {
    border-color: var(--error-color);
    color: var(--error-color);
    background-color: var(--error-subtle-medium);
  }
</style>
