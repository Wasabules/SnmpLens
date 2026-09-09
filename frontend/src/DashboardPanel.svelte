<script>
  import { _ } from 'svelte-i18n';
  import Icon from './Icon.svelte';
  import MetricTiles from './monitor/MetricTiles.svelte';
  import MonitorChart from './monitor/MonitorChart.svelte';
  import StatusTile from './monitor/StatusTile.svelte';
  import MapTile from './monitor/MapTile.svelte';
  import { pollingStore } from './stores/pollingStore';
  import { mibStore } from './stores/mibStore';
  import { settingsStore } from './stores/settingsStore';
  import { dashboardStore, dashboardSession, dashboardCandidates } from './stores/dashboardStore';
  import { formatCadence } from './utils/formatting';
  import { anonMode, anonymizeIp } from './utils/anonymize';
  import { hasLayout, cellStyle, readingOrder } from './utils/presetLayout';
  import { dashboardGroups, dashboardView } from './utils/dashboardGroup';

  // The dashboard: one bound preset, drawn.
  //
  // It reads the SNAPSHOT the session carries, never the preset file. Editing a
  // preset afterwards changes nothing here, and deleting it changes nothing
  // either — rebinding is how an edit is adopted, and that is the same rule the
  // Go side stores.
  //
  // Every derivation is a reactive statement rather than a call in the markup:
  // Svelte 5 tracks what the EXPRESSION reads, and a store read inside a callee
  // is not a dependency it can see. reactive.test.mjs exists for that.

  $: candidates = dashboardCandidates($pollingStore);
  // The groups come first because the fallback must not be one: landing on a
  // merged view of eight switches, unasked, on a machine that has never opened
  // this tab, is a decision the operator did not make.
  $: groups = dashboardGroups($pollingStore);
  $: fallback = dashboardSession($pollingStore, null);
  $: view = dashboardView($pollingStore, $dashboardStore.selection, fallback);
  $: session = view.session;
  $: widgets = session?.preset?.widgets || [];
  $: theme = $settingsStore.theme === 'light' ? 'light' : 'dark';

  // One sync group per session, so panning one chart pans the others.
  $: syncGroup = session ? `dashboard:${session.id}` : null;

  // Two arrangements, and which one applies is decided by the preset rather
  // than by this component. A preset that places nothing keeps the reflowing
  // list of cards it has always had; switching every existing dashboard to a
  // twelve-column grid to make room for a feature they do not use would change
  // what they look like for nothing.
  $: placed = hasLayout(widgets);
  $: orders = readingOrder(widgets);

  const targetOf = (s, masked) => {
    const address = (s?.targets || [])[0] || '';
    return masked ? anonymizeIp(address) : address;
  };

  const sessionLabel = (s, masked) => {
    const address = (s.targets || [])[0] || '';
    const shown = masked ? anonymizeIp(address) : address;
    return s.name ? `${s.name} — ${shown}` : shown;
  };

  // A group is named by its preset and its count, never by its addresses: eight
  // of them do not fit in a select, and the one thing the operator needs to
  // tell it from the entries below it is that it holds all of them.
  const groupLabel = (g, t) =>
    t('dashboard.allEquipments', {
      values: { name: g.name || g.file, count: g.sessions.length },
    });

  // A preset carries the unit it means, because inferUnit reads the OID NAME
  // and a preset carries numeric OIDs only: ifInOctets would render as "1.2 G"
  // where the monitor tab renders "9.8 Gbit/s".
  const unitOf = (widget) => widget.unit || '';

  // Whether a widget has anything to draw yet. Takes both its inputs as
  // arguments, like everything else called from the markup here.
  const hasData = (s, widget) => {
    const wanted = new Set(widget.oids || []);
    return (s.results || []).some((r) => wanted.has(r.oid));
  };
</script>

