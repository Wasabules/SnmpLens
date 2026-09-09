<script>
  import { _ } from 'svelte-i18n';
  import { oidName } from '../utils/oidDisplay';
  import { statusBlocks } from '../utils/dashboardGroup';
  import { anonymizeIp } from '../utils/anonymize';
  import { latestFor, stateText, stateKind } from '../utils/stateColour';

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
  /**
   * The equipments these readings come from. One is the ordinary case and
   * changes nothing; several is a dashboard drawn across a group of them.
   *
   * It is REQUIRED for correctness rather than for presentation: a cell is one
   * OID, and three switches bound from one preset all answer 1.3.6.1.2.1.2.2.1.8.1.
   * Without knowing the targets apart, the wall shows whichever of the three
   * answered most recently, in one cell, with nothing saying so.
   */
  export let targets = [];
  /**
   * Anonymous Mode, passed in rather than read from the store: this
   * component reads no store at all, which is what keeps it clean under
   * reactive.test.mjs. The addresses are masked for DISPLAY only — the
   * blocks are still matched on the real ones, which is what the readings
   * carry.
   */
  export let masked = false;

  // The state rules live in utils/stateColour.js, and both widgets that show a
  // state read them from there: the same port cannot be green on the wall and
  // grey on the map, which is what two copies of one regular expression produce
  // the first time either gains a word.

  // The INSTANCE, and only the instance.
  //
  // The first capture of a port grid showed eight cells all reading
  // "ifOperStatu…" — the name is the same on every cell of a grid by
  // construction, so it is the one part that carries no information, and it was
  // the part that survived the ellipsis while the instance was cut. The name
  // belongs on the widget's title and in the tooltip; the cell shows what tells
  // two ports apart.
  const instanceOf = (oid) => String(oid).split('.').pop();
  const fullName = (oid, tree) => {
    const name = oidName(oid, tree);
    return name && name !== oid ? `${name}.${instanceOf(oid)}` : oid;
  };
</script>

{#each statusBlocks(points, targets, oids) as block (block.target)}
  {#if block.target}
    <p class="equipment">{masked ? anonymizeIp(block.target) : block.target}</p>
  {/if}
  <div class="states" class:dense>
    {#each block.oids as oid (oid)}
      <div class="cell {stateKind(latestFor(oid, block.points), labels)}" title={fullName(oid, mibTree)}>
        <span class="cell-name">{instanceOf(oid)}</span>
        <span class="cell-state">{stateText(latestFor(oid, block.points), labels)}</span>
      </div>
    {/each}
    {#if block.oids.length === 0}
      <p class="empty">{$_('dashboard.noReading')}</p>
    {/if}
  </div>
{/each}

<style>
  /* Only drawn when there is more than one equipment, so it never appears on
     the ordinary dashboard: an address above a single wall of ports is a label
     for something that has no alternative. */
  .equipment {
    margin: 0.5rem 0 0.25rem;
    color: var(--text-muted);
    font-size: 0.72rem;
    font-variant-numeric: tabular-nums;
  }

  .equipment:first-child {
    margin-top: 0;
  }

  .states {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    gap: 4px;
  }

  /* Narrower, because a cell now holds an instance number and a word rather
     than a truncated OID name. Forty-eight of them fit across a panel. */
  .states.dense {
    grid-template-columns: repeat(auto-fill, minmax(62px, 1fr));
  }

  .cell {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 0.35rem 0.45rem;
    border: 1px solid var(--border-color);
    border-left-width: 3px;
    border-radius: 3px;
    background-color: var(--bg-light-color);
    min-width: 0;
  }

  .cell-name {
    color: var(--text-muted);
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
    color: var(--text-muted);
    font-size: 0.78rem;
  }
</style>
