<script>
  import { createEventDispatcher } from 'svelte';
  import { _ } from 'svelte-i18n';
  import Icon from '../Icon.svelte';
  import { seriesColor, MAX_SERIES } from '../utils/chartPalette';
  import { anonMode, anonymizeIp } from '../utils/anonymize';
  import { targetLabels } from '../stores/targetLabels';
  import { mibStore } from '../stores/mibStore';
  import { oidName, oidTooltip } from '../utils/oidDisplay';
  import { STAT_KEYS } from '../utils/seriesStats';

  export let session;
  export let hidden = [];
  export let layout = 'separate'; // 'separate' | 'stacked'
  export let theme = 'dark';
  /** Statistics shown on each tile, in order (the first is the hero figure). */
  export let stats = [];
  /** 'window' = summarise what the chart shows, 'all' = the whole buffer. */
  export let scope = 'window';

  const dispatch = createEventDispatcher();
  const key = (target, oid) => target + '|' + oid;

  $: hiddenSet = new Set(hidden || []);
  // Deduplicated: these feed a keyed {#each}, so a repeated OID or target
  // would crash the render with duplicate keys.
  $: oidList = [
    ...new Set((session?.oids && session.oids.length ? session.oids : [session?.oid]).filter(Boolean)),
  ];

  // Mirror exactly how the chart orders targets (first appearance in the data),
  // so a swatch here is the colour actually drawn there.
  function targetsFor(oid) {
    const scoped = (session?.results || []).filter((r) => (r.oid || session?.oid) === oid);
    const seen = [...new Set(scoped.map((r) => r.target))];
    return seen.length ? seen : [...new Set(session?.targets || [])];
  }

  // Colour assignment differs per layout: per-OID in separate mode, one running
  // counter across every (OID, target) pair when stacked.
  function buildChannels(s, l, dark) {
    const out = [];
    let running = 0;
    for (const oid of oidList) {
      const targets = targetsFor(oid);
      targets.forEach((target, idx) => {
        const color = l === 'stacked' ? seriesColor(running, dark) : seriesColor(idx, dark);
        const capped = l === 'stacked' ? running >= MAX_SERIES : idx >= MAX_SERIES;
        running++;
        out.push({ oid, target, color, capped });
      });
    }
    return out;
  }

  $: channels = buildChannels(session, layout, theme !== 'light');
  $: visibleCount = channels.filter((c) => !hiddenSet.has(key(c.target, c.oid))).length;

  function toggle(c) {
    const k = key(c.target, c.oid);
    const next = new Set(hiddenSet);
    if (next.has(k)) next.delete(k);
    else next.add(k);
    dispatch('change', [...next]);
  }

  function showAll() {
    dispatch('change', []);
  }

  // Order matters: the first selected statistic becomes the tile's hero figure,
  // so toggling appends rather than re-sorting.
  function toggleStat(k) {
    const next = stats.includes(k) ? stats.filter((x) => x !== k) : [...stats, k];
    dispatch('stats', next);
  }

  function showOnly(c) {
    const k = key(c.target, c.oid);
    dispatch('change', channels.map((x) => key(x.target, x.oid)).filter((x) => x !== k));
  }
</script>

<div
  class="modal-overlay"
  on:click={() => dispatch('close')}
  on:keydown={(e) => e.key === 'Escape' && dispatch('close')}
  role="button"
  tabindex="-1"
