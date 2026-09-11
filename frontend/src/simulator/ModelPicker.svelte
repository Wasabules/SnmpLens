<script>
  import { _ } from 'svelte-i18n';
  import { createEventDispatcher, tick } from 'svelte';
  import Icon from '../Icon.svelte';
  import ModelIcon from './ModelIcon.svelte';
  import {
    filterModels, categoriesOf, categoryIcon, modelName, modelDescription, devicesOfModel,
  } from '../utils/simulator.js';

  /**
   * The model a new device is made from: a search over the catalogue and the
   * custom models, the categories to narrow it, and the chosen model described,
   * with its icon when a custom model came with one. Importing and deleting a
   * custom model are asked for here and done by the modal.
   */
  export let models = [];
  /** The custom models' icons, by model ID. */
  export let icons = {};
  /** The devices, which a model is not deleted from under. */
  export let devices = [];
  export let value = '';
  export let busy = false;

  const dispatch = createEventDispatcher();
  let query = '';
  let category = '';
  let confirmDelete = '';
  let listEl;

  $: shown = filterModels(models, query, category, $_);
  $: categories = categoriesOf(models);
  $: selected = models.find((m) => m.id === value);
  $: inUse = selected ? devicesOfModel(devices, selected.id).length : 0;

  function choose(id) {
    if (id === value) return;
    value = id;
    confirmDelete = '';
    dispatch('change', id);
  }

  // Up and down move the choice through what the search shows, without leaving
  // the search field, and Enter takes the first match.
  async function onSearchKey(event, list, current) {
    if (!list.length) return;
    const at = list.findIndex((m) => m.id === current);
    let next;
    if (event.key === 'ArrowDown') next = at < 0 ? 0 : Math.min(at + 1, list.length - 1);
    else if (event.key === 'ArrowUp') next = Math.max(at - 1, 0);
    else if (event.key === 'Enter') next = at < 0 ? 0 : at;
    else return;
    event.preventDefault();
    choose(list[next].id);
    await tick();
    listEl?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' });
  }

  function remove(model) {
    if (confirmDelete !== model.id) {
      confirmDelete = model.id;
      return;
    }
    confirmDelete = '';
    dispatch('delete', model.id);
  }
</script>

<div class="picker">
  <div class="bar">
    <label class="search">
      <Icon name="search" size={14} />
      <input id="sim-model-search" type="search" autocomplete="off" spellcheck="false"
        placeholder={$_('simulator.picker.search')} aria-label={$_('simulator.picker.search')}
        aria-controls="sim-model-list" bind:value={query} on:keydown={(e) => onSearchKey(e, shown, value)} />
    </label>
    <button type="button" class="btn tertiary btn-small" disabled={busy} title={$_('simulator.picker.importTitle')}
      on:click={() => dispatch('import')}>
      <Icon name="upload" size={13} /> {$_('simulator.picker.import')}
    </button>
  </div>

  <div class="chips" role="group" aria-label={$_('simulator.picker.categories')}>
    <button type="button" class="chip" class:active={!category} aria-pressed={!category} on:click={() => (category = '')}>
      {$_('simulator.picker.all')}
    </button>
    {#each categories as c (c)}
      <button type="button" class="chip" class:active={category === c} aria-pressed={category === c}
        on:click={() => (category = category === c ? '' : c)}>
        <Icon name={categoryIcon(c)} size={12} /> {$_(`simulator.category.${c}`)}
      </button>
    {/each}
  </div>

  <div id="sim-model-list" class="list" role="listbox" aria-label={$_('simulator.field.model')} bind:this={listEl}>
    {#each shown as m (m.id)}
      <button type="button" role="option" class="option" class:selected={m.id === value} aria-selected={m.id === value}
        on:click={() => choose(m.id)}>
        <ModelIcon category={m.category} icon={icons[m.id] || ''} size={34} />
        <span class="text">
          <span class="name">{modelName(m, $_)}</span>
          <span class="sub">
            <span class="sub-text">{m.vendor ? `${m.vendor} · ` : ''}{$_(`simulator.category.${m.category}`)}</span>
            {#if m.custom}<span class="tag">{$_('simulator.picker.custom')}</span>{/if}
          </span>
        </span>
      </button>
    {/each}
  </div>
  {#if shown.length === 0}
    <p class="none">{$_('simulator.picker.noMatch', { values: { query } })}</p>
  {/if}

  {#if selected}
    <div class="detail">
      <ModelIcon category={selected.category} icon={icons[selected.id] || ''} size={56} />
      <div class="detail-text">
        <strong>{modelName(selected, $_)}</strong>
        <p>{modelDescription(selected, $_)}</p>
        <p class="sends"><Icon name="radio" size={12} /> {selected.notifications.map((n) => n.name).join(' · ')}</p>
      </div>
      {#if selected.custom}
        <button type="button" class="icon-btn danger" class:confirming={confirmDelete === selected.id}
          disabled={inUse > 0 || busy}
          title={inUse > 0 ? $_('simulator.picker.inUse', { values: { count: inUse } }) : $_('simulator.picker.deleteModel')}
          aria-label={$_('simulator.picker.deleteModel')} on:click={() => remove(selected)}>
          {#if confirmDelete === selected.id}{$_('simulator.confirmDelete')}{:else}<Icon name="trash-2" size={14} />{/if}
        </button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .picker {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 8px 0 14px;
  }

  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .search {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    padding: 0 10px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    color: var(--text-muted);
  }

  .search:focus-within {
    border-color: var(--accent-color);
  }

  .search input {
    flex: 1;
    min-width: 0;
    padding: 8px 0;
    background: none;
    border: none;
    outline: none;
    color: var(--text-color);
  }

  .bar .btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    white-space: nowrap;
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 10px;
    border: 1px solid var(--border-color);
    border-radius: 999px;
    background: none;
    color: var(--text-muted);
    font-size: 0.8em;
    cursor: pointer;
  }

  .chip:hover {
    color: var(--text-color);
    background-color: var(--hover-overlay-medium);
  }

  .chip.active {
    color: var(--accent-color);
    border-color: var(--accent-border);
    background-color: var(--accent-subtle);
  }

  .list {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
    gap: 6px;
    max-height: 238px;
    overflow-y: auto;
    padding: 2px;
  }

  .option {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
    padding: 7px 9px;
    text-align: left;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    color: var(--text-color);
    cursor: pointer;
  }

  .option:hover {
    border-color: var(--border-hover);
  }

  .option.selected {
    border-color: var(--accent-color);
    background-color: var(--accent-subtle);
    box-shadow: inset 0 0 0 1px var(--accent-color);
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .name {
    overflow: hidden;
    font-size: 0.88em;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sub {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    font-size: 0.76em;
    color: var(--text-muted);
  }

  .sub-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tag {
    flex-shrink: 0;
    padding: 0 5px;
    border-radius: 4px;
    font-weight: 600;
    background-color: var(--accent-subtle-medium);
    color: var(--accent-color);
  }

  .none {
    margin: 2px;
    font-size: 0.85em;
    color: var(--text-muted);
  }

  .detail {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 10px 12px;
    background-color: var(--bg-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
  }

  .detail-text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .detail-text p {
    margin: 0;
    font-size: 0.82em;
    line-height: 1.4;
    color: var(--text-muted);
  }

  .detail-text .sends {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 5px;
    color: var(--text-dimmed);
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

  .icon-btn:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }

  .icon-btn.danger:not(:disabled):hover,
  .icon-btn.confirming {
    color: var(--error-color);
    border-color: var(--error-color);
  }

  .icon-btn.confirming {
    font-size: 0.82em;
    font-weight: 600;
  }
</style>
