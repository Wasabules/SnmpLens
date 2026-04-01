<script>
  import { createEventDispatcher } from 'svelte';
  import { _ } from 'svelte-i18n';

  export let node;
  export let onNodeClick;
  export let selectedNode;
  export let compactMode = false;
  export let searchTerm = '';

  const dispatch = createEventDispatcher();

  let hoverTimeout;

  function toggle() {
    if (compactMode && !node.expanded) {
      // In compact mode, when expanding, auto-expand all single-child nodes
      expandCompactPath(node);
    } else {
      node.expanded = !node.expanded;
    }
    onNodeClick(node);
  }

  // Auto-expand all nodes in the compact path
  function expandCompactPath(startNode) {
    startNode.expanded = true;
    let current = startNode;
    
    // Continue expanding while node has exactly one child
    while (current.children && current.children.length === 1) {
      current = current.children[0];
      current.expanded = true;
    }
  }

  function handleContextMenu(event) {
    event.preventDefault();
    event.stopPropagation(); // Prevent bubbling to parent nodes
    dispatch('contextmenu', { node, x: event.clientX, y: event.clientY });
  }

  function handleDblClick(event) {
    event.stopPropagation();
    // Only trigger GET on leaf nodes (no children = scalar OID)
    dispatch('dblclicknode', { node });
  }

  // Tooltip handlers - dispatch events to parent
  function handleMouseEnter(event) {
    event.stopPropagation(); // Prevent bubbling to parent nodes
    clearTimeout(hoverTimeout);
    hoverTimeout = setTimeout(() => {
      dispatch('showtooltip', { 
        node, 
        x: event.clientX + 15, 
        y: event.clientY + 15 
      });
    }, 500); // 500ms delay before showing tooltip
  }

  function handleMouseLeave(event) {
    event.stopPropagation(); // Prevent bubbling to parent nodes
    clearTimeout(hoverTimeout);
    dispatch('hidetooltip');
  }

  function handleMouseMove(event) {
    // Update tooltip position if it's showing
    event.stopPropagation();
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

  // Build compact path: traverse single-child nodes and return array of nodes
  function buildCompactPath(startNode) {
    const path = [startNode];
    let current = startNode;
    
    // Continue while node has exactly one child
    while (current.children && current.children.length === 1) {
      current = current.children[0];
      path.push(current);
    }
    
    return path;
  }

  // Get the last node in compact path (the one with multiple children or no children)
  function getCompactEndNode(startNode) {
    const path = buildCompactPath(startNode);
    return path[path.length - 1];
  }

  $: matchedField = searchTerm ? getMatchedField(node, searchTerm.toLowerCase()) : null;

  function getMatchedField(node, term) {
    if (node.name.toLowerCase().includes(term)) return null;
    if (node.oid.includes(term)) return null;
    if (node.description && node.description.toLowerCase().includes(term)) {
      return { field: 'description', snippet: truncateAround(node.description, term, 80) };
    }
    if (node.syntax && node.syntax.toLowerCase().includes(term)) {
      return { field: 'syntax', value: node.syntax };
    }
    if (node.enumValues) {
      for (const name of Object.keys(node.enumValues)) {
        if (name.toLowerCase().includes(term)) {
          return { field: 'enum', value: name };
        }
      }
    }
    return null;
  }

  function truncateAround(text, term, maxLen) {
    const idx = text.toLowerCase().indexOf(term);
    if (idx === -1) return text.slice(0, maxLen) + '...';
    const start = Math.max(0, idx - 30);
    const end = Math.min(text.length, idx + term.length + 50);
    let snippet = text.slice(start, end);
    if (start > 0) snippet = '...' + snippet;
    if (end < text.length) snippet = snippet + '...';
    return snippet;
  }

  $: isSelected = selectedNode && selectedNode.oid === node.oid;
  $: compactPath = compactMode ? buildCompactPath(node) : [node];
  $: endNode = compactMode ? getCompactEndNode(node) : node;
  $: hasMultipleChildrenAtEnd = endNode.children && endNode.children.length > 1;
</script>

<div 
  class="tree-node" 
  class:selected={isSelected}
  on:contextmenu={handleContextMenu}
  on:mouseenter={handleMouseEnter}
  on:mouseleave={handleMouseLeave}
  on:mousemove={handleMouseMove}
  data-oid={node.oid}
  role="treeitem"
  aria-selected={isSelected}
  tabindex="-1"
>
  <span
    role="button"
    tabindex="0"
    on:click={toggle}
    on:dblclick={handleDblClick}
    on:keydown={(e) => e.key === 'Enter' && toggle()}
    class="node-label"
  >
    {#if node.children && node.children.length > 0}
      <span class="icon">{node.expanded ? '▼' : '►'}</span>
    {:else}
      <span class="icon leaf">●</span>
    {/if}
    <span class="icon-visual" title={node.mibType}>{getNodeIcon(node.mibType)}</span>
    
    {#if compactMode && compactPath.length > 1}
      <!-- Compact mode: show full path -->
      <span class="compact-path">
        {#each compactPath as pathNode, i}
          <span class="path-segment" class:path-end={i === compactPath.length - 1}>
            {pathNode.name}
          </span>
          {#if i < compactPath.length - 1}
            <span class="path-separator">.</span>
          {/if}
        {/each}
      </span>
      {#if hasMultipleChildrenAtEnd}
        <span class="compact-hint" title={$_('mib.nChildren', { values: { count: endNode.children.length } })}>({endNode.children.length})</span>
      {/if}
    {:else}
      <!-- Normal mode: show single node name -->
      {node.name}
    {/if}
    {#if matchedField}
      <span class="match-snippet">
        {matchedField.field}: {matchedField.snippet || matchedField.value}
      </span>
    {/if}
  </span>
  {#if node.expanded && node.children}
    <div class="tree-children">
      {#if compactMode && compactPath.length > 1}
        <!-- In compact mode, show children of the END node (skip intermediate single-child nodes) -->
        {#if endNode.children}
          {#each endNode.children as child}
            <svelte:self node={child} {onNodeClick} {compactMode} {searchTerm} bind:selectedNode on:contextmenu on:dblclicknode on:showtooltip on:hidetooltip />
          {/each}
        {/if}
      {:else}
        <!-- Normal mode: show direct children -->
        {#each node.children as child}
          <svelte:self node={child} {onNodeClick} {compactMode} {searchTerm} bind:selectedNode on:contextmenu on:dblclicknode on:showtooltip on:hidetooltip />
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .tree-node {
    padding: 2px 5px;
    cursor: pointer;
    border-radius: 4px;
    transition: background-color 0.2s;
  }
  .node-label {
    display: flex;
    align-items: center;
  }
  .tree-node:hover {
    background-color: var(--hover-overlay-medium);
  }
  .tree-node.selected {
    background-color: var(--accent-color);
    color: white;
  }
  .tree-children {
    padding-left: 20px;
    border-left: 1px solid var(--border-color);
    margin-left: 7px;
  }
  .icon {
    width: 15px;
    display: inline-block;
    margin-right: 5px;
    font-size: 0.8em;
    color: var(--text-dimmed);
  }
  .icon.leaf {
    font-size: 0.6em;
  }
  .tree-node.selected .icon {
    color: white;
  }

  /* Visual icons (emojis) */
  .icon-visual {
    font-size: 1.1em;
    margin-right: 6px;
    line-height: 1;
    display: inline-block;
    min-width: 20px;
    text-align: center;
  }

  /* Compact mode styles */
  .compact-path {
    display: inline;
  }

  .path-segment {
    color: var(--text-dimmed);
  }

  .path-segment.path-end {
    color: var(--text-color);
    font-weight: 500;
  }

  .tree-node.selected .path-segment {
    color: var(--text-on-dark);
  }

  .tree-node.selected .path-segment.path-end {
    color: white;
    font-weight: 600;
  }

  .path-separator {
    color: var(--text-dimmed);
    margin: 0 2px;
    font-weight: 300;
  }

  .tree-node.selected .path-separator {
    color: var(--text-on-dark-dim);
  }

  .compact-hint {
    margin-left: 6px;
    font-size: 0.85em;
    color: var(--text-muted);
    font-style: italic;
  }

  .tree-node.selected .compact-hint {
    color: var(--text-on-dark);
  }

  .match-snippet {
    display: block;
    font-size: 0.75em;
    color: var(--text-muted);
    font-style: italic;
    margin-top: 1px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100%;
  }
</style>