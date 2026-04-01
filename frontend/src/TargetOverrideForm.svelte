<script>
  import { createEventDispatcher } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { anonMode, maskString } from './utils/anonymize';

  const dispatch = createEventDispatcher();

  /** @type {object} Per-target overrides (sparse) */
  export let overrides = {};

  /** @type {object} Global settings for showing defaults */
  export let globalSettings = {};

  // Local editable copy
  let local = {
    community: overrides.community ?? '',
    snmpVersion: overrides.snmpVersion ?? '',
    port: overrides.port ?? '',
    timeout: overrides.timeout ?? '',
    retries: overrides.retries ?? '',
    v3User: overrides.v3?.user ?? '',
    v3AuthProto: overrides.v3?.authProto ?? '',
    v3AuthPass: overrides.v3?.authPass ?? '',
    v3PrivProto: overrides.v3?.privProto ?? '',
    v3PrivPass: overrides.v3?.privPass ?? '',
    v3SecLevel: overrides.v3?.secLevel ?? '',
    v3ContextName: overrides.v3?.contextName ?? '',
  };

  // Track which fields are overridden
  let enabled = {
    community: overrides.community !== undefined,
    snmpVersion: overrides.snmpVersion !== undefined,
    port: overrides.port !== undefined,
    timeout: overrides.timeout !== undefined,
    retries: overrides.retries !== undefined,
    v3: overrides.v3 !== undefined,
  };

  $: showV3 = enabled.snmpVersion ? local.snmpVersion === 'v3' : globalSettings.snmpVersion === 'v3';

  function handleSave() {
    const result = {};
    if (enabled.community && local.community) result.community = local.community;
    if (enabled.snmpVersion && local.snmpVersion) result.snmpVersion = local.snmpVersion;
    if (enabled.port && local.port !== '') result.port = Number(local.port);
    if (enabled.timeout && local.timeout !== '') result.timeout = Number(local.timeout);
    if (enabled.retries && local.retries !== '') result.retries = Number(local.retries);
    if (enabled.v3) {
      const v3 = {};
      if (local.v3User) v3.user = local.v3User;
      if (local.v3AuthProto) v3.authProto = local.v3AuthProto;
      if (local.v3AuthPass) v3.authPass = local.v3AuthPass;
      if (local.v3PrivProto) v3.privProto = local.v3PrivProto;
      if (local.v3PrivPass) v3.privPass = local.v3PrivPass;
      if (local.v3SecLevel) v3.secLevel = local.v3SecLevel;
      if (local.v3ContextName) v3.contextName = local.v3ContextName;
      if (Object.keys(v3).length > 0) result.v3 = v3;
    }
    dispatch('save', result);
  }

  function handleClear() {
    dispatch('clear');
  }
</script>

