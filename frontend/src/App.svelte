<script>
  import { onMount, onDestroy } from 'svelte';
  import { get } from 'svelte/store';
  import { _ } from 'svelte-i18n';
  import { notificationStore } from './stores/notifications';
  import MibPanel from './MibPanel.svelte';
  import OperationsPanel from './OperationsPanel.svelte';
  import TrapPanel from './TrapPanel.svelte';
  import HistoryPanel from './HistoryPanel.svelte';
  import MonitorPanel from './MonitorPanel.svelte';
  import DiscoveryPanel from './DiscoveryPanel.svelte';
  import Notifications from './Notifications.svelte';
  import SettingsModal from './SettingsModal.svelte';
  import TargetManager from './TargetManager.svelte';
  import DebugPanel from './DebugPanel.svelte';
  import { trapStore } from './stores/trapStore';
  import { mibPathsStore } from './stores/mibPathsStore';
  import { mibStore, mibDiagnostics } from './stores/mibStore';
  import { settingsStore } from './stores/settingsStore';
  import { pollingStore } from './stores/pollingStore';
  import { GetPersistentMibDirectory, ListMibFiles, ImportMibFiles } from '../wailsjs/go/main/App';
  import { OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime';

  let activeTab = 'operations'; // 'operations', 'traps', or 'history'

  // Compute target count from settings
  $: targetCount = $settingsStore.targets
    ? $settingsStore.targets.split('\n').filter(t => t.trim()).length
    : 0;
  let selectedNode = null;
  let showSettings = false;
  let showTargets = false;
  let showDebug = false;
  let pendingSnmpAction = null;
  
  // Resizable panel
  let mibPanelWidth = 350; // Default width in pixels
  let isResizing = false;
  let startX = 0;
  let startWidth = 0;

  // Load saved width from localStorage
  function loadPanelWidth() {
    try {
      const saved = localStorage.getItem('snmplens_panel_width');
      if (saved) {
        mibPanelWidth = parseInt(saved, 10);
      }
    } catch (e) {
      console.error('Failed to load panel width:', e);
    }
  }

  // Save width to localStorage
  function savePanelWidth() {
    try {
      localStorage.setItem('snmplens_panel_width', mibPanelWidth.toString());
    } catch (e) {
      console.error('Failed to save panel width:', e);
    }
  }

  function startResize(event) {
    isResizing = true;
    startX = event.clientX;
    startWidth = mibPanelWidth;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  }

  function handleMouseMove(event) {
    if (!isResizing) return;
    
    const delta = event.clientX - startX;
    const newWidth = startWidth + delta;
    
    // Constrain width between 250px and 800px
    mibPanelWidth = Math.max(250, Math.min(800, newWidth));
  }

  function stopResize() {
    if (!isResizing) return;
    isResizing = false;
    document.body.style.cursor = '';
    document.body.style.userSelect = '';
    savePanelWidth(); // Save on resize stop
  }

  // --- Theme management ---
  const systemDarkQuery = window.matchMedia('(prefers-color-scheme: dark)');

  function applyTheme(themeSetting) {
    let resolved;
    if (themeSetting === 'light' || themeSetting === 'dark') {
      resolved = themeSetting;
    } else {
      // 'system' — use OS preference
      resolved = systemDarkQuery.matches ? 'dark' : 'light';
    }
    if (resolved === 'dark') {
      document.documentElement.removeAttribute('data-theme');
    } else {
      document.documentElement.setAttribute('data-theme', 'light');
    }
  }

  // React to settings changes
  $: applyTheme($settingsStore.theme);

  // React to OS theme changes in real time (only when set to 'system')
  function onSystemThemeChange() {
    if ($settingsStore.theme === 'system' || !$settingsStore.theme) {
      applyTheme('system');
    }
  }
  systemDarkQuery.addEventListener('change', onSystemThemeChange);
  onDestroy(() => {
    systemDarkQuery.removeEventListener('change', onSystemThemeChange);
    OnFileDropOff();
  });

  // --- Drag-and-drop MIB import ---
  let dragOver = false;
  let dragLeaveTimer = null;

  function handleDragOver() {
    clearTimeout(dragLeaveTimer);
    dragOver = true;
  }
  function handleDragLeave() {
    // Small delay to avoid flicker when moving between child elements
    dragLeaveTimer = setTimeout(() => { dragOver = false; }, 80);
  }
  function handleDragEnd() {
    dragOver = false;
  }

  // MIB import result modal state
  let importErrors = [];   // [{ fileName, error }]
  let showImportErrors = false;

  async function handleFileDrop(_x, _y, paths) {
    dragOver = false;
    if (!paths || paths.length === 0) return;

    const t = get(_);
    const droppedNames = paths.map(p => p.replace(/.*[/\\]/, ''));

    try {
      // 1. Copy files to MIB directory
      const copyResults = await ImportMibFiles(paths);
      const copyFailed = copyResults.filter(r => !r.success);
      const skipped = copyResults.filter(r => r.success && r.skipped);
      const newlyImported = copyResults.filter(r => r.success && !r.skipped);
      const copiedNames = newlyImported.map(r => r.fileName);

      if (newlyImported.length === 0 && copyFailed.length === 0) {
        // Everything was skipped (already exists)
        notificationStore.add(t('app.mibDrop.allSkipped', { values: { count: skipped.length } }), 'info');
        return;
      }

      if (newlyImported.length === 0 && copyFailed.length > 0) {
        // All new files failed, only skips otherwise
        importErrors = copyFailed;
        showImportErrors = true;
        return;
      }

      // 2. Re-scan and reload MIBs (suppress default notification via silent flag)
      const defaultPath = await GetPersistentMibDirectory();
      const mibFiles = await ListMibFiles(defaultPath);
      mibPathsStore.setDetectedMibs(defaultPath, mibFiles);
      await mibStore.loadSilent();

      // 3. Check load diagnostics for the files we just dropped
      const diag = get(mibDiagnostics);
      const loadFailed = diag
        .filter(d => !d.success && copiedNames.includes(d.fileName))
        .map(d => ({ fileName: d.fileName, error: d.error || t('app.mibDrop.parseError') }));

      // 4. Combine copy failures + load/parse failures
      const allErrors = [...copyFailed, ...loadFailed];
      const fullySucceeded = copiedNames.length - loadFailed.length;

      // 5. Build notification message
      let msg = '';
      if (fullySucceeded > 0) {
        msg = t('app.mibDrop.success', { values: { count: fullySucceeded } });
      }
      if (skipped.length > 0) {
        const skipMsg = t('app.mibDrop.skipped', { values: { count: skipped.length } });
        msg = msg ? msg + ' — ' + skipMsg : skipMsg;
      }
      if (msg) {
        notificationStore.add(msg, allErrors.length > 0 ? 'info' : 'success');
      }

      if (allErrors.length > 0) {
        importErrors = allErrors;
        showImportErrors = true;
        if (fullySucceeded === 0 && !msg) {
          notificationStore.add(t('app.mibDrop.noFiles'), 'error');
        }
      }
    } catch (e) {
      console.error('MIB drop import failed:', e);
      importErrors = [{ fileName: droppedNames.join(', '), error: String(e) }];
      showImportErrors = true;
    }
  }

  // Initialize MIB paths store on startup
  onMount(async () => {
    // Load saved panel width
    loadPanelWidth();

    // Register Wails file drop handler
    OnFileDrop(handleFileDrop, true);

    mibPathsStore.load();

    // Scan default MIB directory
    try {
      const defaultPath = await GetPersistentMibDirectory();
      const mibFiles = await ListMibFiles(defaultPath);
      mibPathsStore.setDetectedMibs(defaultPath, mibFiles);

      // Now load the MIBs after paths are initialized
      await mibStore.load();
    } catch (e) {
      console.error('Failed to scan default MIB directory:', e);
    }
  });

  // Handle SNMP action from context menu
  function handleSnmpAction(event) {
    const { type, oid, name } = event.detail;
    activeTab = 'operations'; // Switch to operations tab
    pendingSnmpAction = { type, oid, name };

    // Clear the pending action after a short delay
    setTimeout(() => {
      pendingSnmpAction = null;
    }, 100);
  }

  // Global keyboard shortcuts
  function handleGlobalKeydown(event) {
    // Don't trigger shortcuts when typing in inputs
    const target = event.target;
    const isInput = target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT';

    // Ctrl+1/2/3 to switch tabs
    if (event.ctrlKey && !event.shiftKey && !event.altKey) {
      if (event.key === '1') {
        event.preventDefault();
        activeTab = 'operations';
      } else if (event.key === '2') {
        event.preventDefault();
        activeTab = 'traps';
      } else if (event.key === '3') {
        event.preventDefault();
        activeTab = 'history';
      } else if (event.key === '4') {
        event.preventDefault();
        activeTab = 'monitor';
      } else if (event.key === '5') {
        event.preventDefault();
        activeTab = 'discovery';
      } else if (event.key === ',') {
        // Ctrl+, to open settings (like VS Code)
        event.preventDefault();
        showSettings = true;
      }
    }

    // Ctrl+Shift+A to toggle anonymous mode
    if (event.ctrlKey && event.shiftKey && event.key === 'A') {
      event.preventDefault();
      settingsStore.save({ ...$settingsStore, anonymousMode: !$settingsStore.anonymousMode });
    }

    // Escape to close settings modal
    if (event.key === 'Escape' && showSettings) {
      showSettings = false;
    }

    // F5 to reload MIBs (only when not in an input)
    if (event.key === 'F5' && !isInput) {
      event.preventDefault();
      mibStore.load();
    }
  }
</script>

<svelte:window
  on:focus={() => trapStore.setWindowFocus(true)}
  on:blur={() => trapStore.setWindowFocus(false)}
  on:keydown={handleGlobalKeydown}
  on:mousemove={handleMouseMove}
  on:mouseup={stopResize}
  on:dragover|preventDefault={handleDragOver}
  on:dragleave={handleDragLeave}
  on:drop={handleDragEnd}
  on:dragend={handleDragEnd}
/>

{#if dragOver}
  <div class="drop-overlay">
    <div class="drop-content">
      <div class="drop-icon">📄</div>
      <div class="drop-text">{$_('app.mibDrop.overlay')}</div>
      <div class="drop-hint">{$_('app.mibDrop.overlayHint')}</div>
    </div>
  </div>
{/if}

<main style="--wails-drop-target:drop">
  <Notifications />
  {#if showSettings}
    <SettingsModal on:close={() => showSettings = false} />
  {/if}

  {#if showImportErrors}
    <div class="modal-backdrop" on:click={() => showImportErrors = false}>
      <div class="import-error-modal" on:click|stopPropagation>
        <div class="import-error-header">
          <span class="import-error-title">{$_('app.mibDrop.errorTitle')}</span>
          <button class="btn btn-small" on:click={() => showImportErrors = false}>&times;</button>
        </div>
        <div class="import-error-body">
          <p class="import-error-desc">{$_('app.mibDrop.errorDesc')}</p>
          <table class="import-error-table">
            <thead>
              <tr>
                <th>{$_('app.mibDrop.colFile')}</th>
                <th>{$_('app.mibDrop.colError')}</th>
              </tr>
            </thead>
            <tbody>
              {#each importErrors as err}
                <tr>
                  <td class="mono">{err.fileName}</td>
                  <td class="error-cell">{err.error}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  {/if}

  {#if showTargets}
    <div class="modal-backdrop" on:mousedown={() => showTargets = false}>
      <div class="targets-modal" on:mousedown|stopPropagation>
        <div class="targets-modal-header">
          <h2>🎯 {$_('targets.title', { values: { count: targetCount } })}</h2>
          <button class="close-btn" on:click={() => showTargets = false}>&times;</button>
        </div>
        <div class="targets-modal-body">
          <TargetManager />
        </div>
      </div>
    </div>
  {/if}

  <div class="top-bar">
    <div class="top-bar-left">
      <h1>{$_('app.title')}</h1>
      <button class="targets-btn" on:click={() => showTargets = true} title={$_('app.status.targetsTitle')}>
        🎯 {$_('common.target')}
        <span class="targets-badge">{targetCount}</span>
      </button>
    </div>
    <div class="status-bar">
      <div
        class="status-item"
        class:active={$trapStore.isListening}
        title={$trapStore.isListening ? $_('app.status.trapListening', { values: { port: $settingsStore.trapPort } }) : $_('app.status.trapInactive')}
      >
        <span class="status-icon">📡</span>
        <span class="status-label">{$_('app.status.trap')}</span>
        <span class="status-indicator" class:listening={$trapStore.isListening}>
          {$trapStore.isListening ? $settingsStore.trapPort : $_('app.status.trapOff')}
        </span>
      </div>
      <div class="status-separator"></div>
      <div class="version-switcher" title={$_('app.status.snmpVersionTitle')}>
        {#each ['v1', 'v2c', 'v3'] as ver}
          <button
            class="version-btn"
            class:active={$settingsStore.snmpVersion === ver}
            on:click={() => settingsStore.save({ ...$settingsStore, snmpVersion: ver })}
          >
            {ver}
          </button>
        {/each}
      </div>
    </div>
    <button class="btn debug-toggle-btn" class:active={showDebug} on:click={() => showDebug = !showDebug} title={$_('debug.title')}>
      🔍 Debug
    </button>
    <button class="btn settings-btn" on:click={() => showSettings = true} title={$_('app.settingsTooltip')}>
      ⚙️ {$_('app.settings')}
    </button>
  </div>

  <div class="container">
    <div class="mib-panel-container" style="width: {mibPanelWidth}px;">
      <MibPanel 
        on:select={(e) => selectedNode = e.detail.node}
        on:snmpAction={handleSnmpAction}
      />
      <div 
        class="resize-handle" 
        on:mousedown={startResize}
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize sidebar"
      >
        <div class="resize-handle-inner"></div>
      </div>
    </div>

    <div class="main-content">
      <div class="tabs">
        <button class="tab-btn" class:active={activeTab === 'operations'} on:click={() => activeTab = 'operations'} title="Ctrl+1">
          {$_('app.tabs.operations')}
          <span class="shortcut-hint">1</span>
        </button>
        <button class="tab-btn" class:active={activeTab === 'traps'} on:click={() => activeTab = 'traps'} title="Ctrl+2">
          {$_('app.tabs.traps')}
          {#if $trapStore.traps.length > 0}
            <span class="badge">{$trapStore.traps.length}</span>
          {/if}
          {#if $trapStore.isListening}
            <span class="listening-indicator" title={$_('app.status.trapListening', { values: { port: $settingsStore.trapPort } })}>●</span>
          {/if}
          <span class="shortcut-hint">2</span>
        </button>
        <button class="tab-btn" class:active={activeTab === 'history'} on:click={() => activeTab = 'history'} title="Ctrl+3">
          {$_('app.tabs.history')}
          <span class="shortcut-hint">3</span>
        </button>
        <button class="tab-btn" class:active={activeTab === 'monitor'} on:click={() => activeTab = 'monitor'} title="Ctrl+4">
          {$_('app.tabs.monitor')}
          {#if $pollingStore.some(s => s.running)}
            <span class="listening-indicator" title={$_('app.status.trapListening', { values: { port: $settingsStore.trapPort } })}>●</span>
          {/if}
          <span class="shortcut-hint">4</span>
        </button>
        <button class="tab-btn" class:active={activeTab === 'discovery'} on:click={() => activeTab = 'discovery'} title="Ctrl+5">
          {$_('app.tabs.network')}
          <span class="shortcut-hint">5</span>
        </button>
        {#if $settingsStore.anonymousMode}
          <span class="anon-badge" title={$_('settings.general.anonymousMode') + ' (Ctrl+Shift+A)'}>ANON</span>
        {/if}
      </div>

      {#if activeTab === 'operations'}
        <OperationsPanel 
          selectedNode={selectedNode} 
          pendingAction={pendingSnmpAction}
        />
      {/if}

      {#if activeTab === 'traps'}
        <TrapPanel />
      {/if}

      {#if activeTab === 'history'}
        <HistoryPanel />
      {/if}

      {#if activeTab === 'monitor'}
        <MonitorPanel />
      {/if}

      {#if activeTab === 'discovery'}
        <DiscoveryPanel />
      {/if}

      {#if showDebug}
        <DebugPanel />
      {/if}
    </div>
  </div>
</main>

<style>
  .top-bar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0 20px;
    background-color: var(--bg-lighter-color);
    color: white;
    height: 50px;
    border-bottom: 1px solid var(--border-color);
  }

  .top-bar h1 {
    font-size: 1.2em;
    font-weight: 500;
    margin: 0;
  }

  .top-bar-left {
    display: flex;
    align-items: center;
    gap: 16px;
  }

  .targets-btn {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 14px;
    background-color: var(--accent-subtle-strong);
    border: 1px solid var(--accent-border-strong);
    border-radius: 6px;
    color: var(--text-color);
    font-size: 0.88em;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
    white-space: nowrap;
  }

  .targets-btn:hover {
    background-color: var(--accent-border);
    border-color: var(--accent-color);
  }

  .targets-badge {
    background-color: var(--accent-color);
    color: white;
    font-size: 0.8em;
    font-weight: 700;
    padding: 1px 7px;
    border-radius: 10px;
    min-width: 20px;
    text-align: center;
  }

  /* Version Switcher */
  .version-switcher {
    display: flex;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
  }

  .version-btn {
    padding: 5px 12px;
    background: transparent;
    border: none;
    border-right: 1px solid var(--border-color);
    color: var(--text-muted);
    font-size: 0.85em;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.15s;
  }

  .version-btn:last-child {
    border-right: none;
  }

  .version-btn:hover {
    background-color: var(--hover-overlay);
    color: var(--text-color);
  }

  .version-btn.active {
    background-color: var(--accent-color);
    color: white;
  }

  /* Status bar styles */
  .status-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    background-color: var(--bg-color);
    padding: 6px 12px;
    border-radius: 6px;
    font-size: 0.85em;
  }

  .status-item {
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--text-dimmed);
    background: none;
    border: none;
    font: inherit;
    padding: 0;
    cursor: default;
  }

  .status-item.active {
    color: var(--success-color);
  }

  .status-icon {
    font-size: 1em;
  }

  .status-label {
    color: var(--text-muted);
  }

  .status-separator {
    width: 1px;
    height: 20px;
    background-color: var(--border-color);
    margin: 0 4px;
  }

  .status-indicator {
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 0.85em;
    font-weight: 600;
    background-color: var(--hover-overlay-medium);
    color: var(--text-muted);
  }

  .status-indicator.listening {
    background-color: var(--success-subtle-strong);
    color: var(--success-color);
    animation: pulse 2s infinite;
  }

  .debug-toggle-btn {
    font-size: 0.85em;
    padding: 6px 12px;
  }

  .debug-toggle-btn.active {
    background-color: var(--success-color);
  }

  .settings-btn {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .container {
    display: flex;
    height: calc(100vh - 50px);
    position: relative;
  }

  .mib-panel-container {
    position: relative;
    flex-shrink: 0;
    display: flex;
    height: 100%;
    min-width: 250px;
    max-width: 800px;
  }

  /* Resize handle */
  .resize-handle {
    position: absolute;
    top: 0;
    right: -5px;
    width: 10px;
    height: 100%;
    cursor: col-resize;
    z-index: 10;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .resize-handle:hover .resize-handle-inner {
    background-color: var(--accent-color);
    opacity: 1;
  }

  .resize-handle-inner {
    width: 3px;
    height: 60px;
    background-color: var(--border-color);
    border-radius: 2px;
    opacity: 0.5;
    transition: all 0.2s;
  }

  .resize-handle:active .resize-handle-inner {
    background-color: var(--accent-hover-color);
    opacity: 1;
    height: 100px;
  }

  .main-content {
    flex-grow: 1;
    padding: 10px;
    overflow-y: auto;
  }

  .tabs {
    margin-bottom: 10px;
  }

  .tab-btn {
    padding: 10px 15px;
    border: none;
    background-color: transparent;
    color: var(--text-color);
    cursor: pointer;
    border-bottom: 2px solid transparent;
  }

  .tab-btn.active {
    border-bottom: 2px solid var(--accent-color);
    font-weight: bold;
  }

  .badge {
    background-color: var(--accent-color);
    color: white;
    font-size: 0.75em;
    padding: 2px 6px;
    border-radius: 10px;
    margin-left: 6px;
    font-weight: 600;
  }

  .listening-indicator {
    color: var(--success-color);
    margin-left: 4px;
    animation: pulse 1.5s infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }

  .shortcut-hint {
    font-size: 0.7em;
    opacity: 0.4;
    margin-left: 4px;
    padding: 1px 4px;
    border: 1px solid currentColor;
    border-radius: 3px;
    font-weight: normal;
  }

  .tab-btn:hover .shortcut-hint {
    opacity: 0.7;
  }

  .anon-badge {
    margin-left: auto;
    padding: 2px 10px;
    font-size: 0.75em;
    font-weight: 700;
    letter-spacing: 0.05em;
    color: #fff;
    background-color: var(--warning-color);
    border-radius: var(--radius-full);
    cursor: default;
    user-select: none;
    animation: anon-pulse 2s ease-in-out infinite;
  }

  @keyframes anon-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.7; }
  }

  /* Drop overlay */
  :global(.drop-overlay) {
    position: fixed;
    inset: 0;
    z-index: 9999;
    background: var(--backdrop-color-strong);
    display: flex;
    align-items: center;
    justify-content: center;
    pointer-events: none;
  }

  :global(.drop-content) {
    text-align: center;
    padding: 40px 60px;
    border: 3px dashed var(--accent-color);
    border-radius: 16px;
    background: var(--bg-lighter-color);
  }

  :global(.drop-icon) {
    font-size: 3em;
    margin-bottom: 12px;
  }

  :global(.drop-text) {
    font-size: 1.3em;
    font-weight: 700;
    color: var(--text-color);
  }

  :global(.drop-hint) {
    font-size: 0.9em;
    color: var(--text-muted);
    margin-top: 6px;
  }

  /* Import error modal */
  .import-error-modal {
    background: var(--bg-light-color);
    border: 1px solid var(--error-border);
    border-radius: var(--radius-lg);
    width: 520px;
    max-width: 90vw;
    max-height: 70vh;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .import-error-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border-color);
    background: var(--error-subtle);
  }

  .import-error-title {
    font-weight: 700;
    color: var(--error-color);
  }

  .import-error-body {
    padding: 16px;
    overflow-y: auto;
  }

  .import-error-desc {
    margin: 0 0 12px;
    font-size: 0.9em;
    color: var(--text-muted);
  }

  .import-error-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85em;
  }

  .import-error-table th,
  .import-error-table td {
    padding: 6px 10px;
    text-align: left;
    border-bottom: 1px solid var(--border-color);
  }

  .import-error-table th {
    font-weight: 600;
    background: var(--shadow-color);
  }

  .import-error-table .mono {
    font-family: 'Courier New', monospace;
  }

  .import-error-table .error-cell {
    color: var(--error-color);
    word-break: break-word;
  }

  /* Targets Modal */
  .modal-backdrop {
    position: fixed;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    background-color: var(--backdrop-color);
    display: flex;
    justify-content: center;
    align-items: center;
    z-index: 100;
  }

  .targets-modal {
    background-color: var(--bg-light-color);
    border-radius: 8px;
    width: 90%;
    max-width: 600px;
    max-height: 80vh;
    box-shadow: 0 5px 15px var(--shadow-color-strong);
    display: flex;
    flex-direction: column;
  }

  .targets-modal-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 15px 20px;
    border-bottom: 1px solid var(--border-color);
  }

  .targets-modal-header h2 {
    margin: 0;
    font-size: 1.2em;
  }

  .close-btn {
    background: none;
    border: none;
    color: var(--text-color);
    font-size: 1.5rem;
    cursor: pointer;
  }

  .targets-modal-body {
    padding: 20px;
    overflow-y: auto;
  }
</style>
