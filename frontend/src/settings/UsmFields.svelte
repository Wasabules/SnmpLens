<script>
  import { _ } from 'svelte-i18n';
  import { anonMode } from '../utils/anonymize';
  import { SEC_LEVELS, AUTH_PROTOCOLS, PRIV_PROTOCOLS, usesAuth, usesPriv } from '../utils/snmpSecurity.js';

  /**
   * One SNMPv3 USM identity — user, security level, the two protocols with
   * their passphrases, and the context — edited in place.
   *
   * The default identifiers and every v3 credential profile are the same seven
   * fields; written out once per form they had already drifted, one form
   * offering six authentication protocols and the other four.
   */
  export let v3;
  /** Prefix for the element ids, so two of these on one page do not share labels. */
  export let idPrefix = 'v3';
  /** Messages by field — { text, level: 'error' | 'warn' } — shown under it. */
  export let problems = {};
  /** Whether to offer the context: a simulated device has only the default one. */
  export let showContext = true;
</script>

<div class="usm-grid">
  <div class="form-group">
    <label for="{idPrefix}-user">{$_('settings.snmp.username')}</label>
    {#if $anonMode}
      <input id="{idPrefix}-user" type="password" autocomplete="off" bind:value={v3.user} />
    {:else}
      <input id="{idPrefix}-user" type="text" autocomplete="off" spellcheck="false" bind:value={v3.user} />
    {/if}
    {#if problems.user}<span class="problem {problems.user.level}">{problems.user.text}</span>{/if}
  </div>
  <div class="form-group">
    <label for="{idPrefix}-secLevel">{$_('settings.snmp.securityLevel')}</label>
    <select id="{idPrefix}-secLevel" bind:value={v3.secLevel}>
      {#each SEC_LEVELS as level (level)}
        <option value={level}>{level}</option>
      {/each}
    </select>
  </div>
  <div class="form-group">
    <label for="{idPrefix}-authProto">{$_('settings.snmp.authProtocol')}</label>
    <select id="{idPrefix}-authProto" bind:value={v3.authProto} disabled={!usesAuth(v3.secLevel)}>
      {#each AUTH_PROTOCOLS as p (p.value)}
        <option value={p.value}>{p.label}</option>
      {/each}
    </select>
    {#if problems.authProto}<span class="problem {problems.authProto.level}">{problems.authProto.text}</span>{/if}
  </div>
  <div class="form-group">
    <label for="{idPrefix}-authPass">{$_('settings.snmp.authPassword')}</label>
    <input id="{idPrefix}-authPass" type="password" autocomplete="new-password"
      bind:value={v3.authPass} disabled={!usesAuth(v3.secLevel)} />
    {#if problems.authPass}<span class="problem {problems.authPass.level}">{problems.authPass.text}</span>{/if}
  </div>
  <div class="form-group">
    <label for="{idPrefix}-privProto">{$_('settings.snmp.privProtocol')}</label>
    <select id="{idPrefix}-privProto" bind:value={v3.privProto} disabled={!usesPriv(v3.secLevel)}>
      {#each PRIV_PROTOCOLS as p (p.value)}
        <option value={p.value}>{p.label}</option>
      {/each}
    </select>
    {#if problems.privProto}<span class="problem {problems.privProto.level}">{problems.privProto.text}</span>{/if}
  </div>
  <div class="form-group">
    <label for="{idPrefix}-privPass">{$_('settings.snmp.privPassword')}</label>
    <input id="{idPrefix}-privPass" type="password" autocomplete="new-password"
      bind:value={v3.privPass} disabled={!usesPriv(v3.secLevel)} />
    {#if problems.privPass}<span class="problem {problems.privPass.level}">{problems.privPass.text}</span>{/if}
  </div>
  {#if showContext}
    <div class="form-group full-width">
      <label for="{idPrefix}-contextName">{$_('settings.snmp.contextName')}</label>
      <input id="{idPrefix}-contextName" type="text" bind:value={v3.contextName}
        placeholder={$_('settings.snmp.contextPlaceholder')} />
    </div>
  {/if}
</div>

<style>
  .usm-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 15px 20px;
    margin-top: 10px;
    /* A passphrase with a message under it made the row taller, and the protocol
       select beside it grew to match. */
    align-items: start;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    /* Let a long <select> option shrink instead of widening the column. */
    min-width: 0;
  }

  .form-group label {
    margin-bottom: 5px;
    font-size: 0.9em;
    color: var(--text-light);
  }

  input, select {
    width: 100%;
    padding: 8px 10px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
  }

  input:disabled, select:disabled {
    opacity: 0.55;
  }

  .full-width {
    grid-column: 1 / -1;
  }

  .problem {
    margin-top: 4px;
    font-size: 0.78em;
    line-height: 1.35;
  }

  .problem.error {
    color: var(--error-color);
  }

  .problem.warn {
    color: var(--warning-color);
  }

  @media (max-width: 560px) {
    .usm-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