>
  <div class="channels-modal" on:click|stopPropagation on:keydown|stopPropagation role="dialog" tabindex="-1">
    <h3><Icon name="activity" size={16} /> {$_('monitor.channelsTitle')}</h3>
    <p class="hint">{$_('monitor.channelsHint', { values: { visible: visibleCount, total: channels.length } })}</p>

    <div class="channel-list">
      {#each channels as c (c.oid + '|' + c.target)}
        {@const k = key(c.target, c.oid)}
        {@const on = !hiddenSet.has(k)}
        <div class="channel" class:off={!on} class:capped={c.capped}>
          <button class="chk" class:on on:click={() => toggle(c)} aria-pressed={on} aria-label={c.target}>
            {#if on}<Icon name="check" size={13} />{/if}
          </button>
          <span class="swatch" style="background-color: {on ? c.color : 'transparent'}; border-color: {c.color}"></span>
          <span class="ch-target" title={$anonMode ? anonymizeIp(c.target) : c.target}>
            {$anonMode ? anonymizeIp(c.target) : $targetLabels[c.target] || c.target}
          </span>
          {#if oidList.length > 1}
            <span class="ch-oid" title={oidTooltip(c.oid, $mibStore.tree)}>{oidName(c.oid, $mibStore.tree)}</span>
          {/if}
          {#if c.capped}
            <span class="ch-capped">{$_('monitor.channelCapped')}</span>
          {/if}
          <button class="only" on:click={() => showOnly(c)} title={$_('monitor.channelOnlyHint')}>{$_('monitor.channelOnly')}</button>
        </div>
      {/each}
    </div>

    <h4 class="section">{$_('monitor.statsTitle')}</h4>
    <p class="hint">{$_('monitor.statsHint')}</p>
    <div class="stat-picker">
      {#each STAT_KEYS as k}
        <button class="stat-chip" class:on={stats.includes(k)} on:click={() => toggleStat(k)}>
          {#if stats.includes(k)}<Icon name="check" size={11} />{/if}
          {$_('monitor.stat.' + k)}
        </button>
      {/each}
    </div>

    <div class="scope-row">
      <span class="scope-label">{$_('monitor.statsScope')}</span>
      <div class="segmented">
        <button class="seg" class:active={scope === 'window'} on:click={() => dispatch('scope', 'window')}>{$_('monitor.scopeWindow')}</button>
        <button class="seg" class:active={scope === 'all'} on:click={() => dispatch('scope', 'all')}>{$_('monitor.scopeAllData')}</button>
      </div>
    </div>

    <div class="modal-actions">
      <button class="btn tertiary" on:click={showAll}>{$_('monitor.channelAll')}</button>
      <button class="btn" on:click={() => dispatch('close')}>{$_('common.close')}</button>
    </div>
  </div>
</div>

<style>
  .modal-overlay {
    position: fixed;
    inset: 0;
    background-color: var(--backdrop-color-strong);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1300;
  }

  .channels-modal {
    background-color: var(--bg-light-color);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    box-shadow: 0 10px 40px var(--shadow-color-strong);
    padding: 20px;
    width: min(560px, 92vw);
    max-height: 80vh;
    display: flex;
    flex-direction: column;
  }

  .channels-modal h3 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 0 6px;
    font-size: 1.05em;
  }

  .hint {
    margin: 0 0 14px;
    font-size: 0.82em;
    color: var(--text-muted);
  }

  .channel-list {
    flex: 1;
    overflow-y: auto;
    border: 1px solid var(--border-color);
    border-radius: 6px;
  }

  .channel {
    display: grid;
    grid-template-columns: 26px 14px minmax(90px, 1fr) auto auto auto;
    align-items: center;
    gap: 8px;
    padding: 7px 10px;
    border-bottom: 1px solid var(--border-color);
  }

  .channel:last-child {
    border-bottom: none;
  }

  .channel.off .ch-target,
  .channel.off .ch-oid {
    opacity: 0.45;
  }

  .chk {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    box-sizing: border-box;
    border: 2px solid var(--text-muted);
    border-radius: 4px;
    background: none;
    color: var(--text-on-accent);
    cursor: pointer;
  }

  .chk.on {
    background-color: var(--accent-color);
    border-color: var(--accent-color);
  }

  .swatch {
    width: 12px;
    height: 12px;
    border-radius: 3px;
    border: 2px solid;
    box-sizing: border-box;
  }

  .ch-target {
    font-size: 0.85em;
    color: var(--text-color);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ch-oid {
    font-family: 'Courier New', monospace;
    font-size: 0.75em;
    color: var(--oid-color);
  }

  .ch-capped {
    font-size: 0.7em;
    color: var(--warning-color);
  }

  .only {
    background: none;
    border: none;
    color: var(--accent-color);
    font-size: 0.75em;
    cursor: pointer;
    text-decoration: underline;
    padding: 2px 4px;
  }

  .section {
    margin: 16px 0 4px;
    font-size: 0.9em;
    color: var(--text-color);
  }

  .stat-picker {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .stat-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 10px;
    font-size: 0.78em;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 12px;
    color: var(--text-muted);
    cursor: pointer;
  }

  .stat-chip.on {
    color: var(--accent-color);
    border-color: var(--accent-border);
    background-color: var(--accent-subtle);
    font-weight: 600;
  }

  .scope-row {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 12px;
  }

  .scope-label {
    font-size: 0.82em;
    color: var(--text-dimmed);
  }

  .segmented {
    display: inline-flex;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    overflow: hidden;
  }

  .seg {
    padding: 4px 12px;
    font-size: 0.8em;
    background-color: var(--bg-lighter-color);
    border: none;
    border-right: 1px solid var(--border-color);
    color: var(--text-light);
    cursor: pointer;
  }

  .seg:last-child { border-right: none; }

  .seg.active {
    background-color: var(--accent-color);
    color: var(--text-on-accent);
    font-weight: 600;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    margin-top: 14px;
  }
</style>
