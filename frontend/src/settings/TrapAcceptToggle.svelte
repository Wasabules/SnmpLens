<script>
  import { _ } from 'svelte-i18n';
  import Icon from '../Icon.svelte';

  /**
   * Whether the trap listener accepts SNMPv3 notifications from one user — the
   * default one or a profile's. Ticked by default: opting a user out is
   * something written on purpose.
   */
  export let checked = true;
  /** What unticking means here; the default user and a profile say it differently. */
  export let hint = '';
</script>

<label class="trap-accept">
  <input type="checkbox" bind:checked />
  <span class="text">
    <span class="title"><Icon name="radio" size={13} /> {$_('profiles.acceptTraps')}</span>
    {#if hint}<span class="hint">{hint}</span>{/if}
  </span>
</label>

<style>
  .trap-accept {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    margin-top: 14px;
    cursor: pointer;
  }

  input {
    margin: 3px 0 0;
    accent-color: var(--accent-color);
    cursor: pointer;
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .title {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.9em;
    font-weight: 500;
  }

  .hint {
    font-size: 0.78em;
    color: var(--text-muted);
    line-height: 1.4;
  }
</style>
