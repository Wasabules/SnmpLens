<script>
  import { createEventDispatcher, onMount } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { settingsStore } from './stores/settingsStore';
  import GeneralSettings from './settings/GeneralSettings.svelte';
  import MibSettings from './settings/MibSettings.svelte';
  import SnmpSettings from './settings/SnmpSettings.svelte';

  const dispatch = createEventDispatcher();

  let activeTab = 'general';
  let settings;
  let defaultMibPath = '';

  settingsStore.subscribe(value => {
    settings = JSON.parse(JSON.stringify(value));
  });

  onMount(async () => {
    try {
      const { GetPersistentMibDirectory } = await import('../wailsjs/go/main/App');
      defaultMibPath = await GetPersistentMibDirectory();
    } catch (e) {
      console.error('Failed to get default MIB path:', e);
    }
  });

  function handleSave() {
    settingsStore.save(settings);
    dispatch('close');
  }

  function handleCancel() {
    dispatch('close');
  }
</script>

<div class="modal-backdrop" on:mousedown={handleCancel}>
  <div class="modal" on:mousedown|stopPropagation>
    <div class="modal-header">
      <h2>⚙️ {$_('settings.title')}</h2>
      <button class="close-btn" on:click={handleCancel}>&times;</button>
    </div>

    <!-- Tab Navigation -->
    <div class="tabs">
      <button
        class="tab"
        class:active={activeTab === 'general'}
        on:click={() => activeTab = 'general'}
      >
        🔧 {$_('settings.tabs.general')}
      </button>
      <button
        class="tab"
        class:active={activeTab === 'mibs'}
        on:click={() => activeTab = 'mibs'}
      >
        📚 {$_('settings.tabs.mibs')}
      </button>
      <button
        class="tab"
        class:active={activeTab === 'snmp'}
        on:click={() => activeTab = 'snmp'}
      >
        🌐 {$_('settings.tabs.snmp')}
      </button>
    </div>

    <div class="modal-content">
      {#if activeTab === 'general'}
        <GeneralSettings bind:settings />
      {/if}

      {#if activeTab === 'mibs'}
        <MibSettings {defaultMibPath} />
      {/if}

      {#if activeTab === 'snmp'}
        <SnmpSettings bind:settings />
      {/if}
    </div>

    <div class="modal-actions">
      <button class="btn tertiary" on:click={settingsStore.reset}>{$_('common.reset')}</button>
      <div class="main-actions">
        <button class="btn secondary" on:click={handleCancel}>{$_('common.cancel')}</button>
        <button class="btn" on:click={handleSave}>{$_('common.save')}</button>
      </div>
    </div>
  </div>
</div>

<style>
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

  .modal {
    background-color: var(--bg-light-color);
    padding: 0;
    border-radius: 8px;
    width: 90%;
    max-width: 650px;
    box-shadow: 0 5px 15px var(--shadow-color-strong);
    display: flex;
    flex-direction: column;
  }

  .modal-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 15px 20px;
    border-bottom: 1px solid var(--border-color);
  }

  .modal-header h2 {
    margin: 0;
  }

  .close-btn {
    background: none;
    border: none;
    color: var(--text-color);
    font-size: 1.5rem;
    cursor: pointer;
  }

  /* Tabs */
  .tabs {
    display: flex;
    background-color: var(--bg-lighter-color);
    border-bottom: 1px solid var(--border-color);
  }

  .tab {
    flex: 1;
    padding: 12px 16px;
    background: transparent;
    border: none;
    border-bottom: 3px solid transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-weight: 500;
    transition: all 0.2s;
  }

  .tab:hover {
    background-color: var(--hover-overlay);
    color: var(--text-color);
  }

  .tab.active {
    color: var(--accent-color);
    border-bottom-color: var(--accent-color);
    background-color: var(--accent-subtle-medium);
  }

  .modal-content {
    padding: 20px;
    max-height: 70vh;
    overflow-y: auto;
  }

  .modal-actions {
    padding: 15px 20px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    border-top: 1px solid var(--border-color);
    background-color: var(--bg-lighter-color);
    border-bottom-left-radius: 8px;
    border-bottom-right-radius: 8px;
  }

  .main-actions {
    display: flex;
    gap: 10px;
  }

  /* Secondary action button */
  .btn.secondary {
    background-color: var(--bg-disabled);
    color: var(--text-color);
  }
  .btn.secondary:hover {
    background-color: var(--bg-disabled-hover);
  }

  /* Tertiary/ghost action button */
  .btn.tertiary {
    background-color: transparent;
    border: 1px solid var(--border-color);
    color: var(--text-color);
  }
  .btn.tertiary:hover {
    background-color: var(--bg-lighter-color);
    border-color: var(--border-hover);
  }
</style>
