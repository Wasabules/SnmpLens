<script>
  import { _ } from 'svelte-i18n';
  import Icon from '../Icon.svelte';
  import { seriesColor, MAX_SERIES } from '../utils/chartPalette';
  import { inferUnit } from '../utils/snmpUnits';
  import { anonMode, anonymizeIp } from '../utils/anonymize';
  import { targetLabels } from '../stores/targetLabels';
  import { mibStore } from '../stores/mibStore';
  import { oidName, oidTooltip } from '../utils/oidDisplay';
  import { computeStats, pointsInRange, DEFAULT_STATS } from '../utils/seriesStats';
  import { classify } from '../utils/thresholdAlerts';

  export let session;
  export let mode = 'raw';
  export let theme = 'dark';
  /** OIDs to summarise. One row per OID inside each target's tile. */
  export let oids = null;
  /** Colour assignment differs between layouts, so the swatches must match. */
  export let layout = 'separate'; // 'separate' | 'stacked'
  /** Series to leave out, as "target|oid" keys. */
  export let hidden = [];
  /** Statistics to show, in order (the first is the row's hero figure). */
  export let stats = DEFAULT_STATS;
  /** 'window' = summarise what the chart shows, 'all' = the whole buffer. */
  export let scope = 'window';
  /** Visible x-window reported by the chart, {min,max} in ms. */
  export let range = null;

  const FIELD = { raw: 'value', delta: 'delta', rate: 'rate', latency: 'responseTimeMs' };
  const SPARK_POINTS = 40;

  // Counts are cardinalities, not measurements: they must not wear the metric's
  // unit (an "avg 12.4 Mbit/s" beside a "count 12.4 M" would be nonsense).
  const COUNT_STATS = new Set(['count', 'errors']);

  $: hiddenSet = new Set(hidden || []);
  $: oidList = (oids && oids.length ? oids : [session?.oid]).filter(Boolean);

  // Label when the target has one — an operator reads "Routeur-Paris" faster
  // than "10.12.4.7". Anonymous Mode still wins: a label names a site just as
  // plainly as an address does.
  function displayTarget(address, labels, anon) {
    if (anon) return anonymizeIp(address);
    return labels[address] || address;
  }

  function formatStat(key, value, u) {
    if (value === null || value === undefined) return '—';
    if (COUNT_STATS.has(key)) return String(Math.round(value));
    return u.format(value);
  }

  // One tile per target, one row per OID inside it: a separate tile grid per
  // OID stacked vertically and burned a lot of height for the same information.
  function buildTiles(s, m, _hidden, _scope, _range, _stats, _layout, _theme) {
    const source = s?.results || [];
    const field = FIELD[m] || 'value';
    const dark = theme !== 'light';

    // Target order per OID mirrors the chart (first appearance in the data).
    const targetsByOid = new Map();
    for (const o of oidList) {
      const scoped = source.filter((r) => (r.oid || s?.oid) === o);
      targetsByOid.set(o, [...new Set(scoped.map((r) => r.target))]);
    }

    const allTargets = [...new Set(source.map((r) => r.target))];

    return allTargets
      .map((target) => {
        const rows = [];
        oidList.forEach((o, oidIdx) => {
          const targets = targetsByOid.get(o) || [];
          const tIdx = targets.indexOf(target);
          if (tIdx < 0) return;
          if (hiddenSet.has(target + '|' + o)) return;

          // Same counter the chart uses, so a swatch here is the colour drawn there.
          const colorIdx = layout === 'stacked' ? oidIdx * targets.length + tIdx : tIdx;
          if (colorIdx >= MAX_SERIES) return;

          const series = source.filter((r) => r.target === target && (r.oid || s?.oid) === o);
          if (!series.length) return;

          // Statistics follow the requested scope; the sparkline and the live
          // state always describe the newest data, whatever the scope.
          const statSource = scope === 'window' ? pointsInRange(series, range) : series;
          const summary = computeStats(statSource, field);

          const values = series.map((r) => r[field]).filter((v) => v !== null && v !== undefined);
          const recent = values.slice(-SPARK_POINTS);
          const latest = values.length ? values[values.length - 1] : null;
          const lastPoint = series[series.length - 1];

          let trend = 0;
          if (recent.length >= 4) {
            const prior = recent.slice(0, -1);
            const mean = prior.reduce((a, b) => a + b, 0) / prior.length;
            if (mean !== 0) trend = ((latest - mean) / Math.abs(mean)) * 100;
          }

          // Same classifier the alerting path uses, so a red tile and a raised
          // alert can never disagree. Thresholds are per OID.
          const outOfRange = m === 'raw' && !!classify(latest, (s.thresholds || {})[o]);

          rows.push({
            oid: o,
            color: seriesColor(colorIdx, dark),
            unit: inferUnit(o, (series.find((r) => r.snmpType) || {}).snmpType || '', m),
            summary,
            spark: recent,
            trend,
            failing: !!(lastPoint && (lastPoint.error || lastPoint.value === null)),
            outOfRange,
          });
        });
        return { target, rows };
      })
      .filter((t) => t.rows.length);
  }

  $: tiles = buildTiles(session, mode, hiddenSet, scope, range, stats, layout, theme);

  // Sparkline as an SVG polyline in a 100x20 viewBox.
  function sparkPath(values) {
    if (!values || values.length < 2) return '';
    const min = Math.min(...values);
    const max = Math.max(...values);
    const span = max - min || 1;
    const stepX = 100 / (values.length - 1);
    return values
      .map((v, i) => `${(i * stepX).toFixed(2)},${(18 - ((v - min) / span) * 16).toFixed(2)}`)
      .join(' ');
  }
