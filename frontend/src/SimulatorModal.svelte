<script>
  import { _ } from 'svelte-i18n';
  import { createEventDispatcher, onMount } from 'svelte';
  import { onBackdrop } from './utils/modal';
  import { simulatorStore } from './stores/simulatorStore';
  import { settingsStore } from './stores/settingsStore';
  import { notificationStore } from './stores/notifications';
  import { anonMode, maskString } from './utils/anonymize';
  import {
    SIM_VERSIONS, MAX_EVERY, MAX_DESTINATIONS, MAX_SCHEDULES, blankDevice, blankV3, blankDestination, blankSchedule,
    editableDevice, deviceProblems, devicePayload, addDeviceAsTarget, notificationsOf, trapActivity, deliveryReport,
    modelGroups,
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

  /** The editor's SNMPv3 problems, one set per user, in the shape UsmFields shows them. */
  function usmMessages(p, t) {
    return (p.users || []).map((u) => {
      const out = {};
      for (const key of ['user', 'authPass', 'privPass']) {
        if (u[key]) out[key] = { text: t(`simulator.problem.${u[key]}`), level: 'error' };
      }
      return out;
    });
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

  /** The name a new device was given, which follows its model until someone types another. */
  let defaultName = '';

  async function newDevice(models, devices) {
    const model = models[0]?.id || 'linux-server';
    const suggestion = await simulatorStore.suggestAddress().catch(() => null);
    editing = blankDevice(model, suggestion);
    defaultName = `${$_(`simulator.model.${model}.name`)} ${devices.length + 1}`;
    editing.name = defaultName;
    saveError = '';
  }

  // A schedule naming a notification the new model does not send falls back
  // to one at random, rather than being refused at save.
  function onModel() {
    if (editing.name === defaultName) {
      defaultName = `${$_(`simulator.model.${editing.model}.name`)} ${$simulatorStore.devices.length + 1}`;
      editing.name = defaultName;
    }
    const names = notificationsOf($simulatorStore.models, editing.model).map((n) => n.name);
    editing.traps.schedules = editing.traps.schedules.map((s) =>
      (s.notification && !names.includes(s.notification) ? { ...s, notification: '' } : s));
    editing = editing;
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

  function addUser() {
    editing = { ...editing, users: [...editing.users, blankV3(editing.users.length + 1)] };
  }

  function removeUser(index) {
    editing = { ...editing, users: editing.users.filter((u, i) => i !== index) };
  }

  /** The device's SNMPv3 user names, which a v3 notification is sent as. */
  function userNames(users) {
    return users.map((u) => (u.user || '').trim()).filter(Boolean);
  }

  function setTraps(traps) {
    editing = { ...editing, traps: { ...editing.traps, ...traps } };
  }

  function addDestination() {
    setTraps({ destinations: [...editing.traps.destinations, blankDestination($settingsStore.trapPort)] });
  }

  function removeDestination(index) {
    setTraps({ destinations: editing.traps.destinations.filter((d, i) => i !== index) });
  }

  // v3 is sent as one of the device's users — the first, until another is
  // chosen — and v1 has no INFORM.
  function onDestinationVersion(dest) {
    if (dest.version === 'v3' && !dest.user) dest.user = userNames(editing.users)[0] || '';
    if (dest.version === 'v1') dest.inform = false;
    editing = editing;
  }

  async function useListenerEngine(dest) {
    try {
      dest.engineId = await simulatorStore.listenerEngineId();
      editing = editing;
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  function addSchedule() {
    setTraps({ schedules: [...editing.traps.schedules, blankSchedule()] });
  }

  function removeSchedule(index) {
    setTraps({ schedules: editing.traps.schedules.filter((s, i) => i !== index) });
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
      const { settings, added, version, target } = addDeviceAsTarget($settingsStore, device, creds);
      await settingsStore.save(settings);
      notificationStore.add(
        $_(added ? 'simulator.targetAdded' : 'simulator.targetUpdated', { values: { address: target, version } }),
        'success',
      );
    } catch (e) {
      notificationStore.add(String(e), 'error');
    }
  }

  // The menu is a select that goes back to its prompt once used: a notification
  // is an action, not a setting.
  async function sendTrap(device, event) {
    const name = event.currentTarget.value;
    event.currentTarget.value = '';
    if (!name) return;
    busy = { ...busy, [device.id]: true };
    try {
      const deliveries = await simulatorStore.sendTrap(device.id, name);
      for (const line of deliveryReport(name, deliveries)) {
        const values = $anonMode && line.values.destination
          ? { ...line.values, destination: maskString(line.values.destination) }
          : line.values;
        notificationStore.add($_(line.key, { values }), line.level);
      }
    } catch (e) {
      notificationStore.add($_('simulator.traps.sendFailed', { values: { name, error: String(e) } }), 'error');
    } finally {
      busy = { ...busy, [device.id]: false };
      simulatorStore.refresh().catch(() => {});
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
            <select id="sim-model" bind:value={editing.model} disabled={!!editing.id} on:change={onModel}>
              {#each modelGroups($simulatorStore.models) as group (group.category)}
                <optgroup label={$_(`simulator.category.${group.category}`)}>
                  {#each group.models as m (m.id)}
                    <option value={m.id}>{$_(`simulator.model.${m.id}.name`)}</option>
                  {/each}
                </optgroup>
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
          {#each editing.users as u, i (i)}
            <div class="user-block">
              <div class="user-head">
                <span class="user-title">{$_('simulator.userN', { values: { n: i + 1 } })}</span>
                {#if editing.users.length > 1}
                  <button class="icon-btn danger" title={$_('simulator.removeUser')} aria-label={$_('simulator.removeUser')}
                    on:click={() => removeUser(i)}>
                    <Icon name="trash-2" size={14} />
                  </button>
                {/if}
              </div>
              <!-- bind:, as the settings do: UsmFields edits the object in place, and
                   without it the checks above never see a passphrase being typed. -->
              <UsmFields bind:v3={u} idPrefix="sim-v3-{i}" problems={usmProblems[i] || {}} showContext={false} />
            </div>
          {/each}
          {#if problems.noUser}<span class="problem">{$_(`simulator.problem.${problems.noUser}`)}</span>{/if}
          <div class="users-foot">
            <button class="btn tertiary btn-small" on:click={addUser}>
              <Icon name="plus" size={13} /> {$_('simulator.addUser')}
            </button>
            {#if editing.users.length > 1}<span class="users-note">{$_('simulator.firstUserTarget')}</span>{/if}
          </div>
        {/if}

        <h4 class="section-title">{$_('simulator.traps.title')}</h4>
        <p class="hint"><Icon name="radio" size={14} /> {$_('simulator.traps.hint')}</p>
        {#each editing.traps.destinations as dest, i (i)}
          <div class="user-block">
            <div class="user-head">
              <span class="user-title">{$_('simulator.traps.destinationN', { values: { n: i + 1 } })}</span>
              <button class="icon-btn danger" title={$_('simulator.traps.removeDestination')}
                aria-label={$_('simulator.traps.removeDestination')} on:click={() => removeDestination(i)}>
                <Icon name="trash-2" size={14} />
              </button>
            </div>
            <div class="dest-grid">
              <div class="form-group">
                <label for="sim-dest-{i}-host">{$_('simulator.traps.host')}</label>
                <input id="sim-dest-{i}-host" type="text" spellcheck="false" bind:value={dest.host} />
                {#if problems.destinations?.[i]?.host}<span class="problem">{$_(`simulator.problem.${problems.destinations[i].host}`)}</span>{/if}
              </div>
              <div class="form-group">
                <label for="sim-dest-{i}-port">{$_('simulator.traps.port')}</label>
                <input id="sim-dest-{i}-port" type="number" min="1" max="65535" bind:value={dest.port} />
                {#if problems.destinations?.[i]?.port}<span class="problem">{$_(`simulator.problem.${problems.destinations[i].port}`)}</span>{/if}
              </div>
              <div class="form-group">
                <label for="sim-dest-{i}-version">{$_('simulator.traps.version')}</label>
                <select id="sim-dest-{i}-version" bind:value={dest.version} on:change={() => onDestinationVersion(dest)}>
                  {#each SIM_VERSIONS as version (version)}<option value={version}>{version}</option>{/each}
                </select>
              </div>
              {#if dest.version === 'v3'}
                <div class="form-group">
                  <label for="sim-dest-{i}-user">{$_('simulator.traps.user')}</label>
                  <select id="sim-dest-{i}-user" bind:value={dest.user}>
                    {#each userNames(editing.users) as userName (userName)}<option value={userName}>{userName}</option>{/each}
                  </select>
                  {#if problems.destinations?.[i]?.user}<span class="problem">{$_(`simulator.problem.${problems.destinations[i].user}`)}</span>{/if}
                </div>
              {:else}
                <div class="form-group">
                  <label for="sim-dest-{i}-community">{$_('simulator.traps.community')}</label>
                  <input id="sim-dest-{i}-community" type="password" autocomplete="off" bind:value={dest.community} />
                  {#if problems.destinations?.[i]?.community}<span class="problem">{$_(`simulator.problem.${problems.destinations[i].community}`)}</span>{/if}
                </div>
              {/if}
            </div>
            <label class="check inform">
              <input type="checkbox" bind:checked={dest.inform} disabled={dest.version === 'v1'} />
              {$_('simulator.traps.inform')}
            </label>
            {#if dest.version === 'v3' && dest.inform}
              <div class="form-group engine">
                <label for="sim-dest-{i}-engine">{$_('simulator.traps.engineId')}</label>
                <div class="engine-row">
                  <input id="sim-dest-{i}-engine" type="text" spellcheck="false"
                    placeholder={$_('simulator.traps.engineIdPlaceholder')} bind:value={dest.engineId} />
                  <button class="btn tertiary btn-small" on:click={() => useListenerEngine(dest)}>
                    {$_('simulator.traps.useListener')}
                  </button>
                </div>
                <span class="field-hint">{$_('simulator.traps.engineIdHint')}</span>
                {#if problems.destinations?.[i]?.engineId}<span class="problem">{$_(`simulator.problem.${problems.destinations[i].engineId}`)}</span>{/if}
              </div>
            {/if}
          </div>
        {/each}
        <div class="users-foot">
          <button class="btn tertiary btn-small" disabled={editing.traps.destinations.length >= MAX_DESTINATIONS}
            on:click={addDestination}>
            <Icon name="plus" size={13} /> {$_('simulator.traps.addDestination')}
          </button>
          <span class="users-note">{$_('simulator.traps.newDestinationNote')}</span>
        </div>

        {#if editing.traps.destinations.length}
          <div class="triggers">
            <label class="check"><input type="checkbox" bind:checked={editing.traps.onStart} /> {$_('simulator.traps.onStart')}</label>
            <label class="check"><input type="checkbox" bind:checked={editing.traps.onAuthFailure} /> {$_('simulator.traps.onAuthFailure')}</label>
          </div>
          {#each editing.traps.schedules as s, i (i)}
            <div class="schedule">
              <select aria-label={$_('simulator.traps.notification')} bind:value={s.notification}>
                <option value="">{$_('simulator.traps.any')}</option>
                {#each notificationsOf($simulatorStore.models, editing.model) as n (n.name)}
                  <option value={n.name}>{n.name}</option>
                {/each}
              </select>
              <span>{$_('simulator.traps.every')}</span>
              <input type="number" min="1" max={MAX_EVERY} aria-label={$_('simulator.traps.seconds')} bind:value={s.every} />
              <span>{$_('simulator.traps.seconds')}</span>
              <label class="check"><input type="checkbox" bind:checked={s.irregular} /> {$_('simulator.traps.irregular')}</label>
              <button class="icon-btn danger" title={$_('simulator.traps.removeSchedule')}
                aria-label={$_('simulator.traps.removeSchedule')} on:click={() => removeSchedule(i)}>
                <Icon name="trash-2" size={14} />
              </button>
              {#if problems.schedules?.[i]?.every}<span class="problem">{$_('simulator.problem.every')}</span>{/if}
            </div>
          {/each}
          <div class="users-foot">
            <button class="btn tertiary btn-small" disabled={editing.traps.schedules.length >= MAX_SCHEDULES} on:click={addSchedule}>
              <Icon name="plus" size={13} /> {$_('simulator.traps.addSchedule')}
            </button>
          </div>
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
              {@const activity = trapActivity(d.traps)}
              <li class="device" class:running={d.running}>
                <span class="state" title={d.running ? $_('simulator.running') : $_('simulator.stopped')}></span>
                <div class="identity">
                  <span class="name">{d.name}</span>
                  <span class="meta">
                    {$_(`simulator.model.${d.model}.name`)} · <code>{d.address}:{d.port}</code>
                  </span>
                  <span class="meta">
                    {#each d.versions as version (version)}<span class="chip">{version}</span>{/each}
                    {#if d.users?.length}
                      <span class="users">
                        <Icon name="key-round" size={12} />
                        {$anonMode ? maskString(d.users[0].name) : d.users.map((u) => u.name).join(', ')}
                      </span>
                    {/if}
                    <span class="activity">
                      {d.running ? $_('simulator.packets', { values: { count: d.packets } }) : $_('simulator.stopped')}
                    </span>
                    {#if d.running && d.traps?.destinations?.length}
                      <span class="traps-activity" title={activity.lastError}>
                        <Icon name="radio" size={12} />
                        {$_('simulator.traps.activity', { values: { sent: activity.sent } })}
                        {#if activity.failed}
                          · <span class="failed">{$_('simulator.traps.activityFailed', { values: { failed: activity.failed } })}</span>
                        {/if}
                      </span>
                    {/if}
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
                  {#if d.running && d.traps?.destinations?.length}
                    <select class="send-trap" title={$_('simulator.traps.sendTitle')} aria-label={$_('simulator.traps.sendTitle')}
                      disabled={busy[d.id]} on:change={(e) => sendTrap(d, e)}>
                      <option value="">{$_('simulator.traps.send')}</option>
                      {#each notificationsOf($simulatorStore.models, d.model) as n (n.name)}
                        <option value={n.name}>{n.name}</option>
                      {/each}
                    </select>
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

  .user-block {
    margin-top: 10px;
    padding: 10px 12px 12px;
    border: 1px solid var(--border-color);
    border-radius: 6px;
  }

  .user-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    min-height: 28px;
  }

  .user-title {
    font-size: 0.88em;
    font-weight: 600;
    color: var(--text-muted);
  }

  .users-foot {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    margin-top: 10px;
  }

  .users-foot .btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }

  .users-note {
    font-size: 0.82em;
    color: var(--text-muted);
  }

  .users {
    display: inline-flex;
    align-items: center;
    gap: 4px;
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

  .traps-activity {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .traps-activity .failed {
    color: var(--error-color);
  }

  .send-trap {
    height: 28px;
    padding: 0 6px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    font-size: 0.85em;
  }

  .dest-grid {
    display: grid;
    grid-template-columns: 2fr 1fr 1fr 2fr;
    gap: 10px 14px;
    align-items: start;
    margin-top: 6px;
  }

  .inform {
    margin-top: 10px;
    font-size: 0.9em;
  }

  .engine {
    margin-top: 10px;
  }

  .engine-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .engine-row input {
    flex: 1;
    min-width: 0;
  }

  .engine-row .btn {
    white-space: nowrap;
  }

  .field-hint {
    margin-top: 4px;
    font-size: 0.78em;
    color: var(--text-muted);
  }

  .triggers {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 14px;
    font-size: 0.9em;
  }

  .schedule {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 10px;
    font-size: 0.9em;
  }

  .schedule select,
  .schedule input[type='number'] {
    padding: 6px 8px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
  }

  .schedule input[type='number'] {
    width: 90px;
  }

  .schedule .problem {
    flex-basis: 100%;
    margin-top: 0;
  }

  @media (max-width: 640px) {
    .grid {
      grid-template-columns: 1fr;
    }
    .dest-grid {
      grid-template-columns: 1fr 1fr;
    }
    .device {
      flex-wrap: wrap;
    }
    .community {
      max-width: none;
    }
  }
</style>
