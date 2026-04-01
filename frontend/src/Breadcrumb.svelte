<script>
  import { createEventDispatcher } from 'svelte';

  export let selectedNode = null;

  const dispatch = createEventDispatcher();

  // Build the path from root to the selected node
  function buildPath(node) {
    if (!node) return [];
    
    const path = [];
    let current = node;
    
    // Walk up the tree using parent references
    while (current) {
      path.unshift({
        name: current.name,
        oid: current.oid,
        node: current
      });
      current = current.parent;
    }
    
    return path;
  }

  // Navigate to a node by dispatching an event
  function navigateToNode(crumb) {
    dispatch('navigate', { node: crumb.node });
  }

  $: breadcrumbPath = buildPath(selectedNode);
</script>

{#if breadcrumbPath.length > 0}
  <nav class="breadcrumb" aria-label="MIB tree breadcrumb navigation">
    <ol class="breadcrumb-list">
      {#each breadcrumbPath as crumb, index}
        <li class="breadcrumb-item">
          {#if index < breadcrumbPath.length - 1}
            <button 
              class="breadcrumb-link" 
              on:click={() => navigateToNode(crumb)}
              title="OID: {crumb.oid}"
            >
              {crumb.name}
            </button>
            <span class="breadcrumb-separator" aria-hidden="true">›</span>
          {:else}
            <span class="breadcrumb-current" title="OID: {crumb.oid}">
              {crumb.name}
            </span>
          {/if}
        </li>
      {/each}
    </ol>
  </nav>
{/if}

<style>
  .breadcrumb {
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    padding: 8px 12px;
    margin-bottom: 10px;
    overflow-x: auto;
    overflow-y: hidden;
    white-space: nowrap;
    font-size: 0.9em;
  }

  .breadcrumb-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .breadcrumb-item {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .breadcrumb-link {
    background: none;
    border: none;
    color: var(--accent-color);
    cursor: pointer;
    padding: 2px 6px;
    border-radius: 3px;
    transition: all 0.2s;
    font-size: inherit;
    font-family: inherit;
    text-decoration: none;
  }

  .breadcrumb-link:hover {
    background-color: var(--accent-color);
    color: white;
  }

  .breadcrumb-link:focus {
    outline: 2px solid var(--accent-color);
    outline-offset: 2px;
  }

  .breadcrumb-separator {
    color: var(--text-dimmed);
    user-select: none;
    font-size: 1.1em;
    padding: 0 2px;
  }

  .breadcrumb-current {
    color: var(--text-color);
    font-weight: 600;
    padding: 2px 6px;
    background-color: var(--bg-color);
    border-radius: 3px;
  }

  /* Scrollbar styling for horizontal overflow */
  .breadcrumb::-webkit-scrollbar {
    height: 4px;
  }

  .breadcrumb::-webkit-scrollbar-track {
    background: var(--bg-color);
    border-radius: 2px;
  }

  .breadcrumb::-webkit-scrollbar-thumb {
    background: var(--border-color);
    border-radius: 2px;
  }

  .breadcrumb::-webkit-scrollbar-thumb:hover {
    background: var(--bg-disabled-hover);
  }

  /* Responsive: Truncate long paths on small screens */
  @media (max-width: 768px) {
    .breadcrumb {
      font-size: 0.8em;
    }
  }
</style>
