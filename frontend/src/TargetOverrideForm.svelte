<script>
  import { createEventDispatcher } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { anonMode, maskString } from './utils/anonymize';
  import { AUTH_PROTOCOLS, PRIV_PROTOCOLS } from './utils/snmpSecurity.js';
  import {
    blankProfile,
    findProfile,
    hasCustomCredentials,
    normaliseProfile,
    uniqueName,
    validateProfile,
  } from './utils/credentialProfiles.js';

  const dispatch = createEventDispatcher();

  /** @type {object} Per-target overrides (sparse) */
  export let overrides = {};

  /** @type {object} Global settings for showing defaults */
  export let globalSettings = {};

  /** The target this form edits, to name a profile made from it. */
  export let address = '';
  export let label = '';

  // Where the target's identity comes from: 'default', a credential profile's
  // id, or 'custom' for the per-field overrides. ONE choice, because the three
  // are exclusive: a profile replaces the target's own community, version and
  // v3 block rather than being layered under them, and two answers to "what
  // does this target authenticate with" is one too many.
  let identity = findProfile(globalSettings, overrides.profile)
    ? overrides.profile
    : (hasCustomCredentials(overrides) ? 'custom' : 'default');

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

  $: profiles = globalSettings.credentialProfiles || [];
  $: chosen = findProfile(globalSettings, identity);

  function describe(profile, anon) {
    if (profile.version !== 'v3') return profile.version;
    const user = profile.v3?.user || '';
    return `v3 · ${anon ? maskString(user) : user} · ${profile.v3?.secLevel || ''}`;
  }

  function transport() {
    const result = {};
    if (enabled.port && local.port !== '') result.port = Number(local.port);
    if (enabled.timeout && local.timeout !== '') result.timeout = Number(local.timeout);
    if (enabled.retries && local.retries !== '') result.retries = Number(local.retries);
    return result;
  }

  function ownIdentity() {
    const result = {};
    if (enabled.community && local.community) result.community = local.community;
    if (enabled.snmpVersion && local.snmpVersion) result.snmpVersion = local.snmpVersion;
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
    return result;
  }

  function handleSave() {
    const result = transport();
    if (identity === 'custom') Object.assign(result, ownIdentity());
    else if (identity !== 'default') result.profile = identity;
    dispatch('save', result);
  }

  function handleClear() {
    dispatch('clear');
  }

  // "Save as a profile": what this target authenticates with now — its own
  // fields over the defaults — becomes a named profile, and the target is given
  // it. The one-click way from per-target overrides to profiles.
  let profileName = '';
  let profileError = '';

  function saveAsProfile() {
    const own = ownIdentity();
    const draft = blankProfile(own.snmpVersion || globalSettings.snmpVersion || 'v2c', profiles);
    // Never named after the target in Anonymous Mode: a profile name is shown
    // unmasked, and the address or label is exactly what the mode hides.
    const fallback = $anonMode ? '' : $_('profiles.fromTarget', { values: { target: label || address } });
    draft.name = uniqueName(profileName.trim() || fallback, profiles);
    if (draft.version === 'v3') draft.v3 = { ...draft.v3, ...globalSettings.v3, ...(own.v3 || {}) };
    else draft.community = own.community ?? globalSettings.community ?? '';

    const profile = normaliseProfile(draft);
    const [first] = validateProfile(profile, profiles).errors;
    if (first) {
      profileError = $_(first.key, { values: first.values });
      return;
    }
    profileError = '';
    dispatch('saveAsProfile', { profile, overrides: { ...transport(), profile: profile.id } });
  }
</script>

<div class="override-form">
  <div class="identity-row">
    <label for="ov-identity">{$_('profiles.identifiers')}</label>
    <select id="ov-identity" bind:value={identity}>
      <option value="default">{$_('profiles.useDefault')}</option>
      {#each profiles as p (p.id)}
        <option value={p.id}>{p.name} · {p.version}</option>
      {/each}
      <option value="custom">{$_('profiles.custom')}</option>
    </select>
    <span class="identity-note">
      {#if chosen}
        {describe(chosen, $anonMode)}
      {:else if identity === 'default'}
        {globalSettings.snmpVersion}
      {/if}
    </span>
  </div>

  {#if identity === 'custom'}
    <div class="override-grid">
      <div class="override-field">
        <label class="override-toggle">
          <input type="checkbox" bind:checked={enabled.community} />
          <span>Community</span>
        </label>
        {#if enabled.community}
          {#if $anonMode}
            <input type="password" bind:value={local.community} placeholder={maskString(globalSettings.community)} />
          {:else}
            <input type="text" bind:value={local.community} placeholder={globalSettings.community} />
          {/if}
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
          <input type="checkbox" bind:checked={enabled.v3} />
          <span>SNMPv3</span>
        </label>
        {#if !enabled.v3}
          <span class="default-value">{$_('targets.overrides.useDefault')}</span>
        {/if}
      </div>
    </div>

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
              {#each AUTH_PROTOCOLS as p (p.value)}
                <option value={p.value}>{p.label}</option>
              {/each}
            </select>
          </div>
          <div class="v3-field">
            <label for="ov-v3authpass">{$_('settings.snmp.authPassword')}</label>
            <input id="ov-v3authpass" type="password" autocomplete="new-password" bind:value={local.v3AuthPass} placeholder="••••" />
          </div>
          <div class="v3-field">
            <label for="ov-v3priv">{$_('settings.snmp.privProtocol')}</label>
            <select id="ov-v3priv" bind:value={local.v3PrivProto}>
              <option value="">({$_('targets.overrides.useDefault')})</option>
              {#each PRIV_PROTOCOLS as p (p.value)}
                <option value={p.value}>{p.label}</option>
              {/each}
            </select>
          </div>
          <div class="v3-field">
            <label for="ov-v3privpass">{$_('settings.snmp.privPassword')}</label>
            <input id="ov-v3privpass" type="password" autocomplete="new-password" bind:value={local.v3PrivPass} placeholder="••••" />
          </div>
          <div class="v3-field full">
            <label for="ov-v3ctx">{$_('settings.snmp.contextName')}</label>
            <input id="ov-v3ctx" type="text" bind:value={local.v3ContextName} placeholder={globalSettings.v3?.contextName || ''} />
          </div>
        </div>
      </div>
    {/if}

    <div class="as-profile">
      <input type="text" bind:value={profileName} placeholder={$_('profiles.namePlaceholder')} aria-label={$_('profiles.name')} />
      <button class="btn-sm" on:click={saveAsProfile} title={$_('profiles.saveAsProfileHint')}>
        {$_('profiles.saveAsProfile')}
      </button>
      {#if profileError}<span class="as-profile-error">{profileError}</span>{/if}
    </div>
  {/if}

  <div class="override-grid transport">
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
  </div>

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

  .identity-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-bottom: 10px;
  }

  .identity-row label {
    font-size: 0.82em;
    font-weight: 600;
    color: var(--text-dimmed);
  }

  .identity-row select {
    min-width: 220px;
    padding: 5px 8px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    font-size: 0.85em;
  }

  .identity-note {
    font-size: 0.8em;
    color: var(--text-dimmed);
    font-style: italic;
  }

  .override-grid {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 10px;
  }

  .override-grid.transport {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border-color);
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
  .override-field input[type="password"],
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

  .as-profile {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-top: 10px;
  }

  .as-profile input {
    flex: 1;
    min-width: 160px;
    padding: 5px 8px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    font-size: 0.85em;
  }

  .as-profile-error {
    flex-basis: 100%;
    font-size: 0.8em;
    color: var(--error-color);
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
