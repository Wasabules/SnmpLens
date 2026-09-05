<script>
  import { createEventDispatcher } from 'svelte';
  import { _ } from 'svelte-i18n';
  import Icon from '../Icon.svelte';
  import ResultsDisplay from '../operations/ResultsDisplay.svelte';
  import { findNodeByOid, findMibNameByOid, formatValueWithEnum } from '../utils/mibTree';
  import { formatTimestamp, formatDuration } from '../utils/formatting';
  import { anonMode, anonymizeIp } from '../utils/anonymize';
  import { copyToClipboard } from '../utils/clipboard';
  import { targetLabels } from '../stores/targetLabels';
  import { displayTarget, targetTitle } from '../utils/targets';

  export let entry;
  export let expanded = false;
  export let selectMode = false;
  export let diffMode = false;
  export let selected = false;          // selection state (select mode)
  export let diffLabel = null;          // 'A' | 'B' | null (diff mode)
  export let diffEligible = true;       // whether this entry can be diffed
  export let flash = false;             // one-shot highlight animation
  export let oidInfoCache = {};
  export let mibTree = [];

  const dispatch = createEventDispatcher();

  function onActivate() {
    if (selectMode) dispatch('select');
    else if (diffMode) dispatch('diff');
    else dispatch('toggle');
  }

  function getDisplayName(oid) {
    return findMibNameByOid(oid, mibTree) || oid;
  }

  function getOperationIcon(operation) {
    switch (operation) {
      case 'GET': return 'download';
      case 'SET': return 'upload';
      case 'GETNEXT': return 'arrow-right-to-line';
      case 'GETBULK': return 'layers';
      case 'WALK': return 'footprints';
      default: return 'file';
    }
  }

  function countEntryResults(e) {
    let n = 0;
    for (const res of e.results || []) {
      if (res.error) continue;
      if (Array.isArray(res.result?.value)) n += res.result.value.length;
      else if (res.result) n += 1;
    }
    return n;
  }

  // Scalar value for single-value ops, else a "N result(s)" count, else null.
  function getDisplayValue(e, tree) {
    if (!e.results || e.results.length === 0) return null;
    if (e.operation === 'GET' || e.operation === 'SET' || e.operation === 'GETNEXT') {
      const val = e.results[0]?.result?.value;
      if (val !== undefined && !Array.isArray(val)) {
        return formatValueWithEnum(val, findNodeByOid(e.oid, tree));
      }
    }
    const count = e.totalResults ?? countEntryResults(e);
    if (count > 0) return $_('history.resultCount', { values: { count } });
    return null;
  }

  // Central value badge: the value/count in blue, or a red "Error" / muted
  // "No result" so a failed or empty request is never a blank blue chip.
  function getValueBadge(e, tree) {
    if (!e.success) {
      return { text: $_('history.badgeError'), kind: 'error', title: e.error || $_('history.badgeError') };
    }
    const dv = getDisplayValue(e, tree);
    if (dv) return { text: dv, kind: 'value', title: dv };
    return { text: $_('history.badgeNoResult'), kind: 'empty', title: '' };
  }

  // Recompute when the entry or the resolved MIB tree changes.
  $: badge = getValueBadge(entry, mibTree);
</script>

<div
  class="history-entry op-{(entry.operation || '').toLowerCase()}"
  data-entry-id={entry.id}
  class:expanded
  class:diff-selected={diffMode && diffLabel !== null}
  class:select-selected={selectMode && selected}
  class:highlighted={flash}
