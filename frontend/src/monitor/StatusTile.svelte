<script>
  import { _ } from 'svelte-i18n';
  import { oidName } from '../utils/oidDisplay';

  /**
   * A reading shown as a STATE, mapped through the widget's own labels.
   *
   * The one renderer a preset genuinely needs that nothing here already does.
   * formatValueWithEnum maps through the MIB tree's enumValues; a preset
   * carries its OWN map, deliberately — it names the states in the author's
   * words, and it works for a device whose MIB is not loaded.
   *
   * Props only: no store is read here, which is what keeps it clean under
   * reactive.test.mjs and what makes one component serve both the single tile
   * and the wall of them.
   */
  export let oids;
  export let labels = {};
  export let points = [];
  export let mibTree = null;
  export let dense = false;

  // Every function called from the markup takes what it reads as an argument.
  const latest = (oid, all) => {
    let best = null;
    for (const p of all) {
      if (p.oid !== oid) continue;
      if (!best || p.timestamp > best.timestamp) best = p;
    }
    return best;
  };

  const stateOf = (point) => {
    if (!point) return { key: '', text: '—', kind: 'unknown' };
    if (point.error) return { key: '', text: point.error, kind: 'failed' };
    if (point.value === null || point.value === undefined) return { key: '', text: '—', kind: 'unknown' };
    return { key: String(Math.round(point.value)), text: '', kind: 'value' };
  };

  // The label map decides the WORD; the number decides the colour only when the
  // author gave no word for it. A state nobody named is shown as its number
  // rather than as a guess.
  const cellText = (point, map) => {
    const st = stateOf(point);
    if (st.kind !== 'value') return st.text;
    return map[st.key] || st.key;
  };

  const cellKind = (point, map) => {
    const st = stateOf(point);
    if (st.kind !== 'value') return st.kind;
    const word = (map[st.key] || '').toLowerCase();
    if (/^(up|ok|on|active|normal|enabled|good|running)$/.test(word)) return 'good';
    if (/^(down|off|fail|failed|error|critical|inactive|disabled)$/.test(word)) return 'bad';
    if (/^(testing|unknown|dormant|warning|degraded)$/.test(word)) return 'warn';
    return 'value';
  };

  const shortName = (oid, tree) => {
    const name = oidName(oid, tree);
    // The instance is what tells two ports apart, so it is what a cell shows.
    const last = String(oid).split('.').pop();
    return name && name !== oid ? `${name}.${last}` : last;
  };
</script>

<div class="states" class:dense>
  {#each oids as oid (oid)}
    <div class="cell {cellKind(latest(oid, points), labels)}" title={oid}>
      <span class="cell-name">{shortName(oid, mibTree)}</span>
      <span class="cell-state">{cellText(latest(oid, points), labels)}</span>
    </div>
  {/each}
  {#if oids.length === 0}
    <p class="empty">{$_('dashboard.noReading')}</p>
  {/if}
</div>

<style>
  .states {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    gap: 4px;
  }

  .states.dense {
    grid-template-columns: repeat(auto-fill, minmax(78px, 1fr));
  }

  .cell {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 0.35rem 0.45rem;
    border: 1px solid var(--border-color);
    border-left-width: 3px;
    border-radius: 3px;
    background-color: var(--bg-secondary);
    min-width: 0;
  }

  .cell-name {
    color: var(--text-secondary);
    font-size: 0.68rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .cell-state {
    font-size: 0.85rem;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* Semantic colour, on the border only: a wall of forty-eight filled cells is
     a wall of colour, and the one that is down has to be the thing you see. */
  .cell.good {
    border-left-color: var(--success-color, #3fb950);
  }

  .cell.bad {
    border-left-color: var(--error-color, #f85149);
    background-color: var(--error-subtle, transparent);
  }

  .cell.warn {
    border-left-color: var(--warning-color, #d29922);
  }

  .cell.failed {
    border-left-color: var(--error-color, #f85149);
  }

  .cell.failed .cell-state {
    color: var(--error-color, #f85149);
    font-size: 0.7rem;
  }

  .cell.unknown {
    border-left-color: var(--border-color);
  }

  .empty {
    margin: 0;
    color: var(--text-secondary);
    font-size: 0.78rem;
  }
</style>
