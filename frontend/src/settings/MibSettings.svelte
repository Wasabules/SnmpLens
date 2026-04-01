<script>
  import { onMount } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';
  import { mibStore, mibDiagnostics } from '../stores/mibStore';
  import { mibPathsStore } from '../stores/mibPathsStore';
  import { notificationStore } from '../stores/notifications';

  export let defaultMibPath;

  let newMibPath = '';

  onMount(() => {
    mibPathsStore.load();
    if (defaultMibPath) {
      scanPathForMibs(defaultMibPath);
    }
  });

  async function handleBrowseFolder() {
    try {
      const { BrowseDialog } = await import('../../wailsjs/go/main/App');
      const selectedPath = await BrowseDialog();
      if (selectedPath && selectedPath.trim()) {
        newMibPath = selectedPath.trim();
      }
    } catch (e) {
      console.error('Failed to open directory picker:', e);
      const path = prompt('Enter the full path to your MIB directory:');
      if (path && path.trim()) {
        newMibPath = path.trim();
      }
    }
  }

  async function handleAddPath() {
    if (newMibPath && newMibPath.trim()) {
      mibPathsStore.addPath(newMibPath.trim());
      await scanPathForMibs(newMibPath.trim());
      newMibPath = '';
    }
  }

  async function scanPathForMibs(path) {
    try {
      const { ListMibFiles } = await import('../../wailsjs/go/main/App');
      const mibs = await ListMibFiles(path);
      mibPathsStore.setDetectedMibs(path, mibs);
    } catch (e) {
      console.error('Failed to scan path for MIBs:', e);
      mibPathsStore.setDetectedMibs(path, []);
    }
  }

  function handleRemovePath(path) {
    const t = get(_);
    if (confirm(t('settings.mibs.removeConfirm', { values: { path } }))) {
      mibPathsStore.removePath(path);
    }
  }
</script>