>
  <div
    class="entry-header"
    class:diff-mode={diffMode}
    class:select-mode={selectMode}
    on:click={onActivate}
    on:keydown={(e) => e.key === 'Enter' && onActivate()}
    role="button"
    tabindex="0"
  >
    {#if selectMode}
      <span class="select-checkbox" class:selected>
        {#if selected}<Icon name="check" size={14} />{/if}
      </span>
    {:else if diffMode}
      <span class="diff-checkbox" class:disabled={!diffEligible} class:selected={diffLabel !== null}>
        {#if diffLabel}{diffLabel}{/if}
      </span>
    {/if}
    <span class="chevron">{expanded ? '▼' : '▶'}</span>
    <span class="operation-icon"><Icon name={getOperationIcon(entry.operation)} size={14} /></span>
    <span class="op-badge">{entry.operation}</span>
    <span class="timestamp">{formatTimestamp(entry.timestamp)}</span>
    <span class="mib-name" title={entry.oid}>{getDisplayName(entry.oid)}</span>
    <span class="value-badge {badge.kind}" title={badge.title}>{badge.text}</span>
    <span class="targets-count">{entry.targets?.length || 0} target(s)</span>
    <span class="duration">{formatDuration(entry.duration)}</span>
    <span class="status" class:ok={entry.success} class:ko={!entry.success}>
      {#if entry.success}<Icon name="check" size={15} />{:else}<Icon name="x" size={15} />{/if}
    </span>
    <button
      class="btn-icon delete-btn"
      on:click|stopPropagation={() => dispatch('delete')}
      title={$_('history.deleteEntry')}
    >
      <Icon name="trash-2" size={15} />
    </button>
  </div>

  {#if expanded}
    <div class="entry-details">
      <div class="detail-row">
        <strong>{$_('nodeDetails.name')}</strong> <code class="mib-name-detail">{getDisplayName(entry.oid)}</code>
      </div>
      <div class="detail-row">
        <strong>{$_('nodeDetails.oid')}</strong> <code>{entry.oid}</code>
        <button class="btn-copy-small" on:click={() => copyToClipboard(entry.oid, $_('common.oid'))} title={$_('common.oid')}><Icon name="copy" size={13} /></button>
      </div>
      <div class="detail-row">
        <strong>{$_('history.targets')}:</strong>
        {#each entry.targets || [] as t, i}<code title={targetTitle(t, $anonMode)}>{displayTarget(t, $targetLabels, $anonMode)}</code>{#if i < entry.targets.length - 1}<span>, </span>{/if}{/each}
      </div>
      <div class="detail-row">
        <strong>{$_('common.version')}:</strong> <span>{entry.version}</span>
      </div>

      {#if entry.operation === 'SET'}
        <div class="detail-row">
          <strong>{$_('common.value')}:</strong> <code>{entry.value}</code>
          <button class="btn-copy-small" on:click={() => copyToClipboard(String(entry.value), $_('common.value'))} title={$_('common.value')}><Icon name="copy" size={13} /></button>
        </div>
        <div class="detail-row">
          <strong>{$_('common.type')}:</strong> <span>{entry.valueType}</span>
        </div>
      {/if}

      {#if entry.operation === 'WALK' && entry.totalResults}
        <div class="detail-row">
          <strong>{$_('common.results')}:</strong> <span>{entry.totalResults}</span>
        </div>
      {/if}

      {#if entry.error}
        <div class="detail-row error-row">
          <strong>{$_('history.error')}:</strong> <span>{entry.error}</span>
        </div>
      {/if}

      {#if entry.results && entry.results.length > 0}
        <ResultsDisplay
          bulkResults={entry.results}
          activeOperation={entry.operation}
          oidInfoCache={oidInfoCache}
          mibTree={mibTree}
        />
      {/if}
    </div>
  {/if}
</div>

<style>
  .history-entry {
    border-bottom: 1px solid var(--border-color);
    border-left: 3px solid var(--op-color, transparent);
    background-color: var(--bg-lighter-color);
  }

  .history-entry:hover {
    background-color: var(--hover-overlay);
  }

  .history-entry.highlighted {
    animation: history-hl 2.2s ease-out;
  }

  @keyframes history-hl {
    0%, 15% { background-color: var(--accent-subtle-strong); }
    100% { background-color: var(--bg-lighter-color); }
  }

  .history-entry.diff-selected,
  .history-entry.select-selected {
    border-left: 3px solid var(--accent-color);
    background-color: var(--accent-subtle-medium) !important;
  }

  /* Value badge grows in the middle (name | VALUE | targets …). */
  .entry-header {
    display: grid;
    grid-template-columns: 18px 22px auto auto minmax(110px, 1fr) minmax(150px, 1.6fr) auto auto 24px 28px;
    align-items: center;
    gap: 10px;
    padding: 12px;
    cursor: pointer;
    user-select: none;
  }

  /* Diff and select modes prepend a checkbox column. */
  .entry-header.diff-mode,
  .entry-header.select-mode {
    grid-template-columns: 28px 18px 22px auto auto minmax(110px, 1fr) minmax(150px, 1.6fr) auto auto 24px 28px;
  }

  .chevron {
    color: var(--text-muted);
    font-size: 0.8em;
  }

  .operation-icon {
    font-size: 1.2em;
  }

  .timestamp {
    font-size: 0.85em;
    color: var(--text-dimmed);
  }

  .mib-name {
    font-family: 'Courier New', monospace;
    font-size: 0.85em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--oid-color);
    font-weight: 500;
  }

  .value-badge {
    justify-self: center;
    max-width: 100%;
    font-size: 0.9em;
    font-weight: 600;
    padding: 3px 10px;
    border-radius: 4px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .value-badge.value {
    color: var(--accent-color);
    background-color: var(--accent-subtle);
    border: 1px solid var(--accent-border);
  }

  .value-badge.error {
    color: var(--error-color);
    background-color: color-mix(in srgb, var(--error-color) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--error-color) 34%, transparent);
  }

  .value-badge.empty {
    color: var(--text-muted);
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
  }

  .targets-count, .duration {
    font-size: 0.85em;
    color: var(--text-dimmed);
    white-space: nowrap;
  }

  .status {
    font-weight: bold;
    font-size: 1.2em;
  }

  .status.ok { color: var(--success-color); }
  .status.ko { color: var(--error-color); }

  .btn-icon {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 1em;
    padding: 4px;
    opacity: 0.7;
    color: var(--text-color);
    transition: opacity 0.2s;
  }

  .btn-icon:hover {
    opacity: 1;
  }

  .entry-details {
    padding: 15px;
    background-color: var(--bg-color);
    border-top: 1px solid var(--border-color);
    font-size: 0.9em;
  }

  .detail-row {
    margin-bottom: 8px;
    display: flex;
    gap: 10px;
  }

  .detail-row strong {
    min-width: 120px;
    color: var(--text-dimmed);
  }

  .detail-row code {
    background-color: var(--bg-lighter-color);
    padding: 2px 6px;
    border-radius: 3px;
    font-family: 'Courier New', monospace;
  }

  .detail-row code.mib-name-detail {
    color: var(--oid-color);
    font-weight: 600;
    background-color: var(--oid-subtle);
  }

  .error-row {
    color: var(--error-color);
  }

  .diff-checkbox,
  .select-checkbox {
    box-sizing: border-box;
    width: 22px;
    height: 22px;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 2px solid var(--text-muted);
    border-radius: 5px;
    font-weight: 700;
    font-size: 0.82em;
    color: var(--text-on-accent);
  }

  .diff-checkbox.selected,
  .select-checkbox.selected {
    background-color: var(--accent-color);
    border-color: var(--accent-color);
  }

  .diff-checkbox.disabled {
    opacity: 0.3;
    border-style: dashed;
    cursor: not-allowed;
  }
</style>