</script>

{#if tiles.length}
  <div class="tiles">
    {#each tiles as tile (tile.target)}
      <div class="tile">
        <div class="tile-head" title={$anonMode ? anonymizeIp(tile.target) : tile.target}>
          {displayTarget(tile.target, $targetLabels, $anonMode)}
        </div>

        {#each tile.rows as row (row.oid)}
          <div class="row" class:alert={row.outOfRange} class:down={row.failing}>
            <span class="swatch" style="background-color: {row.color}"></span>

            {#if oidList.length > 1}
              <span class="row-oid" title={oidTooltip(row.oid, $mibStore.tree)}>
                {oidName(row.oid, $mibStore.tree)}
              </span>
            {/if}

            <span class="row-hero" title={$_('monitor.stat.' + (stats[0] || 'last'))}>
              {formatStat(stats[0] || 'last', row.summary?.[stats[0] || 'last'], row.unit)}
            </span>

            {#if row.spark.length > 1}
              <svg class="spark" viewBox="0 0 100 20" preserveAspectRatio="none" aria-hidden="true">
                <polyline
                  points={sparkPath(row.spark)}
                  fill="none"
                  stroke={row.color}
                  stroke-width="2"
                  vector-effect="non-scaling-stroke"
                  stroke-linejoin="round"
                  stroke-linecap="round"
                />
              </svg>
            {:else}
              <span></span>
            {/if}

            <span class="row-state">
              {#if row.failing}
                <span class="state critical"><Icon name="x" size={11} /> {$_('monitor.tileDown')}</span>
              {:else if row.outOfRange}
                <span class="state critical"><Icon name="triangle-alert" size={11} /> {$_('monitor.tileOutOfRange')}</span>
              {:else if Math.abs(row.trend) >= 1}
                <span class="state">
                  <Icon name={row.trend > 0 ? 'trending-up' : 'trending-down'} size={11} />
                  {row.trend > 0 ? '+' : ''}{row.trend.toFixed(0)} %
                </span>
              {:else}
                <span class="state muted"><Icon name="activity" size={11} /> {$_('monitor.tileStable')}</span>
              {/if}
            </span>
          </div>

          {#if stats.length > 1}
            <dl class="row-stats">
              {#each stats.slice(1) as k}
                <div class="stat-pair">
                  <dt>{$_('monitor.stat.' + k)}</dt>
                  <dd>{formatStat(k, row.summary?.[k], row.unit)}</dd>
                </div>
              {/each}
            </dl>
          {/if}
        {/each}
      </div>
    {/each}
  </div>
{/if}

<style>
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
    gap: 10px;
    padding: 10px 15px 0;
  }

  .tile {
    padding: 9px 12px 8px;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    background-color: var(--bg-color);
  }

  /* The target names the tile; its OID rows sit underneath. */
  .tile-head {
    font-size: 0.8em;
    font-weight: 600;
    color: var(--text-color);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-bottom: 4px;
  }

  .row {
    display: grid;
    grid-template-columns: 10px auto minmax(70px, auto) minmax(40px, 1fr) auto;
    align-items: center;
    gap: 8px;
  }

  .swatch {
    width: 10px;
    height: 10px;
    border-radius: 2px;
  }

  .row-oid {
    font-family: 'Courier New', monospace;
    font-size: 0.72em;
    color: var(--oid-color);
    white-space: nowrap;
  }

  .row-hero {
    font-size: 1.05em;
    font-weight: 600;
    color: var(--text-color);
    white-space: nowrap;
  }

  .spark {
    display: block;
    width: 100%;
    height: 18px;
    opacity: 0.85;
  }

  .row-state {
    font-size: 0.72em;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .state {
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }

  .state.critical {
    color: var(--error-color);
    font-weight: 600;
  }

  .state.muted {
    opacity: 0.75;
  }

  .row.alert .row-hero,
  .row.down .row-hero {
    color: var(--error-color);
  }

  .row-stats {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 12px;
    margin: 0 0 5px 18px;
    font-size: 0.72em;
  }

  .stat-pair {
    display: inline-flex;
    gap: 4px;
  }

  .row-stats dt {
    color: var(--text-muted);
  }

  .row-stats dd {
    margin: 0;
    color: var(--text-light);
    font-variant-numeric: tabular-nums;
  }
</style>