<fieldset>
  <legend>📂 {$_('settings.mibs.directoriesTitle')}</legend>

  <!-- Default MIB Path -->
  <div class="mib-path-item default">
    <div class="path-header">
      <span class="path-badge">{$_('settings.mibs.default')}</span>
      <span class="path-text">{defaultMibPath || $_('common.loading')}</span>
    </div>
    {#if $mibPathsStore.detectedMibs[defaultMibPath]}
      <div class="mib-list">
        <div class="mib-list-header">
          <span>{$_('settings.mibs.mibsDetected', { values: { count: $mibPathsStore.detectedMibs[defaultMibPath].length } })}</span>
          <div class="mib-actions">
            <button class="btn-small" on:click={() => mibPathsStore.enableAllInPath(defaultMibPath)}>{$_('common.enableAll')}</button>
            <button class="btn-small" on:click={() => mibPathsStore.disableAllInPath(defaultMibPath)}>{$_('common.disableAll')}</button>
          </div>
        </div>
        <div class="mib-items">
          {#each $mibPathsStore.detectedMibs[defaultMibPath] as mib}
            <label class="mib-item">
              <input
                type="checkbox"
                checked={$mibPathsStore.enabledMibs[defaultMibPath]?.[mib] !== false}
                on:change={() => mibPathsStore.toggleMib(defaultMibPath, mib)}
              />
              <span>{mib}</span>
            </label>
          {/each}
        </div>
      </div>
    {/if}
  </div>

  <!-- Custom MIB Paths -->
  {#each $mibPathsStore.customPaths as customPath}
    <div class="mib-path-item">
      <div class="path-header">
        <span class="path-badge custom">{$_('settings.mibs.custom')}</span>
        <span class="path-text">{customPath}</span>
        <button class="btn-remove" on:click={() => handleRemovePath(customPath)} title={$_('common.remove')}>✕</button>
      </div>
      {#if $mibPathsStore.detectedMibs[customPath]}
        <div class="mib-list">
          <div class="mib-list-header">
            <span>{$_('settings.mibs.mibsDetected', { values: { count: $mibPathsStore.detectedMibs[customPath].length } })}</span>
            <div class="mib-actions">
              <button class="btn-small" on:click={() => mibPathsStore.enableAllInPath(customPath)}>{$_('common.enableAll')}</button>
              <button class="btn-small" on:click={() => mibPathsStore.disableAllInPath(customPath)}>{$_('common.disableAll')}</button>
            </div>
          </div>
          <div class="mib-items">
            {#each $mibPathsStore.detectedMibs[customPath] as mib}
              <label class="mib-item">
                <input
                  type="checkbox"
                  checked={$mibPathsStore.enabledMibs[customPath]?.[mib] !== false}
                  on:change={() => mibPathsStore.toggleMib(customPath, mib)}
                />
                <span>{mib}</span>
              </label>
            {/each}
          </div>
        </div>
      {/if}
    </div>
  {/each}

  <!-- Add New Path -->
  <div class="add-path-section">
    <h4>{$_('settings.mibs.addCustomTitle')}</h4>
    <div class="add-path-form">
      <input
        type="text"
        placeholder={$_('settings.mibs.addPlaceholder')}
        bind:value={newMibPath}
        on:keydown={(e) => e.key === 'Enter' && handleAddPath()}
      />
      <button class="btn secondary" on:click={handleBrowseFolder}>📁 {$_('common.browse')}</button>
      <button class="btn" on:click={handleAddPath} disabled={!newMibPath.trim()}>➕ {$_('common.add')}</button>
    </div>
  </div>

  <!-- Reload All MIBs -->
  <div class="reload-section">
    <button
      class="btn"
      on:click={() => mibStore.load()}
      disabled={$mibStore.isLoading}
      style="width: 100%;"
    >
      {$mibStore.isLoading ? '⏳ ' + $_('settings.mibs.reloadingAll') : '🔄 ' + $_('settings.mibs.reloadAll')}
    </button>
    <small class="mib-empty-text">
      {$_('settings.mibs.reloadHint')}
    </small>
  </div>

  <!-- MIB Diagnostics -->
  {#if $mibDiagnostics.length > 0}
    <div class="diagnostics-section">
      <h4>{$_('settings.mibs.diagnosticsTitle')}</h4>
      <div class="diagnostics-summary">
        <span class="diag-success">{$_('settings.mibs.loaded', { values: { count: $mibDiagnostics.filter(d => d.success).length } })}</span>
        <span class="diag-fail">{$_('settings.mibs.failed', { values: { count: $mibDiagnostics.filter(d => !d.success).length } })}</span>
      </div>
      <div class="diagnostics-list">
        {#each $mibDiagnostics as diag}
          <div class="diag-item" class:diag-error={!diag.success}>
            <span class="diag-icon">{diag.success ? '✅' : '❌'}</span>
            <span class="diag-filename">{diag.fileName}</span>
            {#if diag.error}
              <span class="diag-error-msg" title={diag.error}>{diag.error}</span>
            {/if}
          </div>
        {/each}
      </div>
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

  .mib-path-item {
    margin-bottom: 20px;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
  }

  .mib-path-item.default {
    border-color: var(--favorites-border);
  }

  .path-header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px;
    background-color: var(--hover-overlay);
    border-bottom: 1px solid var(--border-color);
  }

  .path-badge {
    padding: 4px 10px;
    border-radius: 4px;
    font-size: 0.8em;
    font-weight: 600;
    text-transform: uppercase;
  }

  .path-badge {
    background-color: var(--favorites-subtle-strong);
    color: var(--favorites-color);
  }

  .path-badge.custom {
    background-color: var(--accent-subtle-strong);
    color: var(--oid-color);
  }

  .path-text {
    flex-grow: 1;
    font-family: 'Courier New', monospace;
    font-size: 0.9em;
    color: var(--text-color);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .btn-remove {
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: 1.2em;
    cursor: pointer;
    padding: 4px 8px;
    border-radius: 4px;
    transition: all 0.2s;
  }

  .btn-remove:hover {
    background-color: var(--error-subtle-strong);
    color: var(--error-color);
  }

  .mib-list {
    padding: 12px;
  }

  .mib-list-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 10px;
    font-size: 0.9em;
    color: var(--text-dimmed);
  }

  .mib-empty-text {
    color: var(--text-dimmed);
    margin-top: 5px;
    display: block;
    text-align: center;
  }

  .mib-actions {
    display: flex;
    gap: 8px;
  }

  .btn-small {
    padding: 4px 8px;
    font-size: 0.8em;
    background-color: transparent;
    border: 1px solid var(--border-color);
    color: var(--text-color);
    border-radius: 3px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .btn-small:hover {
    background-color: var(--hover-overlay-medium);
    border-color: var(--accent-color);
  }

  .mib-items {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 8px;
    max-height: 150px;
    overflow-y: auto;
    padding: 8px;
    background-color: var(--bg-color);
    border-radius: 4px;
  }

  .mib-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border-radius: 4px;
    cursor: pointer;
    transition: background-color 0.2s;
    font-size: 0.9em;
  }

  .mib-item:hover {
    background-color: var(--hover-overlay);
  }

  .mib-item input[type="checkbox"] {
    width: auto;
    cursor: pointer;
  }

  .add-path-section {
    margin-top: 20px;
    padding: 15px;
    background-color: var(--hover-overlay);
    border-radius: 6px;
  }

  .add-path-section h4 {
    margin: 0 0 12px 0;
    font-size: 1em;
    color: var(--text-color);
  }

  .add-path-form {
    display: flex;
    gap: 10px;
  }

  .add-path-form input {
    flex-grow: 1;
  }

  .reload-section {
    margin-top: 20px;
    padding: 15px;
    background-color: var(--favorites-subtle);
    border: 1px solid var(--favorites-subtle-strong);
    border-radius: 6px;
  }

  .diagnostics-section {
    margin-top: 20px;
    padding: 15px;
    background-color: var(--hover-overlay);
    border: 1px solid var(--border-color);
    border-radius: 6px;
  }

  .diagnostics-section h4 {
    margin: 0 0 10px 0;
    font-size: 1em;
  }

  .diagnostics-summary {
    display: flex;
    gap: 15px;
    margin-bottom: 10px;
    font-size: 0.9em;
  }

  .diag-success {
    color: var(--success-color);
    font-weight: 600;
  }

  .diag-fail {
    color: var(--error-color);
    font-weight: 600;
  }

  .diagnostics-list {
    max-height: 200px;
    overflow-y: auto;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    background-color: var(--bg-color);
  }

  .diag-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-color);
    font-size: 0.85em;
  }

  .diag-item:last-child {
    border-bottom: none;
  }

  .diag-item.diag-error {
    background-color: var(--error-subtle);
  }

  .diag-icon {
    flex-shrink: 0;
  }

  .diag-filename {
    font-family: 'Courier New', monospace;
    font-weight: 500;
    min-width: 150px;
  }

  .diag-error-msg {
    color: var(--error-color);
    font-size: 0.85em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
    cursor: help;
  }
</style>
