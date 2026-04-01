<script>
  import { createEventDispatcher, onDestroy } from 'svelte';

  export let x;
  export let y;
  export let items; // [{ label: 'Copy OID', action: 'copyOid', disabled: false }]

  const dispatch = createEventDispatcher();

  function handleClick(item) {
    if (item.disabled) return;
    dispatch('action', { action: item.action });
    dispatch('close');
  }

  function close() {
    dispatch('close');
  }

  // Close menu on escape key
  function handleKeydown(e) {
    if (e.key === 'Escape') {
      close();
    }
  }

  // Add listeners to close the menu when clicking outside
  window.addEventListener('click', close);
  window.addEventListener('contextmenu', close, { capture: true });

  onDestroy(() => {
    window.removeEventListener('click', close);
    window.removeEventListener('contextmenu', close, { capture: true });
  });
</script>

<svelte:window on:keydown={handleKeydown}/>

<div class="context-menu" style="left: {x}px; top: {y}px;" on:click|stopPropagation on:keydown|stopPropagation role="menu" tabindex="-1">
  <ul>
    {#each items as item}
      {#if item.label === '---'}
        <li class="separator"></li>
      {:else}
        <li 
          class:disabled={item.disabled}
          on:click={() => handleClick(item)} 
          on:keydown|preventDefault|stopPropagation
          title={item.disabled ? item.disabledReason || 'This action is not available' : ''}
        >
          {item.label}
        </li>
      {/if}
    {/each}
  </ul>
</div>

<style>
  .context-menu {
    position: fixed;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    box-shadow: 0 5px 15px var(--shadow-color);
    z-index: 200;
    padding: 5px 0;
  }
  ul {
    list-style: none;
    padding: 0;
    margin: 0;
  }
  li {
    padding: 8px 15px;
    cursor: pointer;
    font-size: 0.9em;
    white-space: nowrap;
  }
  li:hover {
    background-color: var(--accent-color);
    color: white;
  }
  li.disabled {
    opacity: 0.4;
    cursor: not-allowed;
    color: var(--text-muted);
  }
  li.disabled:hover {
    background-color: transparent;
    color: var(--text-muted);
  }
  li.separator {
    padding: 0;
    margin: 5px 0;
    height: 1px;
    background-color: var(--border-color);
    cursor: default;
  }
  li.separator:hover {
    background-color: var(--border-color);
  }
</style>
