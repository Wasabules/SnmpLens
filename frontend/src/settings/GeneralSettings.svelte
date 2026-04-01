<script>
  import { _, locale } from 'svelte-i18n';
  import { SUPPORTED_LOCALES } from '../i18n/index.js';
  import { MonitorCleanup } from '../../wailsjs/go/main/App';
  export let settings;

  let cleanupResult = null;
  let cleaningUp = false;

  function handleLocaleChange(event) {
    const newLocale = event.target.value;
    settings.locale = newLocale;
    locale.set(newLocale);
  }

  async function handleCleanup() {
    cleaningUp = true;
    try {
      const deleted = await MonitorCleanup(settings.polling?.retentionDays || 30);
      cleanupResult = deleted;
    } catch (e) {
      console.warn('Cleanup failed:', e);
      cleanupResult = -1;
    } finally {
      cleaningUp = false;
    }
  }
</script>

<fieldset>
  <legend>{$_('settings.general.networkTitle')}</legend>
  <div class="settings-grid">
    <div class="form-group">
      <label for="port">{$_('settings.general.pollingPort')}</label>
      <input id="port" type="number" bind:value={settings.port} />
    </div>
    <div class="form-group">
      <label for="trapPort">{$_('settings.general.trapPort')}</label>
      <input id="trapPort" type="number" bind:value={settings.trapPort} />
    </div>
    <div class="form-group">
      <label for="timeout">{$_('settings.general.timeout')}</label>
      <input id="timeout" type="number" bind:value={settings.timeout} min="1" />
    </div>
    <div class="form-group">
      <label for="retries">{$_('settings.general.retries')}</label>
      <input id="retries" type="number" bind:value={settings.retries} min="0" />
    </div>
  </div>
</fieldset>

<fieldset>
  <legend>📊 {$_('settings.general.persistenceTitle')}</legend>
  <div class="persist-section">
    <div class="form-group compact">
      <label for="retention-days">{$_('settings.general.retentionDays')}</label>
      <div class="retention-row">
        <input id="retention-days" type="number" bind:value={settings.polling.retentionDays} min="0" max="365" style="width: 80px;" />
        <span class="retention-hint">{$_('settings.general.retentionHint')}</span>
      </div>
    </div>
    <label class="toggle-row">
      <input type="checkbox" bind:checked={settings.polling.autoResume} />
      {$_('settings.general.pollingAutoResume')}
    </label>
    <label class="toggle-row">
      <input type="checkbox" bind:checked={settings.traps.persist} />
      {$_('settings.general.trapPersist')}
    </label>
    <div class="cleanup-row">
      <button class="btn btn-small" on:click={handleCleanup} disabled={cleaningUp}>
        {cleaningUp ? '...' : $_('settings.general.cleanupNow')}
      </button>
      {#if cleanupResult !== null && cleanupResult >= 0}
        <span class="cleanup-result">{$_('settings.general.cleanupResult', { values: { count: cleanupResult } })}</span>
      {/if}
    </div>
  </div>
</fieldset>

<fieldset>
  <legend>🎨 {$_('settings.general.theme')}</legend>
  <div class="theme-section">
    <div class="form-group">
      <label for="theme">{$_('settings.general.themeHint')}</label>
      <div class="theme-switcher">
        {#each ['system', 'dark', 'light'] as themeOption}
          <button
            class="theme-btn"
            class:active={settings.theme === themeOption || (!settings.theme && themeOption === 'system')}
            on:click={() => settings.theme = themeOption}
          >
            <span class="theme-icon">
              {themeOption === 'system' ? '💻' : themeOption === 'dark' ? '🌙' : '☀️'}
            </span>
            {$_(`settings.general.theme${themeOption.charAt(0).toUpperCase() + themeOption.slice(1)}`)}
          </button>
        {/each}
      </div>
    </div>
  </div>
</fieldset>

<fieldset>
  <legend>🌐 {$_('settings.general.language')}</legend>
  <div class="language-section">
    <div class="form-group">
      <label for="locale">{$_('settings.general.languageHint')}</label>
      <select id="locale" value={settings.locale || $locale} on:change={handleLocaleChange}>
        {#each SUPPORTED_LOCALES as loc}
          <option value={loc.code}>{loc.label}</option>
        {/each}
      </select>
    </div>
  </div>
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

  .persist-section {
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-top: 10px;
  }

  .toggle-row {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    user-select: none;
    font-size: 0.92em;
  }

  .toggle-row.disabled {
    opacity: 0.5;
  }

  .toggle-row input[type="checkbox"] {
    width: 16px;
    height: 16px;
    accent-color: var(--accent-color);
    cursor: pointer;
  }

  .theme-section {
    margin-top: 10px;
    max-width: 400px;
  }

  .theme-switcher {
    display: flex;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
  }

  .theme-btn {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 10px 16px;
    background: transparent;
    border: none;
    border-right: 1px solid var(--border-color);
    color: var(--text-muted);
    font-size: 0.9em;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.15s;
  }

  .theme-btn:last-child {
    border-right: none;
  }

  .theme-btn:hover {
    background-color: var(--hover-overlay);
    color: var(--text-color);
  }

  .theme-btn.active {
    background-color: var(--accent-color);
    color: white;
  }

  .theme-icon {
    font-size: 1.1em;
  }

  .retention-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .retention-hint {
    font-size: 0.8em;
    color: var(--text-muted);
    font-style: italic;
  }

  .cleanup-row {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 8px;
  }

  .cleanup-result {
    font-size: 0.85em;
    color: var(--success-color);
  }

  .form-group.compact {
    margin-bottom: 8px;
  }

  .language-section {
    margin-top: 10px;
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
</style>
