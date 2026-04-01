<script>
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';
  import { notificationStore } from '../stores/notifications';
  import { TestConnection } from '../../wailsjs/go/main/App';
  import { buildTestRequest } from '../utils/snmpParams';
  import { anonMode, maskString, maskSysDescr } from '../utils/anonymize';

  export let settings;

  let testTarget = '';
  let testVersion = 'v2c';
  let isTesting = false;
  let testResult = null;

  async function handleTestConnection() {
    const t = get(_);
    if (!testTarget.trim()) {
      notificationStore.add(t('settings.snmp.enterTarget'), 'error');
      return;
    }

    isTesting = true;
    testResult = null;

    try {
      const result = await TestConnection(buildTestRequest({...settings, snmpVersion: testVersion}, testTarget.trim()));

      testResult = result;
      if (result.error) {
        notificationStore.add(t('settings.snmp.testFailed', { values: { error: result.error } }), 'error');
      } else {
        notificationStore.add(t('settings.snmp.testSuccess', { values: { target: testTarget } }), 'success');
      }
    } catch (err) {
      testResult = { error: String(err) };
      notificationStore.add(t('settings.snmp.testError', { values: { error: String(err) } }), 'error');
    } finally {
      isTesting = false;
    }
  }
</script>

<fieldset>
  <legend>{$_('settings.snmp.v1v2cTitle')}</legend>
  <div class="settings-grid single-column">
    <div class="form-group">
      <label for="community">{$_('settings.snmp.community')}</label>
      {#if $anonMode}
        <input id="community" type="password" bind:value={settings.community} />
      {:else}
        <input id="community" type="text" bind:value={settings.community} />
      {/if}
    </div>
  </div>
</fieldset>

<fieldset>
  <legend>{$_('settings.snmp.v3Title')}</legend>
  <div class="settings-grid">
    <div class="form-group">
      <label for="v3-user">{$_('settings.snmp.username')}</label>
      {#if $anonMode}
        <input id="v3-user" type="password" bind:value={settings.v3.user} />
      {:else}
        <input id="v3-user" type="text" bind:value={settings.v3.user} />
      {/if}
    </div>
    <div class="form-group">
      <label for="v3-secLevel">{$_('settings.snmp.securityLevel')}</label>
      <select id="v3-secLevel" bind:value={settings.v3.secLevel}>
        <option value="NoAuthNoPriv">NoAuthNoPriv</option>
        <option value="AuthNoPriv">AuthNoPriv</option>
        <option value="AuthPriv">AuthPriv</option>
      </select>
    </div>
    <div class="form-group">
      <label for="v3-authProto">{$_('settings.snmp.authProtocol')}</label>
      <select id="v3-authProto" bind:value={settings.v3.authProto} disabled={settings.v3.secLevel === 'NoAuthNoPriv'}>
        <option value="MD5">MD5</option>
        <option value="SHA">SHA</option>
        <option value="SHA224">SHA-224</option>
        <option value="SHA256">SHA-256</option>
        <option value="SHA384">SHA-384</option>
        <option value="SHA512">SHA-512</option>
      </select>
    </div>
    <div class="form-group">
      <label for="v3-authPass">{$_('settings.snmp.authPassword')}</label>
      <input id="v3-authPass" type="password" bind:value={settings.v3.authPass} disabled={settings.v3.secLevel === 'NoAuthNoPriv'} />
    </div>
    <div class="form-group">
      <label for="v3-privProto">{$_('settings.snmp.privProtocol')}</label>
      <select id="v3-privProto" bind:value={settings.v3.privProto} disabled={settings.v3.secLevel !== 'AuthPriv'}>
        <option value="DES">DES</option>
        <option value="AES">AES-128</option>
        <option value="AES192C">AES-192</option>
        <option value="AES256C">AES-256</option>
        <option value="AES192">AES-192 (Blumenthal)</option>
        <option value="AES256">AES-256 (Blumenthal)</option>
      </select>
    </div>
    <div class="form-group">
      <label for="v3-privPass">{$_('settings.snmp.privPassword')}</label>
      <input id="v3-privPass" type="password" bind:value={settings.v3.privPass} disabled={settings.v3.secLevel !== 'AuthPriv'} />
    </div>
    <div class="form-group full-width">
      <label for="v3-contextName">{$_('settings.snmp.contextName')}</label>
      <input id="v3-contextName" type="text" bind:value={settings.v3.contextName} placeholder={$_('settings.snmp.contextPlaceholder')} />
    </div>
  </div>
</fieldset>

<fieldset class="test-connection">
  <legend>🔌 {$_('settings.snmp.testTitle')}</legend>
  <div class="test-form">
    <div class="form-group">
      <label for="test-target">{$_('settings.snmp.testTarget')}</label>
      <input
        id="test-target"
        type="text"
        bind:value={testTarget}
        placeholder={$_('settings.snmp.testTargetPlaceholder')}
      />
    </div>
    <div class="form-group">
      <label for="test-version">{$_('settings.snmp.testVersion')}</label>
      <select id="test-version" bind:value={testVersion}>
        <option value="v1">v1</option>
        <option value="v2c">v2c</option>
        <option value="v3">v3</option>
      </select>
    </div>
    <button
      class="btn test-btn"
      on:click={handleTestConnection}
      disabled={isTesting || !testTarget.trim()}
    >
      {isTesting ? '⏳ ' + $_('settings.snmp.testing') : '🔍 ' + $_('settings.snmp.testButton')}
    </button>
  </div>
  {#if testResult}
    <div class="test-result" class:success={!testResult.error} class:error={testResult.error}>
      {#if testResult.error}
        <span class="result-icon">❌</span>
        <span class="result-text">{testResult.error}</span>
      {:else}
        <span class="result-icon">✅</span>
        <span class="result-text">sysDescr: {$anonMode ? maskSysDescr(testResult.result?.value || 'OK') : (testResult.result?.value || 'OK')}</span>
      {/if}
    </div>
  {/if}
</fieldset>

<style>
  fieldset {
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 20px;
    margin-bottom: 20px;
  }

  legend {
    padding: 0 10px;
    color: var(--text-color);
    font-weight: 500;
    font-size: 1.1em;
  }

  .settings-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 15px 20px;
    margin-top: 10px;
  }

  .settings-grid.single-column {
    grid-template-columns: 1fr;
    max-width: 300px;
  }

  .form-group {
    flex-direction: column;
    align-items: stretch;
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

  .full-width {
    grid-column: 1 / -1;
  }

  .test-connection {
    margin-top: 15px;
    background-color: var(--accent-subtle);
    border-color: var(--accent-border);
  }

  .test-form {
    display: flex;
    gap: 15px;
    align-items: flex-end;
    flex-wrap: wrap;
  }

  .test-form .form-group {
    flex: 1;
    min-width: 150px;
    margin-bottom: 0;
  }

  .test-btn {
    flex-shrink: 0;
    height: 38px;
  }

  .test-result {
    margin-top: 12px;
    padding: 10px 12px;
    border-radius: 4px;
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.9em;
  }

  .test-result.success {
    background-color: var(--success-subtle-medium);
    border: 1px solid var(--success-border-strong);
    color: var(--success-color);
  }

  .test-result.error {
    background-color: var(--error-subtle-medium);
    border: 1px solid var(--error-border-strong);
    color: var(--error-color);
  }

  .result-text {
    word-break: break-word;
    flex: 1;
  }
</style>
