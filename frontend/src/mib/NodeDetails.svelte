<script>
  import { copyToClipboard } from '../utils/clipboard';
  import { _ } from 'svelte-i18n';

  export let selectedNode = null;

  let collapsed = false;
</script>

{#if selectedNode}
  <div class="node-details" class:collapsed>
    <div class="node-details-header">
      <h4>📋 {$_('nodeDetails.title')}</h4>
      <button
        class="btn-collapse"
        on:click={() => collapsed = !collapsed}
        title={collapsed ? $_('nodeDetails.expand') : $_('nodeDetails.collapse')}
      >
        {collapsed ? '▲' : '▼'}
      </button>
    </div>

    {#if !collapsed}
      <div class="node-details-content">
        <div class="detail-row">
          <span class="detail-label">{$_('nodeDetails.name')}</span>
          <span class="detail-value">{selectedNode.name}</span>
        </div>

        <div class="detail-row">
          <span class="detail-label">{$_('nodeDetails.oid')}</span>
          <span class="detail-value mono">{selectedNode.oid}</span>
          <button
            class="btn-copy"
            on:click={() => copyToClipboard(selectedNode.oid, 'OID')}
            title={$_('common.copyOid')}
          >📋</button>
        </div>

        {#if selectedNode.mibType}
          <div class="detail-row">
            <span class="detail-label">{$_('nodeDetails.type')}</span>
            <span class="detail-value type-badge {selectedNode.mibType.toLowerCase()}">{selectedNode.mibType}</span>
          </div>
        {/if}

        {#if selectedNode.syntax}
          <div class="detail-row">
            <span class="detail-label">{$_('nodeDetails.syntax')}</span>
            <span class="detail-value">{selectedNode.syntax}</span>
          </div>
        {/if}

        {#if selectedNode.access}
          <div class="detail-row">
            <span class="detail-label">{$_('nodeDetails.access')}</span>
            <span class="detail-value access-badge {selectedNode.access.toLowerCase().replace('-', '')}">{selectedNode.access}</span>
          </div>
        {/if}

        {#if selectedNode.status}
          <div class="detail-row">
            <span class="detail-label">{$_('nodeDetails.status')}</span>
            <span class="detail-value">{selectedNode.status}</span>
          </div>
        {/if}

        {#if selectedNode.units}
          <div class="detail-row">
            <span class="detail-label">{$_('nodeDetails.units')}</span>
            <span class="detail-value">{selectedNode.units}</span>
          </div>
        {/if}

        {#if selectedNode.parent}
          <div class="detail-row">
            <span class="detail-label">{$_('nodeDetails.parent')}</span>
            <span class="detail-value">{selectedNode.parent.name}</span>
          </div>
        {/if}

        {#if selectedNode.children && selectedNode.children.length > 0}
          <div class="detail-row">
            <span class="detail-label">{$_('nodeDetails.children')}</span>
            <span class="detail-value">{$_('nodeDetails.nNodes', { values: { count: selectedNode.children.length } })}</span>
          </div>
        {/if}

        {#if selectedNode.description}
          <div class="detail-row description">
            <span class="detail-label">{$_('nodeDetails.description')}</span>
            <span class="detail-value">{selectedNode.description}</span>
          </div>
        {/if}
      </div>
    {/if}
  </div>
{/if}

<style>
  .node-details {
    margin-top: 10px;
    border: 1px solid var(--border-color);
    background-color: var(--bg-lighter-color);
    border-radius: 4px;
    font-size: 0.9em;
    transition: all 0.3s ease;
  }

  .node-details.collapsed {
    height: auto;
  }

  .node-details-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px;
    border-bottom: 1px solid var(--border-color);
    background-color: var(--favorites-subtle);
  }

  .node-details-header h4 {
    margin: 0;
    font-size: 1em;
    color: var(--text-color);
  }

  .btn-collapse {
    background: transparent;
    border: none;
    color: var(--text-color);
    font-size: 1.2em;
    cursor: pointer;
    padding: 4px 8px;
    border-radius: 4px;
    transition: background-color 0.2s;
  }

  .btn-collapse:hover {
    background-color: var(--hover-overlay-medium);
  }

  .node-details-content {
    padding: 10px;
    max-height: 400px;
    overflow-y: auto;
  }

  .detail-row {
    display: flex;
    gap: 10px;
    margin-bottom: 8px;
    align-items: flex-start;
  }

  .detail-row.description {
    flex-direction: column;
    gap: 5px;
  }

  .detail-label {
    font-weight: 600;
    color: var(--text-dimmed);
    min-width: 90px;
    flex-shrink: 0;
  }

  .detail-value {
    color: var(--text-color);
    word-break: break-word;
  }

  .detail-value.mono {
    font-family: 'Courier New', monospace;
    background-color: var(--bg-color);
    padding: 2px 6px;
    border-radius: 3px;
    font-size: 0.9em;
  }

  .btn-copy {
    background: transparent;
    border: none;
    cursor: pointer;
    padding: 2px 6px;
    font-size: 0.9em;
    opacity: 0.6;
    transition: opacity 0.2s;
    flex-shrink: 0;
  }

  .btn-copy:hover {
    opacity: 1;
  }

  /* Type badges */
  .type-badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 3px;
    font-size: 0.85em;
    font-weight: 600;
  }

  .type-badge.scalar {
    background-color: var(--accent-subtle-strong);
    color: var(--oid-color);
  }

  .type-badge.table {
    background-color: rgba(155, 89, 182, 0.2);
    color: #9b59b6;
  }

  .type-badge.column {
    background-color: var(--success-subtle-strong);
    color: var(--success-color);
  }

  .type-badge.notification {
    background-color: var(--favorites-subtle-strong);
    color: var(--favorites-color);
  }

  .type-badge.node, .type-badge.group {
    background-color: var(--hover-overlay-medium);
    color: var(--text-muted);
  }

  /* Access badges */
  .access-badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 3px;
    font-size: 0.85em;
    font-weight: 600;
  }

  .access-badge.readonly {
    background-color: var(--accent-subtle-strong);
    color: var(--oid-color);
  }

  .access-badge.readwrite {
    background-color: var(--error-subtle-strong);
    color: var(--error-color);
  }

  .access-badge.writeonly {
    background-color: var(--warning-subtle);
    color: var(--warning-color);
  }

  .access-badge.notaccessible {
    background-color: var(--hover-overlay-medium);
    color: var(--text-muted);
  }
</style>
