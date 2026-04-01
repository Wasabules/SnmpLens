<script>
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';
  import { createEventDispatcher } from 'svelte';
  import { settingsStore } from './stores/settingsStore';
  import { notificationStore } from './stores/notifications';
  import { TestConnection } from '../wailsjs/go/main/App';
  import { buildTestRequest } from './utils/snmpParams';
  import { getEffectiveSettings } from './utils/targets';
  import TargetOverrideForm from './TargetOverrideForm.svelte';
  import { anonMode, anonymizeIp } from './utils/anonymize';

  const dispatch = createEventDispatcher();

  function parseTargets(targetsString) {
    if (!targetsString) return [];
    return targetsString.split('\n')
      .map((line, index) => {
        const trimmed = line.trim();
        if (!trimmed) return null;
        const isDisabled = trimmed.startsWith('//');
        const withoutPrefix = isDisabled ? trimmed.substring(2).trim() : trimmed;
        const parts = withoutPrefix.split('#');
        const address = parts[0].trim();
        const label = parts[1]?.trim() || '';
        return { id: index, address, label, enabled: !isDisabled, testing: false, status: null };
      })
      .filter(t => t !== null);
  }

  function serializeTargets(targets) {
    return targets.map(t => {
      let line = t.enabled ? t.address : '//' + t.address;
      if (t.label) line += ' # ' + t.label;
      return line;
    }).join('\n');
  }

  let targets = parseTargets($settingsStore.targets);
  let newAddress = '';
  let newLabel = '';
  let showAddForm = false;
  let expandedOverrideId = null;
  let selectedGroupId = 'all';
  let showGroupMenu = false;
  let newGroupName = '';

  $: {
    const parsed = parseTargets($settingsStore.targets);
    if (JSON.stringify(parsed.map(t => ({a: t.address, l: t.label, e: t.enabled}))) !==
        JSON.stringify(targets.map(t => ({a: t.address, l: t.label, e: t.enabled})))) {
      targets = parsed;
    }
  }

  $: groups = $settingsStore.targetGroups || [{ id: 'default', name: 'Default' }];
  $: assignments = $settingsStore.targetGroupAssignments || {};

  function getGroupForTarget(address) {
    return assignments[address] || 'default';
  }

  function getGroupTargetCount(groupId) {
    if (groupId === 'all') return targets.length;
    return targets.filter(t => getGroupForTarget(t.address) === groupId).length;
  }

  $: filteredTargets = selectedGroupId === 'all'
    ? targets
    : targets.filter(t => getGroupForTarget(t.address) === selectedGroupId);

  $: enabledTargets = targets.filter(t => t.enabled);
  $: enabledCount = enabledTargets.length;

  function saveTargets() {
    settingsStore.save({ ...$settingsStore, targets: serializeTargets(targets) });
  }

  function addTarget() {
    if (!newAddress.trim()) return;
    targets = [...targets, {
      id: Date.now(), address: newAddress.trim(), label: newLabel.trim(),
      enabled: true, testing: false, status: null
    }];
    // Assign to current group (or default)
    const groupId = selectedGroupId === 'all' ? 'default' : selectedGroupId;
    const newAssignments = { ...($settingsStore.targetGroupAssignments || {}), [newAddress.trim()]: groupId };
    newAddress = '';
    newLabel = '';
    showAddForm = false;
    settingsStore.save({ ...$settingsStore, targets: serializeTargets(targets), targetGroupAssignments: newAssignments });
  }

  function removeTarget(id) {
    const target = targets.find(t => t.id === id);
    targets = targets.filter(t => t.id !== id);
    const overrides = { ...($settingsStore.targetOverrides || {}) };
    const assigns = { ...($settingsStore.targetGroupAssignments || {}) };
    if (target) {
      delete overrides[target.address];
      delete assigns[target.address];
    }
    settingsStore.save({ ...$settingsStore, targets: serializeTargets(targets), targetOverrides: overrides, targetGroupAssignments: assigns });
  }

  function toggleTarget(id) {
    targets = targets.map(t => t.id === id ? { ...t, enabled: !t.enabled } : t);
    saveTargets();
  }

  function updateLabel(id, label) {
    targets = targets.map(t => t.id === id ? { ...t, label } : t);
    saveTargets();
  }

  async function testTarget(id) {
    const target = targets.find(t => t.id === id);
    if (!target) return;
    targets = targets.map(t => t.id === id ? { ...t, testing: true, status: null } : t);
    try {
      const effectiveSettings = getEffectiveSettings($settingsStore, target.address);
      const result = await TestConnection(buildTestRequest(effectiveSettings, target.address));
      const success = !result.error;
      targets = targets.map(t => t.id === id ? { ...t, testing: false, status: success ? 'success' : 'error' } : t);
      const t = get(_);
      if (success) {
        notificationStore.add(t('targets.connectionOk', { values: { address: target.address } }), 'success');
      } else {
        notificationStore.add(t('targets.connectionFailed', { values: { address: target.address, error: result.error } }), 'error');
      }
    } catch (err) {
      targets = targets.map(t => t.id === id ? { ...t, testing: false, status: 'error' } : t);
      const t = get(_);
      notificationStore.add(t('targets.connectionFailed', { values: { address: target.address, error: err } }), 'error');
    }
  }

  async function testAllTargets() {
    for (const target of enabledTargets) {
      await testTarget(target.id);
    }
  }

  function hasOverrides(address) {
    const ov = $settingsStore.targetOverrides?.[address];
    return ov && Object.keys(ov).length > 0;
  }

  function toggleOverrides(id) {
    expandedOverrideId = expandedOverrideId === id ? null : id;
  }

  function saveOverride(address, overrideData) {
    const overrides = { ...($settingsStore.targetOverrides || {}) };
    const cleaned = Object.fromEntries(Object.entries(overrideData).filter(([, v]) => v !== undefined && v !== null && v !== ''));
    if (Object.keys(cleaned).length > 0) overrides[address] = cleaned;
    else delete overrides[address];
    settingsStore.save({ ...$settingsStore, targetOverrides: overrides });
    expandedOverrideId = null;
  }

  function clearOverride(address) {
    const overrides = { ...($settingsStore.targetOverrides || {}) };
    delete overrides[address];
    settingsStore.save({ ...$settingsStore, targetOverrides: overrides });
    expandedOverrideId = null;
  }

  // --- Group management ---

  function addGroup() {
    if (!newGroupName.trim()) return;
    const id = 'grp-' + Date.now();
    const updatedGroups = [...groups, { id, name: newGroupName.trim() }];
    settingsStore.save({ ...$settingsStore, targetGroups: updatedGroups });
    newGroupName = '';
    showGroupMenu = false;
    selectedGroupId = id;
  }

  function renameGroup(groupId, newName) {
    if (!newName.trim() || groupId === 'default') return;
    const updatedGroups = groups.map(g => g.id === groupId ? { ...g, name: newName.trim() } : g);
    settingsStore.save({ ...$settingsStore, targetGroups: updatedGroups });
  }

  function deleteGroup(groupId) {
    if (groupId === 'default') return;
    const t = get(_);
    if (!confirm(t('targets.groups.deleteConfirm'))) return;
    // Move targets to default
    const assigns = { ...($settingsStore.targetGroupAssignments || {}) };
    for (const [addr, gid] of Object.entries(assigns)) {
      if (gid === groupId) assigns[addr] = 'default';
    }
    const updatedGroups = groups.filter(g => g.id !== groupId);
    settingsStore.save({ ...$settingsStore, targetGroups: updatedGroups, targetGroupAssignments: assigns });
    if (selectedGroupId === groupId) selectedGroupId = 'all';
  }

  function moveTargetToGroup(address, groupId) {
    const assigns = { ...($settingsStore.targetGroupAssignments || {}), [address]: groupId };
    settingsStore.save({ ...$settingsStore, targetGroupAssignments: assigns });
  }
