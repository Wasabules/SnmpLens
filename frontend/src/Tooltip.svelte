<script>
  import { onMount, onDestroy } from 'svelte';
  import { _ } from 'svelte-i18n';

  /** @type {any} */
  export let node = null;
  export let x = 0;
  export let y = 0;
  export let visible = false;

  let tooltipElement;
  let adjustedX = x;
  let adjustedY = y;

  // Adjust position to keep tooltip within viewport
  function adjustPosition() {
    if (!tooltipElement) return;

    const rect = tooltipElement.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;

    adjustedX = x;
    adjustedY = y;

    // Adjust horizontal position
    if (x + rect.width > viewportWidth - 10) {
      adjustedX = viewportWidth - rect.width - 10;
    }
    if (adjustedX < 10) {
      adjustedX = 10;
    }

    // Adjust vertical position
    if (y + rect.height > viewportHeight - 10) {
      adjustedY = y - rect.height - 10;
    }
    if (adjustedY < 10) {
      adjustedY = 10;
    }
  }

  $: if (visible && tooltipElement) {
    setTimeout(adjustPosition, 0);
  }

  // Get icon for node type
  function getNodeIcon(type) {
    const icons = {
      'Scalar': '📊',
      'Column': '📑',
      'Table': '📋',
      'Notification': '🔔',
      'Node': '📁',
      'Group': '📦',
    };
    return icons[type] || '📄';
  }

  // Get color for node type
  function getTypeColor(type) {
    const colors = {
      'Scalar': '#3b82f6',      // Blue
      'Column': '#16a34a',      // Green
      'Table': '#f97316',       // Orange
      'Notification': '#ef4444', // Red
      'Node': '#6b7280',        // Gray
      'Group': '#6b7280',       // Gray
    };
    return colors[type] || '#6b7280';
  }

  // Format access type
  function formatAccess(node) {
    if (node.access && node.access !== 'AccessUnknown' && node.access !== 'Unknown') {
      return node.access;
    }
    // Fallback for nodes without access information
    if (node.mibType === 'Table') return 'not-accessible';
    if (node.mibType === 'Column') return 'read-only';
    return null;
  }

  // Get syntax/type
  function getSyntax(node) {
    if (node.syntax) return node.syntax;
    if (node.type) return node.type;
    return null;
  }
</script>

{#if visible && node}
  <div 
    class="tooltip" 
    bind:this={tooltipElement}
    style="left: {adjustedX}px; top: {adjustedY}px;"
  >
    <div class="tooltip-header">
      <span class="tooltip-icon" style="color: {getTypeColor(node.mibType)}">
        {getNodeIcon(node.mibType)}
      </span>
      <span class="tooltip-name">{node.name}</span>
    </div>
    
    <div class="tooltip-body">
      <div class="tooltip-row">
        <span class="tooltip-label">{$_('tooltip.oid')}</span>
        <span class="tooltip-value oid">{node.oid}</span>
      </div>

      <div class="tooltip-row">
        <span class="tooltip-label">{$_('tooltip.type')}</span>
        <span class="tooltip-value" style="color: {getTypeColor(node.mibType)}">
          {node.mibType}
        </span>
      </div>

      {#if getSyntax(node)}
        <div class="tooltip-row">
          <span class="tooltip-label">{$_('tooltip.syntax')}</span>
          <span class="tooltip-value">{getSyntax(node)}</span>
        </div>
      {/if}

      <div class="tooltip-row">
        <span class="tooltip-label">{$_('tooltip.access')}</span>
        <span class="tooltip-value access">{formatAccess(node) || $_('tooltip.na')}</span>
      </div>

      {#if node.enumValues && Object.keys(node.enumValues).length > 0}
        <div class="tooltip-row">
          <span class="tooltip-label">{$_('tooltip.values')}</span>
          <span class="tooltip-value enum-values">
            {#each Object.entries(node.enumValues) as [name, value], i}
              {#if i > 0}, {/if}<span class="enum-item">{name}({value})</span>
            {/each}
          </span>
        </div>
      {/if}
      
      {#if node.description}
        <div class="tooltip-description">
          <span class="tooltip-label">{$_('tooltip.description')}</span>
          <p>{node.description}</p>
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .tooltip {
    position: fixed;
    background-color: var(--bg-light-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 0;
    box-shadow: 0 4px 12px var(--shadow-color-intense);
    z-index: 10000;
    max-width: 350px;
    font-size: 0.85em;
    pointer-events: none;
    animation: fadeIn 0.15s ease-out;
  }

  @keyframes fadeIn {
    from {
      opacity: 0;
      transform: translateY(-5px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .tooltip-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    background-color: var(--bg-lighter-color);
    border-bottom: 1px solid var(--border-color);
    border-radius: 6px 6px 0 0;
  }

  .tooltip-icon {
    font-size: 1.3em;
    line-height: 1;
  }

  .tooltip-name {
    font-weight: 600;
    color: var(--text-color);
    font-size: 1.05em;
  }

  .tooltip-body {
    padding: 10px 12px;
  }

  .tooltip-row {
    display: flex;
    margin-bottom: 6px;
    gap: 8px;
  }

  .tooltip-row:last-child {
    margin-bottom: 0;
  }

  .tooltip-label {
    color: var(--text-muted);
    font-weight: 500;
    min-width: 65px;
    flex-shrink: 0;
  }

  .tooltip-value {
    color: var(--text-color);
    word-break: break-all;
  }

  .tooltip-value.oid {
    font-family: 'Courier New', monospace;
    font-size: 0.95em;
    color: var(--oid-color);
  }

  .tooltip-value.access {
    text-transform: lowercase;
    font-style: italic;
  }

  .tooltip-value.enum-values {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .enum-item {
    background-color: var(--border-color);
    padding: 2px 6px;
    border-radius: 3px;
    font-size: 0.9em;
    color: var(--favorites-color);
    font-family: 'Courier New', monospace;
  }

  .tooltip-description {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border-color);
  }

  .tooltip-description .tooltip-label {
    display: block;
    margin-bottom: 4px;
  }

  .tooltip-description p {
    margin: 0;
    color: var(--text-light);
    line-height: 1.4;
    font-size: 0.95em;
    max-height: 100px;
    overflow-y: auto;
  }

  /* Scrollbar for long descriptions */
  .tooltip-description p::-webkit-scrollbar {
    width: 4px;
  }

  .tooltip-description p::-webkit-scrollbar-track {
    background: var(--bg-light-color);
  }

  .tooltip-description p::-webkit-scrollbar-thumb {
    background: var(--bg-disabled);
    border-radius: 2px;
  }

  .tooltip-description p::-webkit-scrollbar-thumb:hover {
    background: var(--bg-disabled-hover);
  }
</style>
