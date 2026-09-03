<script>
  import { createEventDispatcher } from 'svelte';
  import { _ } from 'svelte-i18n';

  export let exportData = '';
  export let scope = 'all';        // bound: 'all' | 'filtered' | 'selected'
  export let format = 'json';      // bound: 'json' | 'csv'
  export let selectedCount = 0;    // disables the "selected" scope when 0

  const dispatch = createEventDispatcher();
  const close = () => dispatch('close');
</script>

<!-- Escape on the window, not on the overlay: a keydown starts at whatever
     has focus — a field inside the dialog — and the box below stops clicks,
     which used to stop keys with them. The overlay handler never ran. -->
<svelte:window on:keydown={(e) => {
  if (e.key !== 'Escape') return;
    close();
}} />

<div
  class="modal-overlay"
  on:click={close}
  role="button"
  tabindex="-1"
>
  <div class="modal" on:click|stopPropagation role="dialog">
    <h3>{$_('history.exportTitle')}</h3>
    <div class="export-options">
      <div class="export-opt-group">
        <span class="export-opt-label">{$_('history.exportScope')}</span>
        <div class="segmented">
          <button class="seg" class:active={scope === 'all'} on:click={() => scope = 'all'}>{$_('history.scopeAll')}</button>
          <button class="seg" class:active={scope === 'filtered'} on:click={() => scope = 'filtered'}>{$_('history.scopeFiltered')}</button>
          <button class="seg" class:active={scope === 'selected'} on:click={() => scope = 'selected'} disabled={selectedCount === 0}>{$_('history.scopeSelected')}</button>
        </div>
      </div>
      <div class="export-opt-group">
        <span class="export-opt-label">{$_('history.exportFormat')}</span>
        <div class="segmented">
          <button class="seg" class:active={format === 'json'} on:click={() => format = 'json'}>JSON</button>
          <button class="seg" class:active={format === 'csv'} on:click={() => format = 'csv'}>CSV</button>
        </div>
      </div>
    </div>
    <textarea readonly>{exportData}</textarea>
    <div class="modal-actions">
      <button class="btn" on:click={() => dispatch('copy')}>{$_('history.copyClipboard')}</button>
      <button class="btn" on:click={() => dispatch('download')}>{$_('history.downloadFile')}</button>
      <button class="btn tertiary" on:click={close}>{$_('common.close')}</button>
    </div>
  </div>
</div>

<style>
  .modal-overlay {
    position: fixed;
    inset: 0;
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

  .export-options {
    display: flex;
    flex-wrap: wrap;
    gap: 20px;
    margin-top: 12px;
  }

  .export-opt-group {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .export-opt-label {
    font-size: 0.85em;
    color: var(--text-dimmed);
  }

  .segmented {
    display: inline-flex;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    overflow: hidden;
  }

  .seg {
    padding: 5px 12px;
    font-size: 0.82em;
    background-color: var(--bg-lighter-color);
    border: none;
    border-right: 1px solid var(--border-color);
    color: var(--text-light);
    cursor: pointer;
  }

  .seg:last-child {
    border-right: none;
  }

  .seg:hover:not(:disabled):not(.active) {
    background-color: var(--hover-overlay);
  }

  .seg.active {
    background-color: var(--accent-color);
    color: var(--text-on-accent);
    font-weight: 600;
  }

  .seg:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
</style>