<div class="override-form">
  <div class="override-grid">
    <div class="override-field">
      <label class="override-toggle">
        <input type="checkbox" bind:checked={enabled.community} />
        <span>Community</span>
      </label>
      {#if enabled.community}
        <input type={$anonMode ? 'password' : 'text'} bind:value={local.community} placeholder={$anonMode ? maskString(globalSettings.community) : globalSettings.community} />
      {:else}
        <span class="default-value">{$anonMode ? maskString(globalSettings.community) : globalSettings.community}</span>
      {/if}
    </div>

    <div class="override-field">
      <label class="override-toggle">
        <input type="checkbox" bind:checked={enabled.snmpVersion} />
        <span>{$_('common.version')}</span>
      </label>
      {#if enabled.snmpVersion}
        <select bind:value={local.snmpVersion}>
          <option value="v1">v1</option>
          <option value="v2c">v2c</option>
          <option value="v3">v3</option>
        </select>
      {:else}
        <span class="default-value">{globalSettings.snmpVersion}</span>
      {/if}
    </div>

    <div class="override-field">
      <label class="override-toggle">
        <input type="checkbox" bind:checked={enabled.port} />
        <span>Port</span>
      </label>
      {#if enabled.port}
        <input type="number" bind:value={local.port} placeholder={String(globalSettings.port)} />
      {:else}
        <span class="default-value">{globalSettings.port}</span>
      {/if}
    </div>

    <div class="override-field">
      <label class="override-toggle">
        <input type="checkbox" bind:checked={enabled.timeout} />
        <span>Timeout</span>
      </label>
      {#if enabled.timeout}
        <input type="number" bind:value={local.timeout} placeholder={String(globalSettings.timeout)} min="1" />
      {:else}
        <span class="default-value">{globalSettings.timeout}s</span>
      {/if}
    </div>

    <div class="override-field">
      <label class="override-toggle">
        <input type="checkbox" bind:checked={enabled.retries} />
        <span>{$_('settings.general.retries')}</span>
      </label>
      {#if enabled.retries}
        <input type="number" bind:value={local.retries} placeholder={String(globalSettings.retries)} min="0" />
      {:else}
        <span class="default-value">{globalSettings.retries}</span>
      {/if}
    </div>

    <div class="override-field">
      <label class="override-toggle">
        <input type="checkbox" bind:checked={enabled.v3} />
        <span>SNMPv3</span>
      </label>
      {#if !enabled.v3}
        <span class="default-value">{$_('targets.overrides.useDefault')}</span>
      {/if}
    </div>
  </div>

  {#if enabled.v3 || showV3}
    {#if enabled.v3}
      <div class="v3-overrides">
        <div class="v3-grid">
          <div class="v3-field">
            <label for="ov-v3user">{$_('settings.snmp.username')}</label>
            <input id="ov-v3user" type="text" bind:value={local.v3User} placeholder={globalSettings.v3?.user || ''} />
          </div>
          <div class="v3-field">
            <label for="ov-v3sec">{$_('settings.snmp.securityLevel')}</label>
            <select id="ov-v3sec" bind:value={local.v3SecLevel}>
              <option value="">({$_('targets.overrides.useDefault')})</option>
              <option value="NoAuthNoPriv">NoAuthNoPriv</option>
              <option value="AuthNoPriv">AuthNoPriv</option>
              <option value="AuthPriv">AuthPriv</option>
            </select>
          </div>
          <div class="v3-field">
            <label for="ov-v3auth">{$_('settings.snmp.authProtocol')}</label>
            <select id="ov-v3auth" bind:value={local.v3AuthProto}>
              <option value="">({$_('targets.overrides.useDefault')})</option>
              <option value="MD5">MD5</option>
              <option value="SHA">SHA</option>
              <option value="SHA256">SHA-256</option>
              <option value="SHA512">SHA-512</option>
            </select>
          </div>
          <div class="v3-field">
            <label for="ov-v3authpass">{$_('settings.snmp.authPassword')}</label>
            <input id="ov-v3authpass" type="password" bind:value={local.v3AuthPass} placeholder="••••" />
          </div>
          <div class="v3-field">
            <label for="ov-v3priv">{$_('settings.snmp.privProtocol')}</label>
            <select id="ov-v3priv" bind:value={local.v3PrivProto}>
              <option value="">({$_('targets.overrides.useDefault')})</option>
              <option value="DES">DES</option>
              <option value="AES">AES-128</option>
              <option value="AES256C">AES-256</option>
            </select>
          </div>
          <div class="v3-field">
            <label for="ov-v3privpass">{$_('settings.snmp.privPassword')}</label>
            <input id="ov-v3privpass" type="password" bind:value={local.v3PrivPass} placeholder="••••" />
          </div>
          <div class="v3-field full">
            <label for="ov-v3ctx">{$_('settings.snmp.contextName')}</label>
            <input id="ov-v3ctx" type="text" bind:value={local.v3ContextName} placeholder={globalSettings.v3?.contextName || ''} />
          </div>
        </div>
      </div>
    {/if}
  {/if}

  <div class="override-actions">
    <button class="btn-sm" on:click={handleClear}>{$_('targets.overrides.clear')}</button>
    <button class="btn-sm primary" on:click={handleSave}>{$_('common.save')}</button>
  </div>
</div>

<style>
  .override-form {
    padding: 12px;
    background-color: var(--accent-subtle);
    border: 1px solid var(--accent-subtle-strong);
    border-radius: 6px;
    margin-top: 8px;
  }

  .override-grid {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 10px;
  }

  .override-field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .override-toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 0.82em;
    font-weight: 600;
    color: var(--text-dimmed);
    cursor: pointer;
    user-select: none;
  }

  .override-toggle input[type="checkbox"] {
    width: 14px;
    height: 14px;
    accent-color: var(--accent-color);
    cursor: pointer;
    margin: 0;
  }

  .override-field input[type="text"],
  .override-field input[type="number"],
  .override-field select {
    padding: 5px 8px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    font-size: 0.85em;
    width: 100%;
    box-sizing: border-box;
  }

  .default-value {
    font-size: 0.82em;
    color: var(--text-dimmed);
    font-style: italic;
    padding: 5px 0;
  }

  .v3-overrides {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border-color);
  }

  .v3-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }

  .v3-field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .v3-field.full {
    grid-column: 1 / -1;
  }

  .v3-field label {
    font-size: 0.8em;
    color: var(--text-dimmed);
    font-weight: 500;
  }

  .v3-field input,
  .v3-field select {
    padding: 5px 8px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    font-size: 0.85em;
    width: 100%;
    box-sizing: border-box;
  }

  .override-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 12px;
  }

  .btn-sm {
    padding: 4px 12px;
    font-size: 0.85em;
    border: 1px solid var(--border-color);
    background: transparent;
    color: var(--text-color);
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .btn-sm:hover {
    background-color: var(--bg-color);
  }

  .btn-sm.primary {
    background-color: var(--accent-color);
    border-color: var(--accent-color);
    color: white;
  }

  .btn-sm.primary:hover {
    background-color: var(--accent-hover-color);
  }
</style>
