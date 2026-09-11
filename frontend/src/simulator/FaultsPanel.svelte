<script>
  import { _ } from 'svelte-i18n';
  import { createEventDispatcher } from 'svelte';
  import Icon from '../Icon.svelte';
  import { COUNTER_SPEEDS, MAX_LATENCY_MS, blankFaults, editableFaults, faultsPayload } from '../utils/simulator.js';

  /**
   * What a device is made to do wrong. Applied at once and without a restart —
   * the device keeps the uptime and the counters a fault is tested against —
   * and kept with it. The modal applies; this asks.
   */
  /** The device's faults, as the list gives them. */
  export let faults = null;
  /** Keeps the fields' ids unique when more than one panel is open. */
  export let idPrefix = 'sim-faults';
  export let busy = false;

  const dispatch = createEventDispatcher();
  let form = editableFaults(faults);

  function clearAll() {
    form = blankFaults();
    dispatch('apply', faultsPayload(form));
  }
</script>

<div class="faults">
  <p class="hint"><Icon name="zap" size={14} /> {$_('simulator.faults.hint')}</p>
  <label class="check">
    <input type="checkbox" bind:checked={form.mute} /> {$_('simulator.faults.mute')}
  </label>
  <div class="fields">
    <div class="field">
      <label for="{idPrefix}-latency">{$_('simulator.faults.latency')}</label>
      <span class="inputs">
        <input id="{idPrefix}-latency" type="number" min="0" max={MAX_LATENCY_MS} step="50" bind:value={form.latencyMs} />
        <span class="unit">ms ±</span>
        <input id="{idPrefix}-jitter" type="number" min="0" max={MAX_LATENCY_MS} step="50" bind:value={form.jitterMs}
          aria-label={$_('simulator.faults.jitter')} title={$_('simulator.faults.jitter')} />
        <span class="unit">ms</span>
      </span>
    </div>
    <div class="field">
      <label for="{idPrefix}-loss">{$_('simulator.faults.loss')}</label>
      <span class="inputs">
        <input id="{idPrefix}-loss" type="number" min="0" max="100" bind:value={form.lossPercent} />
        <span class="unit">%</span>
      </span>
    </div>
    <div class="field">
      <label for="{idPrefix}-error">{$_('simulator.faults.error')}</label>
      <span class="inputs">
        <select id="{idPrefix}-error" bind:value={form.error}>
          <option value="">{$_('simulator.faults.errorNone')}</option>
          <option value="genErr">genErr</option>
          <option value="tooBig">tooBig</option>
        </select>
        {#if form.error}
          <input id="{idPrefix}-error-share" type="number" min="1" max="100" bind:value={form.errorPercent}
            aria-label={$_('simulator.faults.error')} />
          <span class="unit">%</span>
        {/if}
      </span>
    </div>
    <div class="field">
      <label for="{idPrefix}-counters">{$_('simulator.faults.counters')}</label>
      <span class="inputs">
        <select id="{idPrefix}-counters" bind:value={form.counterSpeed}>
          {#each COUNTER_SPEEDS as s (s)}<option value={s}>×{s}</option>{/each}
        </select>
      </span>
    </div>
  </div>
  <div class="foot">
    <button type="button" class="btn secondary btn-small" on:click={() => dispatch('close')}>{$_('common.close')}</button>
    <button type="button" class="btn secondary btn-small" disabled={busy} on:click={clearAll}>{$_('simulator.faults.clear')}</button>
    <button type="button" class="btn btn-small" disabled={busy} on:click={() => dispatch('apply', faultsPayload(form))}>
      {$_('simulator.faults.apply')}
    </button>
  </div>
</div>

<style>
  .faults {
    flex-basis: 100%;
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-top: 8px;
    padding: 10px 12px;
    background-color: var(--bg-color);
    border: 1px solid var(--warning-border);
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

  .check {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.9em;
    cursor: pointer;
  }

  .fields {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 10px 16px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .field label {
    font-size: 0.85em;
    color: var(--text-light);
  }

  .inputs {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .inputs input {
    width: 80px;
  }

  input,
  select {
    padding: 5px 8px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
  }

  .unit {
    font-size: 0.85em;
    color: var(--text-muted);
  }

  .foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
