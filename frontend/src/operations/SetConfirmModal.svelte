<script>
  import { createEventDispatcher } from 'svelte';
  import { onBackdrop } from '../utils/modal';
  import { _ } from 'svelte-i18n';
  import Icon from '../Icon.svelte';

  export let oid = '';
  export let nodeName = '';
  export let value = '';
  export let type = '';
  export let targets = [];
  export let dontAsk = false; // bound

  const dispatch = createEventDispatcher();
</script>

<svelte:window on:keydown={(e) => e.key === 'Escape' && dispatch('cancel')} />

<div class="set-confirm-backdrop" on:mousedown={onBackdrop(() => dispatch('cancel'))} role="presentation">
  <div class="set-confirm-modal" role="dialog" aria-modal="true" tabindex="-1">
    <h3><Icon name="triangle-alert" class="icon-warning" size={18} /> {$_('operations.setConfirm.title')}</h3>
    <p class="set-confirm-warning">{$_('operations.setConfirm.message')}</p>
    <dl class="set-confirm-details">
      <dt>{$_('common.oid')}</dt>
      <dd>{oid}{nodeName ? ` (${nodeName})` : ''}</dd>
      <dt>{$_('common.value')}</dt>
      <dd>{value}</dd>
      <dt>{$_('common.type')}</dt>
      <dd>{type}</dd>
      <dt>{$_('operations.setConfirm.targets')}</dt>
      <dd>{targets.join(', ')}</dd>
    </dl>
    <label class="set-confirm-dontask">
      <input type="checkbox" bind:checked={dontAsk} />
      {$_('operations.setConfirm.dontAskAgain')}
    </label>
    <div class="set-confirm-actions">
      <button class="btn" on:click={() => dispatch('cancel')}>{$_('common.cancel')}</button>
      <button class="btn btn-primary" on:click={() => dispatch('confirm')}><Icon name="upload" /> {$_('operations.setConfirm.confirm')}</button>
    </div>
  </div>
</div>

<style>
  .set-confirm-backdrop {
    position: fixed;
    inset: 0;
    background: var(--backdrop-color-strong);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1100;
  }
  .set-confirm-modal {
    background-color: var(--bg-light-color);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    padding: 20px;
    width: min(480px, 90vw);
    box-shadow: 0 10px 40px var(--shadow-color-strong);
  }
  .set-confirm-modal h3 { margin: 0 0 8px; font-size: 1.1em; }
  .set-confirm-warning { margin: 0 0 14px; font-size: 0.88em; color: var(--text-muted); }
  .set-confirm-details {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 6px 14px;
    margin: 0 0 16px;
    padding: 12px;
    background-color: var(--bg-color);
    border-radius: 6px;
    font-size: 0.88em;
  }
  .set-confirm-details dt { color: var(--text-muted); font-weight: 600; }
  .set-confirm-details dd { margin: 0; font-family: 'Courier New', monospace; word-break: break-all; }
  .set-confirm-dontask {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 16px;
    font-size: 0.85em;
    color: var(--text-dimmed);
    cursor: pointer;
  }
  .set-confirm-dontask input { width: auto; cursor: pointer; }
  .set-confirm-actions { display: flex; justify-content: flex-end; gap: 10px; }
</style>
