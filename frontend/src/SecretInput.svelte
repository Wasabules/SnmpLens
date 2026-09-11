<script>
  import { _ } from 'svelte-i18n';
  import Icon from './Icon.svelte';
  import { anonMode } from './utils/anonymize';

  /**
   * A field for a secret — a community, a passphrase — whose value can be shown.
   * Masking is not protection: the value is in the page either way, so seeing
   * what was typed is offered. Not in Anonymous Mode, which is for a screen
   * somebody else is watching: there the eye is gone and the value stays masked.
   */
  export let value = '';
  export let id;
  export let placeholder = '';
  export let autocomplete = 'off';
  export let disabled = false;

  let shown = false;
</script>

<span class="secret">
  <!-- Two inputs rather than a type that changes, as UsmFields does for its
       user: an input's type stays fixed where a value is bound to it. -->
  {#if shown && !$anonMode}
    <input {id} type="text" spellcheck="false" {autocomplete} {placeholder} {disabled} bind:value />
  {:else}
    <input {id} type="password" {autocomplete} {placeholder} {disabled} bind:value />
  {/if}
  {#if !$anonMode}
    <button type="button" class="reveal" {disabled} aria-controls={id} aria-pressed={shown}
      title={shown ? $_('common.hideSecret') : $_('common.showSecret')}
      aria-label={shown ? $_('common.hideSecret') : $_('common.showSecret')}
      on:click={() => (shown = !shown)}>
      <Icon name={shown ? 'eye-off' : 'eye'} size={15} />
    </button>
  {/if}
</span>

<style>
  .secret {
    position: relative;
    display: flex;
    width: 100%;
  }

  input {
    width: 100%;
    padding: 8px 34px 8px 10px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
  }

  input:disabled {
    opacity: 0.55;
  }

  .reveal {
    position: absolute;
    top: 50%;
    right: 6px;
    transform: translateY(-50%);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 3px;
    background: none;
    border: none;
    border-radius: 3px;
    color: var(--text-muted);
    cursor: pointer;
  }

  .reveal:hover:not(:disabled) {
    color: var(--text-color);
  }

  .reveal:focus-visible {
    outline: 2px solid var(--accent-color);
  }

  .reveal:disabled {
    cursor: default;
    opacity: 0.4;
  }
</style>