</script>

<div class="target-manager">
  <!-- Group tabs -->
  <div class="group-tabs">
    <button class="group-tab" class:active={selectedGroupId === 'all'} on:click={() => selectedGroupId = 'all'}>
      {$_('targets.groups.all')} <span class="group-count">{targets.length}</span>
    </button>
    {#each groups as group (group.id)}
      <button
        class="group-tab"
        class:active={selectedGroupId === group.id}
        on:click={() => selectedGroupId = group.id}
        on:dblclick={() => {
          if (group.id !== 'default') {
            const newName = prompt($_('targets.groups.renamePrompt'), group.name);
            if (newName) renameGroup(group.id, newName);
          }
        }}
        title={group.id !== 'default' ? $_('targets.groups.dblClickRename') : ''}
      >
        {group.name} <span class="group-count">{getGroupTargetCount(group.id)}</span>
        {#if group.id !== 'default'}
          <button class="group-delete" on:click|stopPropagation={() => deleteGroup(group.id)} title={$_('common.delete')}>✕</button>
        {/if}
      </button>
    {/each}
    <button class="group-tab group-add" on:click={() => showGroupMenu = !showGroupMenu} title={$_('targets.groups.addGroup')}>+</button>
  </div>

  {#if showGroupMenu}
    <div class="group-add-form">
      <input
        type="text"
        bind:value={newGroupName}
        placeholder={$_('targets.groups.namePlaceholder')}
        on:keydown={(e) => e.key === 'Enter' && addGroup()}
      />
      <button class="btn-sm primary" on:click={addGroup} disabled={!newGroupName.trim()}>{$_('common.add')}</button>
      <button class="btn-sm" on:click={() => { showGroupMenu = false; newGroupName = ''; }}>{$_('common.cancel')}</button>
    </div>
  {/if}

  <!-- Header actions -->
  <div class="target-header">
    <div class="target-actions">
      <button class="btn-sm" on:click={testAllTargets} disabled={enabledCount === 0} title={$_('targets.testAllTooltip')}>
        🔍 {$_('targets.testAll')}
      </button>
      <button class="btn-sm primary" on:click={() => showAddForm = !showAddForm}>
        {showAddForm ? '✕' : $_('targets.addButton')}
      </button>
    </div>
  </div>

  {#if showAddForm}
    <div class="add-form">
      <input type="text" bind:value={newAddress} placeholder={$_('targets.addressPlaceholder')} on:keydown={(e) => e.key === 'Enter' && addTarget()} />
      <input type="text" bind:value={newLabel} placeholder={$_('targets.labelPlaceholder')} on:keydown={(e) => e.key === 'Enter' && addTarget()} />
      <button class="btn-sm primary" on:click={addTarget} disabled={!newAddress.trim()}>{$_('common.add')}</button>
    </div>
  {/if}

  <div class="target-list">
    {#if filteredTargets.length === 0}
      <div class="empty-state">{$_('targets.empty')}</div>
    {:else}
      {#each filteredTargets as target (target.id)}
        <div class="target-entry">
          <div class="target-item" class:disabled={!target.enabled}>
            <label class="target-checkbox">
              <input type="checkbox" checked={target.enabled} on:change={() => toggleTarget(target.id)} />
            </label>
            <div class="target-info">
              <span class="target-address" class:disabled={!target.enabled}>
                {$anonMode ? anonymizeIp(target.address) : target.address}
                {#if hasOverrides(target.address)}
                  <span class="override-badge" title={$_('targets.overrides.badge')}>⚙</span>
                {/if}
              </span>
              <input
                type="text" class="target-label-input" value={target.label}
                placeholder={$_('targets.labelPlaceholder')}
                on:blur={(e) => updateLabel(target.id, e.target.value)}
                on:keydown={(e) => e.key === 'Enter' && e.target.blur()}
              />
            </div>
            <div class="target-status">
              {#if target.testing}
                <span class="status-icon testing">⏳</span>
              {:else if target.status === 'success'}
                <span class="status-icon success" title={$_('targets.statusSuccess')}>✅</span>
              {:else if target.status === 'error'}
                <span class="status-icon error" title={$_('targets.statusError')}>❌</span>
              {/if}
            </div>
            <div class="target-buttons">
              {#if groups.length > 1}
                <select
                  class="group-select"
                  value={getGroupForTarget(target.address)}
                  on:change={(e) => moveTargetToGroup(target.address, e.target.value)}
                  title={$_('targets.groups.moveToGroup')}
                >
                  {#each groups as g}
                    <option value={g.id}>{g.name}</option>
                  {/each}
                </select>
              {/if}
              <button class="btn-icon" class:active={expandedOverrideId === target.id}
                on:click={() => toggleOverrides(target.id)} title={$_('targets.overrides.title')}>
                {hasOverrides(target.address) ? '⚙️' : '⚙'}
              </button>
              <button class="btn-icon" on:click={() => testTarget(target.id)}
                disabled={target.testing || !target.enabled} title={$_('targets.testTooltip')}>🔍</button>
              <button class="btn-icon danger" on:click={() => removeTarget(target.id)}
                title={$_('targets.deleteTooltip')}>🗑️</button>
            </div>
          </div>
          {#if expandedOverrideId === target.id}
            <TargetOverrideForm
              overrides={$settingsStore.targetOverrides?.[target.address] || {}}
              globalSettings={$settingsStore}
              on:save={(e) => saveOverride(target.address, e.detail)}
              on:clear={() => clearOverride(target.address)}
            />
          {/if}
        </div>
      {/each}
    {/if}
  </div>
</div>

<style>
  .target-manager { padding: 0; }

  /* Group tabs */
  .group-tabs {
    display: flex;
    gap: 4px;
    margin-bottom: 12px;
    flex-wrap: wrap;
    border-bottom: 1px solid var(--border-color);
    padding-bottom: 8px;
  }

  .group-tab {
    padding: 4px 12px;
    background: transparent;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-muted);
    font-size: 0.82em;
    cursor: pointer;
    transition: all 0.15s;
    display: flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
  }

  .group-tab:hover { background-color: var(--hover-overlay); color: var(--text-color); }
  .group-tab.active { background-color: var(--accent-color); border-color: var(--accent-color); color: white; }

  .group-count {
    font-size: 0.85em;
    opacity: 0.7;
    background: var(--hover-overlay-strong);
    padding: 0 5px;
    border-radius: 8px;
  }

  .group-delete {
    background: none;
    border: none;
    color: inherit;
    font-size: 0.8em;
    cursor: pointer;
    opacity: 0.5;
    padding: 0 2px;
  }
  .group-delete:hover { opacity: 1; }

  .group-tab.group-add {
    border-style: dashed;
    font-size: 1em;
    padding: 4px 10px;
  }

  .group-add-form {
    display: flex;
    gap: 8px;
    margin-bottom: 10px;
    padding: 8px;
    background-color: var(--bg-color);
    border-radius: 4px;
  }

  .group-add-form input {
    flex: 1;
    padding: 5px 8px;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    background-color: var(--bg-lighter-color);
    color: var(--text-color);
    font-size: 0.88em;
  }

  .group-select {
    padding: 2px 4px;
    font-size: 0.78em;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 3px;
    color: var(--text-dimmed);
    cursor: pointer;
    max-width: 80px;
  }

  /* Target list */
  .target-header { display: flex; justify-content: flex-end; align-items: center; margin-bottom: 10px; }
  .target-actions { display: flex; gap: 8px; }

  .btn-sm { padding: 4px 10px; font-size: 0.85em; border: 1px solid var(--border-color); background: transparent; color: var(--text-color); border-radius: 4px; cursor: pointer; transition: all 0.2s; }
  .btn-sm:hover:not(:disabled) { background-color: var(--bg-color); }
  .btn-sm.primary { background-color: var(--accent-color); border-color: var(--accent-color); color: white; }
  .btn-sm.primary:hover:not(:disabled) { background-color: var(--accent-hover-color); }
  .btn-sm:disabled { opacity: 0.5; cursor: not-allowed; }

  .add-form { display: flex; gap: 8px; margin-bottom: 10px; padding: 10px; background-color: var(--bg-color); border-radius: 4px; }
  .add-form input { flex: 1; padding: 6px 10px; border: 1px solid var(--border-color); border-radius: 4px; background-color: var(--bg-lighter-color); color: var(--text-color); font-size: 0.9em; }
  .add-form input:first-child { flex: 2; }

  .target-list { display: flex; flex-direction: column; gap: 6px; max-height: 400px; overflow-y: auto; }
  .target-entry { display: flex; flex-direction: column; }
  .target-item { display: flex; align-items: center; gap: 10px; padding: 8px 10px; background-color: var(--bg-color); border-radius: 4px; transition: opacity 0.2s; }
  .target-item.disabled { opacity: 0.5; }

  .target-checkbox { display: flex; align-items: center; }
  .target-checkbox input { width: 16px; height: 16px; cursor: pointer; }

  .target-info { flex: 1; display: flex; align-items: center; gap: 10px; min-width: 0; }
  .target-address { font-family: 'Courier New', monospace; font-size: 0.9em; font-weight: 500; white-space: nowrap; display: flex; align-items: center; gap: 4px; }
  .target-address.disabled { text-decoration: line-through; color: var(--text-muted); }
  .override-badge { font-size: 0.75em; color: var(--accent-color); }

  .target-label-input { flex: 1; padding: 4px 8px; border: 1px solid transparent; border-radius: 3px; background: transparent; color: var(--text-muted); font-size: 0.85em; min-width: 80px; }
  .target-label-input:hover, .target-label-input:focus { border-color: var(--border-color); background-color: var(--bg-lighter-color); }
  .target-label-input:focus { outline: none; border-color: var(--accent-color); }

  .target-status { width: 24px; text-align: center; }
  .status-icon { font-size: 0.9em; }
  .status-icon.testing { animation: pulse 1s infinite; }

  .target-buttons { display: flex; gap: 4px; align-items: center; }
  .btn-icon { background: transparent; border: none; cursor: pointer; padding: 4px 6px; font-size: 0.85em; opacity: 0.6; transition: opacity 0.2s; border-radius: 3px; }
  .btn-icon:hover:not(:disabled) { opacity: 1; background-color: var(--hover-overlay-medium); }
  .btn-icon:disabled { opacity: 0.3; cursor: not-allowed; }
  .btn-icon.active { opacity: 1; background-color: var(--accent-subtle-strong); }
  .btn-icon.danger:hover:not(:disabled) { background-color: var(--error-subtle-strong); }

  .empty-state { text-align: center; padding: 20px; color: var(--text-muted); font-size: 0.9em; }

  @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
</style>
