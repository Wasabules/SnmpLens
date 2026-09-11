<script>
  import { _ } from 'svelte-i18n';
  import { createEventDispatcher, onMount } from 'svelte';
  import { onBackdrop } from './utils/modal';
  import { simulatorStore } from './stores/simulatorStore';
  import { settingsStore } from './stores/settingsStore';
  import { notificationStore } from './stores/notifications';
  import {
    SIM_VERSIONS, blankDevice, editableDevice, deviceProblems, devicePayload, addDeviceAsTarget,
  } from './utils/simulator.js';
  import UsmFields from './settings/UsmFields.svelte';
  import Icon from './Icon.svelte';

  /**
   * The simulated devices: a list to start, stop and add as targets, and an
   * editor for one device at a time. Go keeps the devices, their secrets and the
   * loopback rule; this shows, and asks.
   */
  const dispatch = createEventDispatcher();

  /** The device being edited, in the editor's shape — or null for the list. */
  let editing = null;
  let saving = false;
  let saveError = '';
  /** Devices with a start, a stop or a removal on the way, by id. */
  let busy = {};
  /** The device whose delete button has been pressed once. */
  let confirmDelete = null;

  // A running device's request count moves and nothing announces it, so the
  // list is re-read while the dialog is open.
  onMount(() => {
    simulatorStore.refresh().catch((e) => notificationStore.add(String(e), 'error'));
    const timer = setInterval(() => simulatorStore.refresh().catch(() => {}), 2000);
    return () => clearInterval(timer);
  });

  $: problems = editing ? deviceProblems(editing) : {};
  $: usmProblems = usmMessages(problems, $_);

  /** The editor's SNMPv3 problems, in the shape UsmFields shows them. */
  function usmMessages(p, t) {
    const out = {};
    for (const key of ['user', 'authPass', 'privPass']) {
      if (p[key]) out[key] = { text: t(`simulator.problem.${p[key]}`), level: 'error' };
    }
    return out;
  }

  function close() {
    dispatch('close');
  }

  // The editor first, the dialog after: Escape backs out one level at a time.
  function onWindowKey(event) {
    if (event.key !== 'Escape') return;
    if (editing) {
      editing = null;
      return;
    }
    close();
  }

  async function newDevice(models, devices) {
    const model = models[0]?.id || 'linux-server';
    const suggestion = await simulatorStore.suggestAddress().catch(() => null);
    editing = blankDevice(model, suggestion);
    editing.name = `${$_(`simulator.model.${model}.name`)} ${devices.length + 1}`;
    saveError = '';
  }

  async function edit(device) {
    try {
      editing = editableDevice(device, await simulatorStore.credentials(device.id));
      saveError = '';
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  function toggleVersion(version) {
    const versions = editing.versions.includes(version)
      ? editing.versions.filter((v) => v !== version)
      : [...editing.versions, version];
    editing = { ...editing, versions };
  }

  async function save() {
    saving = true;
    saveError = '';
    try {
      await simulatorStore.save(devicePayload(editing));
      editing = null;
      await simulatorStore.refresh();
    } catch (e) {
      saveError = String(e);
    } finally {
      saving = false;
    }
  }

  async function run(device, action) {
    busy = { ...busy, [device.id]: true };
    try {
      await simulatorStore[action](device.id);
    } catch (e) {
      notificationStore.add($_('simulator.startFailed', { values: { name: device.name, error: String(e) } }), 'error');
    } finally {
      busy = { ...busy, [device.id]: false };
      simulatorStore.refresh().catch(() => {});
    }
  }

  async function remove(device) {
    if (confirmDelete !== device.id) {
      confirmDelete = device.id;
      return;
    }
    confirmDelete = null;
    try {
      await simulatorStore.remove(device.id);
      await simulatorStore.refresh();
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  async function addAsTarget(device) {
    try {
      const creds = await simulatorStore.credentials(device.id);
      const { settings, added, version } = addDeviceAsTarget($settingsStore, device, creds);
      await settingsStore.save(settings);
      notificationStore.add(
        $_(added ? 'simulator.targetAdded' : 'simulator.targetUpdated', { values: { address: device.address, version } }),
        'success',
      );
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }
</script>

<svelte:window on:keydown={onWindowKey} />

<div class="sim-backdrop" on:mousedown={onBackdrop(close)} role="presentation">
  <div class="sim-modal" role="dialog" aria-modal="true" aria-labelledby="sim-title" tabindex="-1">
    <header class="sim-header">
      <h2 id="sim-title"><Icon name="server-cog" size={20} /> {$_('simulator.title')}</h2>
      <button class="sim-close" on:click={close} aria-label={$_('common.close')}>&times;</button>
    </header>

    {#if editing}
      <div class="sim-body">
        <h3 class="editor-title">{editing.id ? $_('simulator.editTitle') : $_('simulator.newTitle')}</h3>
        <div class="grid">
          <div class="form-group">
            <label for="sim-name">{$_('simulator.field.name')}</label>
            <input id="sim-name" type="text" maxlength="64" spellcheck="false" bind:value={editing.name} />
            {#if problems.name}<span class="problem">{$_(`simulator.problem.${problems.name}`)}</span>{/if}
          </div>
          <div class="form-group">
            <label for="sim-model">{$_('simulator.field.model')}</label>
            <select id="sim-model" bind:value={editing.model} disabled={!!editing.id}>
              {#each $simulatorStore.models as m (m.id)}
                <option value={m.id}>{$_(`simulator.model.${m.id}.name`)}</option>
              {/each}
            </select>
          </div>
          <p class="model-description">{$_(`simulator.model.${editing.model}.description`)}</p>
          <div class="form-group">
            <label for="sim-address">{$_('simulator.field.address')}</label>
            <input id="sim-address" type="text" spellcheck="false" bind:value={editing.address} />
          </div>
          <div class="form-group">
            <label for="sim-port">{$_('simulator.field.port')}</label>
            <input id="sim-port" type="number" min="1" max="65535" bind:value={editing.port} />
            {#if problems.port}<span class="problem">{$_(`simulator.problem.${problems.port}`)}</span>{/if}
          </div>
        </div>
        <p class="hint"><Icon name="shield-check" size={14} /> {$_('simulator.loopbackHint')}</p>

        <fieldset class="versions">
          <legend>{$_('simulator.field.versions')}</legend>
          {#each SIM_VERSIONS as version (version)}
            <label class="check">
              <input type="checkbox" checked={editing.versions.includes(version)} on:change={() => toggleVersion(version)} />
              {version}
            </label>
          {/each}
          {#if problems.versions}<span class="problem">{$_(`simulator.problem.${problems.versions}`)}</span>{/if}
        </fieldset>

        {#if editing.versions.includes('v1') || editing.versions.includes('v2c')}
          <div class="form-group community">
            <label for="sim-community">{$_('simulator.field.community')}</label>
            <input id="sim-community" type="password" autocomplete="off" bind:value={editing.community} />
            {#if problems.community}<span class="problem">{$_(`simulator.problem.${problems.community}`)}</span>{/if}
          </div>
        {/if}

        {#if editing.versions.includes('v3')}
          <h4 class="section-title">{$_('simulator.field.v3')}</h4>
          <UsmFields v3={editing.v3} idPrefix="sim-v3" problems={usmProblems} showContext={false} />
        {/if}
      </div>
      <footer class="sim-footer">
        {#if saveError}<span class="save-error">{saveError}</span>{/if}
        <button class="btn secondary" on:click={() => (editing = null)}>{$_('common.cancel')}</button>
        <button class="btn" disabled={saving || Object.keys(problems).length > 0} on:click={save}>
          {$_('common.save')}
        </button>
      </footer>
    {:else}
      <div class="sim-body">
        <p class="intro">{$_('simulator.intro')}</p>
        {#if $simulatorStore.devices.length === 0}
          <div class="empty">
            <Icon name="server-cog" size={32} strokeWidth={1.5} />
            <p>{$_('simulator.empty')}</p>
          </div>
        {:else}
          <ul class="devices">
            {#each $simulatorStore.devices as d (d.id)}
              <li class="device" class:running={d.running}>
                <span class="state" title={d.running ? $_('simulator.running') : $_('simulator.stopped')}></span>
                <div class="identity">
                  <span class="name">{d.name}</span>
                  <span class="meta">
                    {$_(`simulator.model.${d.model}.name`)} · <code>{d.address}:{d.port}</code>
                  </span>
                  <span class="meta">
                    {#each d.versions as version (version)}<span class="chip">{version}</span>{/each}
                    <span class="activity">
                      {d.running ? $_('simulator.packets', { values: { count: d.packets } }) : $_('simulator.stopped')}
                    </span>
                  </span>
                </div>
                <div class="actions">
                  {#if d.running}
                    <button class="btn secondary btn-small" disabled={busy[d.id]} on:click={() => run(d, 'stop')}>
                      <Icon name="square" size={13} /> {$_('common.stop')}
                    </button>
                  {:else}
                    <button class="btn btn-small" disabled={busy[d.id]} on:click={() => run(d, 'start')}>
                      <Icon name="play" size={13} /> {$_('common.start')}
                    </button>
                  {/if}
                  <button class="btn tertiary btn-small" title={$_('simulator.addAsTargetTitle')} on:click={() => addAsTarget(d)}>
                    <Icon name="target" size={13} /> {$_('simulator.addAsTarget')}
                  </button>
                  <button class="icon-btn" title={$_('common.edit')} aria-label={$_('common.edit')} on:click={() => edit(d)}>
                    <Icon name="pencil" size={14} />
                  </button>
                  <button class="icon-btn danger" class:confirming={confirmDelete === d.id}
                    title={$_('simulator.deleteTitle')} aria-label={$_('simulator.deleteTitle')} on:click={() => remove(d)}>
                    {#if confirmDelete === d.id}{$_('simulator.confirmDelete')}{:else}<Icon name="trash-2" size={14} />{/if}
                  </button>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
      <footer class="sim-footer">
        <button class="btn" on:click={() => newDevice($simulatorStore.models, $simulatorStore.devices)}>
          <Icon name="plus" size={14} /> {$_('simulator.new')}
        </button>
      </footer>
    {/if}
  </div>
</div>

<style>
  .sim-backdrop {
    position: fixed;
    inset: 0;
    background-color: var(--backdrop-color);
    display: flex;
    justify-content: center;
    align-items: center;
    z-index: 100;
  }

  .sim-modal {
    background-color: var(--bg-light-color);
    border-radius: 8px;
    width: 92%;
    max-width: 760px;
    max-height: 85vh;
    box-shadow: 0 5px 15px var(--shadow-color-strong);
    display: flex;
    flex-direction: column;
  }

  .sim-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 15px 20px;
    border-bottom: 1px solid var(--border-color);
  }

  .sim-header h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-size: 1.2em;
  }

  .sim-close {
    background: none;
    border: none;
    color: var(--text-color);
    font-size: 1.5rem;
    cursor: pointer;
  }

  .sim-body {
    padding: 16px 20px;
    overflow-y: auto;
    flex: 1;
  }

  .sim-footer {
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: 10px;
    padding: 12px 20px;
    border-top: 1px solid var(--border-color);
  }

  .intro {
    margin: 0 0 14px;
    color: var(--text-muted);
    font-size: 0.9em;
    line-height: 1.45;
  }

  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 28px 0;
    color: var(--text-dimmed);
  }

  .empty p {
    margin: 0;
  }

  .devices {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .device {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
  }

  .state {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    flex-shrink: 0;
    background-color: var(--text-dimmed);
  }

  .device.running .state {
    background-color: var(--success-color);
    box-shadow: 0 0 0 3px var(--success-subtle-strong);
  }

  .identity {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
  }

  .name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    font-size: 0.82em;
    color: var(--text-muted);
  }

  .meta code {
    color: var(--text-color);
  }

  .chip {
    padding: 0 6px;
    border-radius: 4px;
    font-size: 0.92em;
    font-weight: 600;
    background-color: var(--hover-overlay-medium);
    color: var(--text-muted);
  }

  .activity {
    color: var(--text-dimmed);
  }

  .device.running .activity {
    color: var(--success-color);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .actions .btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    white-space: nowrap;
  }

  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 28px;
    height: 28px;
    padding: 0 6px;
    background: none;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-muted);
    cursor: pointer;
  }

  .icon-btn:hover {
    color: var(--text-color);
    background-color: var(--hover-overlay-medium);
  }

  .icon-btn.danger:hover,
  .icon-btn.confirming {
    color: var(--error-color);
    border-color: var(--error-color);
  }

  .icon-btn.confirming {
    font-size: 0.82em;
    font-weight: 600;
  }

  .editor-title {
    margin: 0 0 12px;
    font-size: 1em;
  }

  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px 20px;
    /* A field with a message under it must not stretch its neighbour. */
    align-items: start;
  }

  /* stretch, not the global .form-group's centring: labels line up on the left,
     as they do in UsmFields beside them. */
  .form-group {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    min-width: 0;
  }

  .form-group label {
    margin-bottom: 5px;
    font-size: 0.9em;
    color: var(--text-light);
  }

  .form-group input,
  .form-group select {
    width: 100%;
    padding: 8px 10px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
  }

  .form-group select:disabled {
    opacity: 0.7;
  }

  .model-description {
    grid-column: 1 / -1;
    margin: -4px 0 0;
    font-size: 0.82em;
    color: var(--text-muted);
    line-height: 1.4;
  }

  .hint {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin: 10px 0 0;
    font-size: 0.82em;
    color: var(--text-muted);
    line-height: 1.4;
  }

  .versions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 16px;
    margin: 14px 0 0;
    padding: 8px 12px 10px;
    border: 1px solid var(--border-color);
    border-radius: 6px;
  }

  .versions legend {
    padding: 0 4px;
    font-size: 0.9em;
    color: var(--text-light);
  }

  .check {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;
  }

  .community {
    margin-top: 14px;
    max-width: 50%;
  }

  .section-title {
    margin: 16px 0 0;
    font-size: 0.95em;
  }

  .problem {
    margin-top: 4px;
    font-size: 0.78em;
    color: var(--error-color);
  }

  .save-error {
    flex: 1;
    font-size: 0.85em;
    color: var(--error-color);
  }

  @media (max-width: 640px) {
    .grid {
      grid-template-columns: 1fr;
    }
    .device {
      flex-wrap: wrap;
    }
    .community {
      max-width: none;
    }
  }
</style>
