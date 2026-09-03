<script>
  import { createEventDispatcher } from 'svelte';
  import Icon from './Icon.svelte';

  // Reusable confirmation modal. All user-facing strings are passed in already
  // translated by the caller, so this component stays i18n-agnostic.
  export let title = '';
  export let text = '';
  export let confirmLabel = 'OK';
  export let cancelLabel = 'Cancel';
  export let titleIcon = 'triangle-alert'; // Lucide icon name shown next to the title
  export let confirmIcon = null;           // optional icon on the confirm button
  export let danger = false;               // red confirm button when true

  const dispatch = createEventDispatcher();

  const cancel = () => dispatch('cancel');
  const confirm = () => dispatch('confirm');
</script>

<!-- Escape on the window, not on the overlay: a keydown starts at whatever
     has focus — a field inside the dialog — and the box below stops clicks,
     which used to stop keys with them. The overlay handler never ran. -->
<svelte:window on:keydown={(e) => {
  if (e.key !== 'Escape') return;
    cancel();
}} />

<div
  class="modal-overlay"
  on:click={cancel}
  role="button"
  tabindex="-1"
>
  <div class="confirm-dialog" on:click|stopPropagation role="dialog" tabindex="-1">
    <h3 class="confirm-title">
      {#if titleIcon}<Icon name={titleIcon} class="icon-warning" size={18} />{/if}
      {title}
    </h3>
    <p class="confirm-text">{text}</p>
    <div class="confirm-actions">
      <button class="btn secondary" on:click={cancel}>{cancelLabel}</button>
      <button class="btn" class:danger on:click={confirm}>
        {#if confirmIcon}<Icon name={confirmIcon} size={15} />{/if}
        {confirmLabel}
      </button>
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

  .confirm-dialog {
    background-color: var(--bg-light-color);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    box-shadow: 0 5px 20px var(--shadow-color-strong);
    padding: 20px;
    width: 90%;
    max-width: 420px;
  }

  .confirm-title {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 0 12px;
    font-size: 1.05em;
    color: var(--text-color);
  }

  .confirm-text {
    margin: 0 0 20px;
    color: var(--text-light);
    line-height: 1.5;
  }

  .confirm-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }

  .confirm-actions .btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
</style>
