<script>
  import { _ } from 'svelte-i18n';
  import { createEventDispatcher } from 'svelte';
  import Icon from '../Icon.svelte';
  import { CUSTOM_CATEGORIES } from '../utils/simulator.js';

  /**
   * Recording a real device as a model: which target, and what the model is
   * called. SnmpLens reaches the device with what that target uses; the modal
   * does the recording and says how far it has got.
   */
  /** The targets the settings list, offered first. */
  export let targets = [];
  /** The recording under way — { objects } — or null. */
  export let recording = null;

  const dispatch = createEventDispatcher();
  let target = targets[0] || '';
  let name = '';
  let vendor = '';
  let category = 'network';

  $: ready = target.trim() !== '' && name.trim() !== '';

  function submit() {
    if (!ready || recording) return;
    dispatch('record', { target: target.trim(), name: name.trim(), vendor: vendor.trim(), category });
  }
</script>

<form class="record" on:submit|preventDefault={submit}>
  <p class="hint"><Icon name="circle-dot" size={14} /> {$_('simulator.record.hint')}</p>
  <div class="fields">
    <div class="form-group">
      <label for="sim-rec-target">{$_('simulator.record.target')}</label>
      <input id="sim-rec-target" type="text" list="sim-rec-targets" spellcheck="false" autocomplete="off"
        disabled={!!recording} bind:value={target} />
      <datalist id="sim-rec-targets">
        {#each targets as t (t)}<option value={t}></option>{/each}
      </datalist>
    </div>
    <div class="form-group">
      <label for="sim-rec-name">{$_('simulator.record.name')}</label>
      <input id="sim-rec-name" type="text" maxlength="64" spellcheck="false" disabled={!!recording} bind:value={name} />
    </div>
    <div class="form-group">
      <label for="sim-rec-vendor">{$_('simulator.record.vendor')}</label>
      <input id="sim-rec-vendor" type="text" maxlength="64" spellcheck="false" disabled={!!recording} bind:value={vendor} />
    </div>
    <div class="form-group">
      <label for="sim-rec-category">{$_('simulator.record.category')}</label>
      <select id="sim-rec-category" disabled={!!recording} bind:value={category}>
        {#each CUSTOM_CATEGORIES as c (c)}<option value={c}>{$_(`simulator.category.${c}`)}</option>{/each}
      </select>
    </div>
  </div>
  <div class="foot">
    {#if recording}
      <span class="progress" role="status">
        <span class="spin"><Icon name="loader-circle" size={14} /></span>
        {$_('simulator.record.progress', { values: { count: recording.objects } })}
      </span>
      <button type="button" class="btn secondary btn-small" on:click={() => dispatch('stop')}>
        <Icon name="square" size={12} /> {$_('simulator.record.stop')}
      </button>
    {:else}
      <button type="button" class="btn secondary btn-small" on:click={() => dispatch('close')}>{$_('common.cancel')}</button>
      <button type="submit" class="btn btn-small" disabled={!ready}>
        <Icon name="circle-dot" size={13} /> {$_('simulator.record.start')}
      </button>
    {/if}
  </div>
</form>

<style>
  .record {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px 12px 12px;
    background-color: var(--bg-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
  }

  .hint {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin: 0;
    font-size: 0.82em;
    line-height: 1.4;
    color: var(--text-muted);
  }

  .fields {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px 16px;
    align-items: start;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    min-width: 0;
  }

  .form-group label {
    margin-bottom: 5px;
    font-size: 0.85em;
    color: var(--text-light);
  }

  .form-group input,
  .form-group select {
    width: 100%;
    padding: 7px 10px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
  }

  .foot {
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: 8px;
  }

  .foot .btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }

  .progress {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex: 1;
    font-size: 0.85em;
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }

  .spin {
    display: inline-flex;
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .spin {
      animation: none;
    }
  }

  @media (max-width: 640px) {
    .fields {
      grid-template-columns: 1fr;
    }
  }
</style>