<div class="dashboard-panel">
  {#if candidates.length === 0}
    <div class="empty-state">
      <p>{$_('dashboard.empty')}</p>
      <p class="hint">{$_('dashboard.emptyHint')}</p>
    </div>
  {:else}
    <div class="dash-head">
      <label class="picker">
        <span class="picker-label">{$_('dashboard.equipment')}</span>
        <select
          value={session ? session.id : ''}
          on:change={(e) => dashboardStore.select(e.currentTarget.value)}
        >
          {#each groups as g (g.id)}
            <option value={g.id}>{groupLabel(g, $_)}</option>
          {/each}
          {#each candidates as c (c.id)}
            <option value={c.id}>{sessionLabel(c, $anonMode)}</option>
          {/each}
        </select>
      </label>

      {#if session}
        <span class="meta">
          {$_('dashboard.summary', {
            values: {
              preset: session.preset.name || session.preset.file,
              cadence: formatCadence(session.interval),
              oids: (session.oids || []).length,
            },
          })}
        </span>
        <span class="state" class:running={session.running}>
          {session.running ? $_('monitor.running') : $_('monitor.stopped')}
        </span>
      {/if}
    </div>

    {#if session?.overrun}
      <!-- The guardrail's question, rendered once and in one place: the same
           banner the monitor tab shows, driven by the same session field. -->
      <div class="overrun-banner" role="status">
        <Icon name="triangle-alert" size={14} />
        <div class="overrun-text">
          <strong>{$_('monitor.overrunTitle')}</strong>
          {#if session.overrun.accepted}
            {$_('monitor.overrunAccepted', { values: {
              interval: formatCadence(session.overrun.intervalMs),
              cycle: formatCadence(session.overrun.cycleMs),
            } })}
          {:else}
            {$_('monitor.overrunBody', { values: {
              interval: formatCadence(session.overrun.intervalMs),
              cycle: formatCadence(session.overrun.cycleMs),
              effective: formatCadence(session.overrun.effectiveMs),
            } })}
          {/if}
        </div>
        {#if !session.overrun.accepted}
          <button
            class="btn btn-small"
            title={$_('monitor.overrunAcceptHint')}
            on:click={() => pollingStore.acceptSlow(session.id)}
          >{$_('monitor.overrunAccept')}</button>
        {/if}
      </div>
    {/if}

    {#if session}
      <div class="widgets" class:grid12={placed}>
        {#each widgets as widget, i (i)}
          <!-- The placement is custom properties and a class, never an inline
               grid-column: an inline placement wins over every rule, and the
               media query that collapses this to one column would have nothing
               to override. -->
          <section
            class="widget"
            class:wide={widget.kind === 'chart' || widget.kind === 'grid' || widget.kind === 'map'}
            class:placed={!!widget.layout}
            style="{cellStyle(widget)};--order:{orders[i]}"
          >
            <header class="widget-head">
              <h3>{widget.title || $_(`preset.widget.${widget.kind}`)}</h3>
              {#if widget.unit}<span class="unit">{widget.unit}</span>{/if}
            </header>

            {#if !hasData(session, widget)}
              <!-- Before the first sample there is nothing to draw, and an
                   empty card with a title on it reads as a broken widget. Said
                   once per widget rather than left blank. -->
              <p class="waiting">{$_('dashboard.waiting')}</p>
            {:else if widget.kind === 'value'}
              <MetricTiles
                {session}
                oids={widget.oids}
                mode="raw"
                {theme}
                stats={['last']}
              />
            {:else if widget.kind === 'rate'}
              <MetricTiles
                {session}
                oids={widget.oids}
                mode="rate"
                {theme}
                stats={['last', 'avg', 'max']}
              />
            {:else if widget.kind === 'chart'}
              <MonitorChart
                {session}
                oids={widget.oids}
                mode="raw"
                {theme}
                {syncGroup}
              />
            {:else if widget.kind === 'status' || widget.kind === 'grid'}
              <StatusTile
                oids={widget.oids}
                labels={widget.labels || {}}
                points={session.results}
                targets={session.targets || []}
                masked={$anonMode}
                mibTree={$mibStore.tree}
                dense={widget.kind === 'grid'}
              />
            {:else if widget.kind === 'map'}
              <MapTile
                drawing={widget.map}
                labels={widget.labels || {}}
                points={session.results}
                targets={session.targets || []}
                mibTree={$mibStore.tree}
              />
            {:else}
              <!-- A kind this version does not draw. Said out loud rather than
                   rendered as an empty box: a preset written for a newer
                   release is a thing that will happen. -->
              <p class="unknown-kind">{$_('dashboard.unknownWidget', { values: { kind: widget.kind } })}</p>
            {/if}
          </section>
        {/each}
      </div>

      <p class="footnote">
        {#if session.groupOf > 1}
          <!-- The snapshot rule holds per session, so a group has as many
               snapshots as it has equipments. They are known to agree — the
               grouping key is the widget vocabulary itself — but "as it was
               when 10.0.0.1 was bound" would name one of eight arbitrarily. -->
          {$_('dashboard.snapshotNoteGroup', {
            values: { file: session.preset.file, count: session.groupOf },
          })}
        {:else}
          {$_('dashboard.snapshotNote', {
            values: { file: session.preset.file, target: targetOf(session, $anonMode) },
          })}
        {/if}
      </p>
    {/if}
  {/if}
</div>

<style>
  .dashboard-panel {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 0.75rem;
    min-width: 0;
  }

  .empty-state {
    padding: 2rem;
    text-align: center;
    border: 1px dashed var(--border-color);
    border-radius: 6px;
  }

  .hint {
    color: var(--text-muted);
    font-size: 0.8rem;
  }

  .dash-head {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    flex-wrap: wrap;
  }

  .picker {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .picker-label {
    color: var(--text-muted);
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }

  .meta {
    color: var(--text-muted);
    font-size: 0.78rem;
  }

  .state {
    padding: 0.05rem 0.4rem;
    border-radius: 3px;
    background-color: var(--bg-lighter-color);
    color: var(--text-muted);
    font-size: 0.72rem;
  }

  .state.running {
    background-color: var(--success-subtle, var(--bg-lighter-color));
    color: var(--success-color, var(--text-color));
  }

  .overrun-banner {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.5rem 0.75rem;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    background-color: var(--warning-subtle);
    color: var(--warning-color);
    font-size: 0.82rem;
  }

  .overrun-text {
    flex: 1;
    min-width: 0;
  }

  .widgets {
    display: grid;
    /* 340, not 280: a value tile carries an address, a figure, a sparkline and
       a trend, and at 280 the first capture showed it spilling past the card's
       own border. */
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 0.6rem;
    align-items: start;
  }

  /* A chart and a wall of ports both need width; a single reading does not. */
  .widget.wide {
    grid-column: 1 / -1;
  }

  /* The arrangement a preset asked for. Twelve columns because that is what
     divides into halves, thirds and quarters, which is what a dashboard is made
     of; rows size themselves to their content, so a chart declaring two rows is
     twice a tile and never a fixed number of pixels that a font size breaks. */
  .widgets.grid12 {
    grid-template-columns: repeat(12, minmax(0, 1fr));
    grid-auto-rows: minmax(110px, auto);
  }

  .widgets.grid12 .widget.placed {
    grid-column: var(--x) / span var(--w);
    grid-row: var(--y) / span var(--h);
  }

  /* A widget the author did not place, in a preset where others are. The
     browser puts it in the first free space, and a third of the width is the
     nearest thing to what it would have had in the reflowing arrangement. */
  .widgets.grid12 .widget:not(.placed) {
    grid-column: auto / span 4;
  }

  .widgets.grid12 .widget:not(.placed).wide {
    grid-column: 1 / -1;
  }

  /* Narrow enough that twelve columns is four characters each. Everything
     becomes one column, and `order` keeps the author's sequence — top to
     bottom, then left to right — instead of falling back to the order the
     widgets happen to be declared in. */
  @media (max-width: 900px) {
    .widgets.grid12 {
      grid-template-columns: 1fr;
    }

    .widgets.grid12 .widget,
    .widgets.grid12 .widget.placed,
    .widgets.grid12 .widget:not(.placed) {
      grid-column: 1 / -1;
      grid-row: auto;
      order: var(--order);
    }
  }

  .widget {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    padding: 0.6rem;
    border: 1px solid var(--border-color);
    border-radius: 5px;
    background-color: var(--bg-color);
    min-width: 0;
    /* Wide content scrolls inside its own widget rather than out of it. A tile
       with a long address, or a chart on a narrow window, must not draw over
       the widget beside it. */
    overflow-x: auto;
  }

  .widget-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.5rem;
  }

  h3 {
    margin: 0;
    font-size: 0.82rem;
    font-weight: 600;
  }

  .unit {
    color: var(--text-muted);
    font-size: 0.72rem;
  }

  .unknown-kind {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.78rem;
  }

  .waiting {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.78rem;
  }

  .footnote {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.72rem;
  }
</style>
