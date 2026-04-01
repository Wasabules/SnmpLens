<script>
  import { createEventDispatcher } from 'svelte';
  import { _ } from 'svelte-i18n';

  export let visible = false;

  const dispatch = createEventDispatcher();
</script>

{#if visible}
  <div class="keyboard-help">
    <div class="keyboard-help-header">
      <h4>⌨️ {$_('keyboard.title')}</h4>
      <button class="close-btn" on:click={() => dispatch('close')}>✕</button>
    </div>
    <div class="shortcuts-grid">
      <div class="shortcut">
        <kbd>↑</kbd> <span>{$_('keyboard.previousNode')}</span>
      </div>
      <div class="shortcut">
        <kbd>↓</kbd> <span>{$_('keyboard.nextNode')}</span>
      </div>
      <div class="shortcut">
        <kbd>←</kbd> <span>{$_('keyboard.collapseOrParent')}</span>
      </div>
      <div class="shortcut">
        <kbd>→</kbd> <span>{$_('keyboard.expandOrChild')}</span>
      </div>
      <div class="shortcut">
        <kbd>Enter</kbd> <span>{$_('keyboard.toggleExpand')}</span>
      </div>
      <div class="shortcut">
        <kbd>Home</kbd> <span>{$_('keyboard.goToFirst')}</span>
      </div>
      <div class="shortcut">
        <kbd>End</kbd> <span>{$_('keyboard.goToLast')}</span>
      </div>
      <div class="shortcut">
        <kbd>/</kbd> <span>{$_('keyboard.focusSearch')}</span>
      </div>
      <div class="shortcut">
        <kbd>?</kbd> <span>{$_('keyboard.toggleHelp')}</span>
      </div>
    </div>
  </div>
{/if}

<style>
  .keyboard-help {
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--accent-color);
    border-radius: 4px;
    padding: 12px;
    margin-bottom: 10px;
    font-size: 0.85em;
    box-shadow: 0 2px 8px var(--shadow-color);
  }

  .keyboard-help-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 10px;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--border-color);
  }

  .keyboard-help-header h4 {
    margin: 0;
    font-size: 1em;
    color: var(--text-color);
  }

  .close-btn {
    background: none;
    border: none;
    color: var(--text-color);
    cursor: pointer;
    font-size: 1.2em;
    padding: 0 4px;
    transition: color 0.2s;
  }

  .close-btn:hover {
    color: var(--accent-color);
  }

  .shortcuts-grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 6px;
  }

  .shortcut {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .shortcut kbd {
    background-color: var(--bg-color);
    border: 1px solid var(--border-color);
    border-radius: 3px;
    padding: 3px 8px;
    font-family: monospace;
    font-size: 0.9em;
    min-width: 50px;
    text-align: center;
    box-shadow: 0 1px 2px var(--shadow-color);
  }

  .shortcut span {
    color: var(--text-dimmed);
  }
</style>
