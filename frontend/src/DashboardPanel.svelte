<script>
  import { _ } from 'svelte-i18n';
  import Icon from './Icon.svelte';
  import MetricTiles from './monitor/MetricTiles.svelte';
  import MonitorChart from './monitor/MonitorChart.svelte';
  import StatusTile from './monitor/StatusTile.svelte';
  import { pollingStore } from './stores/pollingStore';
  import { mibStore } from './stores/mibStore';
  import { settingsStore } from './stores/settingsStore';
  import { dashboardStore, dashboardSession, dashboardCandidates } from './stores/dashboardStore';
  import { formatCadence } from './utils/formatting';
  import { anonMode, anonymizeIp } from './utils/anonymize';

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
  $: session = dashboardSession($pollingStore, $dashboardStore.sessionId);
  $: widgets = session?.preset?.widgets || [];
  $: theme = $settingsStore.theme === 'light' ? 'light' : 'dark';

  // One sync group per session, so panning one chart pans the others.
  $: syncGroup = session ? `dashboard:${session.id}` : null;

  const targetOf = (s, masked) => {
    const address = (s?.targets || [])[0] || '';
    return masked ? anonymizeIp(address) : address;
  };

  const sessionLabel = (s, masked) => {
    const address = (s.targets || [])[0] || '';
    const shown = masked ? anonymizeIp(address) : address;
    return s.name ? `${s.name} — ${shown}` : shown;
  };

  // A preset carries the unit it means, because inferUnit reads the OID NAME
  // and a preset carries numeric OIDs only: ifInOctets would render as "1.2 G"
  // where the monitor tab renders "9.8 Gbit/s".
  const unitOf = (widget) => widget.unit || '';
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
      <div class="widgets">
        {#each widgets as widget, i (i)}
          <section class="widget" class:wide={widget.kind === 'chart' || widget.kind === 'grid'}>
            <header class="widget-head">
              <h3>{widget.title || $_(`preset.widget.${widget.kind}`)}</h3>
              {#if widget.unit}<span class="unit">{widget.unit}</span>{/if}
            </header>

            {#if widget.kind === 'value'}
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
              <div class="chart-box">
                <MonitorChart
                  {session}
                  oids={widget.oids}
                  mode="raw"
                  {theme}
                  {syncGroup}
                />
              </div>
            {:else if widget.kind === 'status' || widget.kind === 'grid'}
              <StatusTile
                oids={widget.oids}
                labels={widget.labels || {}}
                points={session.results}
                mibTree={$mibStore.tree}
                dense={widget.kind === 'grid'}
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
        {$_('dashboard.snapshotNote', { values: { file: session.preset.file, target: targetOf(session, $anonMode) } })}
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
    color: var(--text-secondary);
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
    color: var(--text-secondary);
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }

  .meta {
    color: var(--text-secondary);
    font-size: 0.78rem;
  }

  .state {
    padding: 0.05rem 0.4rem;
    border-radius: 3px;
    background-color: var(--bg-tertiary);
    color: var(--text-secondary);
    font-size: 0.72rem;
  }

  .state.running {
    background-color: var(--success-subtle, var(--bg-tertiary));
    color: var(--success-color, var(--text-primary));
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
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 0.6rem;
    align-items: start;
  }

  /* A chart and a wall of ports both need width; a single reading does not. */
  .widget.wide {
    grid-column: 1 / -1;
  }

  .widget {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    padding: 0.6rem;
    border: 1px solid var(--border-color);
    border-radius: 5px;
    background-color: var(--bg-primary);
    min-width: 0;
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
    color: var(--text-secondary);
    font-size: 0.72rem;
  }

  .chart-box {
    height: 240px;
    min-width: 0;
  }

  .unknown-kind {
    margin: 0;
    color: var(--text-secondary);
    font-size: 0.78rem;
  }

  .footnote {
    margin: 0;
    color: var(--text-secondary);
    font-size: 0.72rem;
  }
</style>
