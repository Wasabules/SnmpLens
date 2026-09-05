<script>
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';
  import { Chart, registerables } from 'chart.js';
  import 'chartjs-adapter-date-fns';
  import zoomPlugin from 'chartjs-plugin-zoom';
  import Icon from '../Icon.svelte';
  import { seriesColor, chartChrome, MAX_SERIES, STATUS } from '../utils/chartPalette';
  import { inferUnit } from '../utils/snmpUnits';
  import { anonMode, anonymizeIp } from '../utils/anonymize';
  import { targetLabels } from '../stores/targetLabels';
  import { mibStore } from '../stores/mibStore';
  import { oidName } from '../utils/oidDisplay';
  import { notificationStore } from '../stores/notifications';
  import { downloadFile } from '../utils/csv';
  import { chartSync, broadcastRange, broadcastLive } from './chartSync';

  Chart.register(...registerables, zoomPlugin);

  /** Session being charted (needs .id, .oid, .results, .thresholds). */
  export let session;
  /** 'raw' | 'delta' | 'rate' | 'latency' */
  export let mode = 'raw';
  /** Optional point override — charts stored history instead of live data. */
  export let points = null;
  /** Optional aggregated buckets: [{target, timestamp, avgValue, avgRate, ...}] */
  export let buckets = null;
  /** Resolved theme ('dark' | 'light'), passed in so a switch re-themes the chart. */
  export let theme = 'dark';
  /** When a session polls several OIDs, the one this chart renders. */
  export let oid = null;
  /**
   * Stacked mode: render these OIDs as curves on ONE plot, each with its own
   * y-axis (alternating left/right, tinted like its curve).
   *
   * Note this is a deliberate exception to the usual one-axis rule: stacking
   * measures of different scale makes their relative shape easy to compare but
   * their relative HEIGHT meaningless. Colour-matched axes are the mitigation,
   * and "separate" (small multiples) stays the default.
   */
  export let oids = null;
  /**
   * Series to leave out, as "target|oid" keys. Colours are assigned BEFORE
   * filtering: a colour belongs to a series, not to its rank, so hiding one
   * curve must never repaint the others.
   */
  export let hidden = [];
  /**
   * Persisted view options ({windowMs, yScaleType, zeroBased, height}). Read
   * once at mount: toggling separate/stacked or leaving the tab builds a NEW
   * chart component, and these must come back as the user left them.
   */
  export let options = {};

  const dispatch = createEventDispatcher();

  $: hiddenSet = new Set(hidden || []);
  const seriesKey = (target, o) => target + '|' + o;

  $: stacked = Array.isArray(oids) && oids.length > 1 ? oids : null;
  /** Charts sharing a syncGroup keep the same x window (small multiples). */
  export let syncGroup = null;

  // Identity within the sync group, so this chart ignores the echo of its own
  // broadcast instead of ping-ponging with its siblings.
  const syncId = Math.random().toString(36).slice(2);
  let applyingSync = false;
  // resetZoom() fires onZoomComplete itself, so without this guard our own
  // "back to live" call would immediately switch tracking back off — and the
  // LIVE button could never re-arm.
  let programmaticZoom = false;

  let canvas;
  let chart = null;
  let fullscreen = false; // deliberately not persisted: transient by nature
  let yScaleType = options.yScaleType || 'linear'; // 'linear' | 'logarithmic'
  let zeroBased = options.zeroBased ?? false;
  let height = options.height ?? 260;
  let hiddenSeriesNotice = 0;
  let stackedAxes = [];

  // Oscilloscope behaviour: the chart shows a sliding window of `windowMs` over
  // a much deeper buffer. While `follow` is on the window tracks the newest
  // sample; zooming or panning hands control to the user (follow off) so the
  // view stops jumping under them, and LIVE hands it back.
  const TIMEBASES = [60000, 300000, 900000, 3600000, 0]; // 0 = whole buffer
  let windowMs = options.windowMs ?? 300000;
  let follow = true; // always resume live on mount

  // A chart rendering stored history is never live.
  $: isHistorical = !!(points || buckets);

  // Surface option changes so the parent can persist them.
  $: dispatch('options', { windowMs, yScaleType, zeroBased, height });

  // Publish the window actually on screen, so the stat tiles can summarise
  // exactly what the user is looking at.
  function emitRange() {
    if (!chart || !chart.scales || !chart.scales.x) return;
    dispatch('range', { min: chart.scales.x.min, max: chart.scales.x.max });
  }

  const FIELD = { raw: 'value', delta: 'delta', rate: 'rate', latency: 'responseTimeMs' };

  function snmpTypeOf(s) {
    const p = (s?.results || []).find((r) => r.snmpType && (!oid || (r.oid || s?.oid) === oid));
    return p ? p.snmpType : '';
  }

  // Points of the OID this view is about (all of them for a single-OID session).
  function forOid(list, s) {
    if (!oid) return list;
    return list.filter((r) => (r.oid || s?.oid || oid) === oid);
  }

  $: unit = inferUnit(oid || session?.oid || '', snmpTypeOf(session), mode);

  $: labels = $targetLabels;
  $: mibTree = $mibStore.tree;

  // Series are named by their label when the target has one; the raw address
  // stays available in the channel picker and the tiles' tooltip.
  function label(target) {
    return get(anonMode) ? anonymizeIp(target) : labels[target] || target;
  }

  // One curve per (OID x target), each OID bound to its own axis. Capped at the
  // validated palette size so no two series ever share a colour.
  function buildStacked(s, m) {
    const dark = theme !== 'light';
    const source = s?.results || [];
    const field = FIELD[m] || 'value';
    const datasets = [];
    const axes = [];
    let colorIdx = 0;

    stacked.forEach((o, i) => {
      const scoped = source.filter((r) => (r.oid || s?.oid) === o);
      const targets = [...new Set(scoped.map((r) => r.target))];
      if (!targets.length || colorIdx >= MAX_SERIES) return;

      const axisId = 'y' + i;
      const type = (scoped.find((r) => r.snmpType) || {}).snmpType || '';
      axes.push({
        id: axisId,
        index: axes.length,
        oid: o,
        unit: inferUnit(o, type, m),
        color: seriesColor(colorIdx, dark),
      });

      for (const t of targets) {
        if (colorIdx >= MAX_SERIES) break;
        if (hiddenSet.has(seriesKey(t, o))) {
          colorIdx++; // keep the colour reserved for this series
          continue;
        }
        datasets.push({
          label: (targets.length > 1 ? label(t) + ' · ' : '') + oidName(o, mibTree),
          yAxisID: axisId,
          data: scoped
            .filter((r) => r.target === t)
            .map((r) => ({ x: Date.parse(r.timestamp), y: r[field] ?? null })),
          borderColor: seriesColor(colorIdx, dark),
          backgroundColor: seriesColor(colorIdx, dark),
          borderWidth: 2,
          pointRadius: 0,
          pointHoverRadius: 4,
          tension: 0.25,
          spanGaps: false,
        });
        colorIdx++;
      }
    });

    hiddenSeriesNotice = 0;
    return { datasets, axes };
  }

  // --- Data -----------------------------------------------------------------

  // Points keep their null holes: a failed poll must BREAK the line, never be
  // bridged — a line interpolated across an outage reads as perfectly healthy.
  function buildDatasets(s, m, override, bucketed) {
    const field = FIELD[m] || 'value';
    const dark = theme !== 'light';

    if (bucketed && bucketed.length) {
      const scoped = oid ? bucketed.filter((b) => !b.oid || b.oid === oid) : bucketed;
      const targets = [...new Set(scoped.map((b) => b.target))].slice(0, MAX_SERIES);
      return targets.map((target, idx) => ({
        label: label(target),
        data: scoped
          .filter((b) => b.target === target)
          .map((b) => ({
            x: Date.parse(b.timestamp),
            y: m === 'rate' ? b.avgRate : m === 'latency' ? b.avgLatency : b.avgValue,
          })),
        borderColor: seriesColor(idx, dark),
        backgroundColor: seriesColor(idx, dark),
        borderWidth: 2,
        pointRadius: 0,
        pointHoverRadius: 4,
        tension: 0.25,
        spanGaps: false,
      }));
    }

    const source = forOid(override || s?.results || [], s);
    const allTargets = [...new Set(source.map((r) => r.target))];
    const targets = allTargets.slice(0, MAX_SERIES);
    hiddenSeriesNotice = allTargets.length - targets.length;

    const key = oid || s?.oid;
    return targets
      .map((target, idx) => ({ target, color: seriesColor(idx, dark) }))
      .filter(({ target }) => !hiddenSet.has(seriesKey(target, key)))
      .map(({ target, color }) => ({
        label: label(target),
        data: source
          .filter((r) => r.target === target)
          .map((r) => ({ x: Date.parse(r.timestamp), y: r[field] ?? null })),
        borderColor: color,
        backgroundColor: color,
        borderWidth: 2,
        pointRadius: 0,
        pointHoverRadius: 4,
        tension: 0.25,
        spanGaps: false,
      }));
  }

  // Contiguous stretches where EVERY target failed — shaded so an outage is
  // visible rather than silently skipped.
  function outageRanges(s, override) {
    const source = forOid(override || s?.results || [], s);
    const byTs = new Map();
    for (const r of source) {
      const t = Date.parse(r.timestamp);
      const cur = byTs.get(t) || { total: 0, failed: 0 };
      cur.total++;
      if (r.error || r.value === null) cur.failed++;
      byTs.set(t, cur);
    }
    const stamps = [...byTs.keys()].sort((a, b) => a - b);
    const ranges = [];
    let start = null;
    let prev = null;
    for (const t of stamps) {
      const { total, failed } = byTs.get(t);
      const down = total > 0 && failed === total;
      if (down && start === null) start = t;
      if (!down && start !== null) {
        ranges.push({ from: start, to: prev ?? t });
        start = null;
      }
      prev = t;
    }
    if (start !== null) ranges.push({ from: start, to: prev });
    return ranges;
  }

  // --- Custom plugins -------------------------------------------------------

  const crosshair = {
    id: 'snmplensCrosshair',
    afterDatasetsDraw(c) {
      const active = c.tooltip?.getActiveElements?.() || [];
      if (!active.length) return;
      const x = active[0].element.x;
      const { top, bottom } = c.chartArea;
      const ctx = c.ctx;
      ctx.save();
      ctx.beginPath();
      ctx.setLineDash([4, 4]);
      ctx.lineWidth = 1;
      ctx.strokeStyle = chartChrome().muted;
      ctx.moveTo(x, top);
      ctx.lineTo(x, bottom);
      ctx.stroke();
      ctx.restore();
    },
  };

  // Bounds are stated against the raw value, so they mean nothing on the derived
  // modes. In stacked mode each OID owns its own axis, so each band is drawn on
  // the scale it actually belongs to.
  function thresholdTargets() {
    if (mode !== 'raw') return [];
    const all = session?.thresholds || {};
    if (stacked) {
      return stackedAxes
        .map((a) => ({ scaleId: a.id, th: all[a.oid], label: oidName(a.oid, mibTree) }))
        .filter((x) => x.th);
    }
    const key = oid || session?.oid;
    return all[key] ? [{ scaleId: 'y', th: all[key], label: '' }] : [];
  }

  function makeBandsPlugin(getRanges, getThresholds, getMode) {
    return {
      id: 'snmplensBands',
      beforeDatasetsDraw(c) {
        const ctx = c.ctx;
        const { top, bottom, left, right } = c.chartArea;

        // Threshold bands: a red rule at each bound, with the out-of-band area
        // tinted so "outside" reads at a glance.
        for (const entry of getThresholds()) {
          const y = c.scales[entry.scaleId];
          if (!y) continue;
          const th = entry.th;

          const drawBound = (value, isMax) => {
            if (value === null || value === undefined || value === '') return;
            const yPix = y.getPixelForValue(Number(value));
            if (!Number.isFinite(yPix)) return;

            ctx.save();
            // Tint the side that is out of range.
            ctx.fillStyle = STATUS.critical + '1a';
            if (isMax && yPix > top) {
              ctx.fillRect(left, top, right - left, Math.min(yPix, bottom) - top);
            } else if (!isMax && yPix < bottom) {
              const y0 = Math.max(yPix, top);
              ctx.fillRect(left, y0, right - left, bottom - y0);
            }

            if (yPix >= top && yPix <= bottom) {
              ctx.beginPath();
              ctx.setLineDash([6, 4]);
              ctx.lineWidth = 1.5;
              ctx.strokeStyle = STATUS.critical;
              ctx.moveTo(left, yPix);
              ctx.lineTo(right, yPix);
              ctx.stroke();

              ctx.setLineDash([]);
              ctx.fillStyle = STATUS.critical;
              ctx.font = '10px system-ui, sans-serif';
              ctx.textBaseline = isMax ? 'bottom' : 'top';
              const dwell = th.forSeconds ? ' · ' + th.forSeconds + 's' : '';
              const prefix = entry.label ? entry.label + ' ' : '';
              ctx.fillText(
                prefix + (isMax ? 'max ' : 'min ') + unit.format(Number(value)) + dwell,
                left + 4,
                isMax ? yPix - 2 : yPix + 2
              );
            }
            ctx.restore();
          };

          drawBound(th.max, true);
          drawBound(th.min, false);
        }

        // Outage bands
        const x = c.scales.x;
        ctx.save();
        ctx.fillStyle = chartChrome().muted + '33';
        for (const r of getRanges()) {
          const x1 = x.getPixelForValue(r.from);
          const x2 = x.getPixelForValue(r.to);
          const a = Math.max(left, Math.min(x1, x2));
          const b = Math.min(right, Math.max(x1, x2));
          if (b > a) ctx.fillRect(a, top, Math.max(b - a, 2), bottom - top);
        }
        ctx.restore();
      },
    };
  }

  // --- Chart lifecycle ------------------------------------------------------

  // One y scale, or one per stacked OID: alternating sides, tinted like the
  // curve they belong to (that colour link is what keeps a multi-axis plot
  // readable), and only the first draws gridlines so they never overlap.
  function buildYScales(axes, chrome) {
    if (!axes || !axes.length) {
      return {
        y: {
          type: yScaleType,
          beginAtZero: zeroBased,
          title: { display: !!unit.label, text: unit.label, color: chrome.muted },
          ticks: { color: chrome.muted, callback: (v) => unit.format(v) },
          grid: { color: chrome.grid },
        },
      };
    }
    const scales = {};
    for (const a of axes) {
      scales[a.id] = {
        type: yScaleType,
        position: a.index % 2 === 0 ? 'left' : 'right',
        beginAtZero: zeroBased,
        title: {
          display: true,
          text: oidName(a.oid, mibTree) + (a.unit.label ? ' (' + a.unit.label + ')' : ''),
          color: a.color,
        },
        ticks: { color: a.color, callback: (v) => a.unit.format(v) },
        grid: { color: chrome.grid, drawOnChartArea: a.index === 0 },
      };
    }
    return scales;
  }

  function buildOptions(datasetCount, axes) {
    const chrome = chartChrome();
    return {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      // Required by the decimation plugin — and faster: we already hand
      // Chart.js sorted {x,y} pairs.
      parsing: false,
      normalized: true,
      interaction: { mode: 'index', intersect: false },
      scales: {
        x: {
          type: 'time',
          time: { tooltipFormat: 'dd/MM HH:mm:ss' },
          ticks: { color: chrome.muted, maxRotation: 0, autoSkipPadding: 20 },
          grid: { color: chrome.grid },
        },
        ...buildYScales(axes, chrome),
      },
      plugins: {
        // A single series names itself in the session header; a legend box only
        // earns its place once identity must be carried across several lines.
        legend: {
          display: datasetCount > 1,
          labels: { color: chrome.ink, boxWidth: 12, boxHeight: 12, usePointStyle: true },
        },
        tooltip: {
          callbacks: { label: (ctx) => ctx.dataset.label + ': ' + unit.format(ctx.parsed.y) },
        },
        // min-max keeps each bucket's extremes — a one-sample spike is exactly
        // what a supervision chart must never smooth away. Only engages past
        // ~4x the chart width in points, so live sessions are untouched.
        decimation: { enabled: true, algorithm: 'min-max' },
        zoom: {
          limits: { x: { minRange: 5000 } },
          pan: { enabled: true, mode: 'x', modifierKey: 'shift', onPanComplete: publishRange },
          zoom: {
            wheel: { enabled: true },
            pinch: { enabled: true },
            drag: { enabled: true, backgroundColor: chrome.muted + '22' },
            mode: 'x',
            onZoomComplete: publishRange,
          },
        },
      },
    };
  }

  function publishRange({ chart: c }) {
    emitRange();
    // Ignore zoom events we caused ourselves (resetZoom); only a real user
    // gesture hands control over.
    if (programmaticZoom) return;
    // The user drove the view: stop tracking the leading edge until they ask
    // for LIVE again, otherwise the next sample would yank the window away.
    if (!applyingSync) follow = false;
    if (!syncGroup || applyingSync) return;
    broadcastRange(syncGroup, syncId, c.scales.x.min, c.scales.x.max);
  }

  // Follow the group's window when a sibling chart drives it.
  const unsubscribeSync = chartSync.subscribe((state) => {
    if (!syncGroup || !chart) return;
    const entry = state[syncGroup];
    if (!entry || entry.sourceId === syncId) return;

    if (entry.live) {
      // A sibling went back to live: adopt its timebase and resume tracking.
      if (entry.windowMs !== undefined) windowMs = entry.windowMs;
      applyingSync = true;
      try {
        goLive(false);
      } finally {
        applyingSync = false;
      }
      return;
    }

    follow = false; // a sibling is driving the shared window
    const x = chart.scales.x;
    if (x.min === entry.min && x.max === entry.max) return;
    applyingSync = true;
    try {
      chart.zoomScale('x', { min: entry.min, max: entry.max }, 'none');
      emitRange();
    } finally {
      applyingSync = false;
    }
  });

  function create() {
    if (!canvas || chart) return;
    const parent = canvas.parentElement;
    if (!parent || parent.clientWidth === 0) return;
    let datasets;
    let axes = null;
    if (stacked) {
      const built = buildStacked(session, mode);
      datasets = built.datasets;
      axes = built.axes;
    } else {
      datasets = buildDatasets(session, mode, points, buckets);
    }
    stackedAxes = axes || [];
    chart = new Chart(canvas, {
      type: 'line',
      data: { datasets },
      options: buildOptions(datasets.length, axes),
      plugins: [
        crosshair,
        makeBandsPlugin(() => outageRanges(session, points), () => thresholdTargets(), () => mode),
      ],
    });
    applyWindow();
    chart.update('none');
    emitRange();
  }

  // Newest x across all series — the leading edge the live window tracks.
  function latestX() {
    let max = null;
    for (const ds of chart.data.datasets) {
      const arr = ds._data || ds.data;
      if (!arr || !arr.length) continue;
      const x = arr[arr.length - 1].x;
      if (x != null && (max === null || x > max)) max = x;
    }
    return max;
  }

  // Pin the visible window to the leading edge. Only while following: once the
  // user has zoomed or panned, the zoom plugin owns x.min/x.max and we must not
  // fight it.
  function applyWindow() {
    if (!chart || !follow || isHistorical) return;
    const x = chart.options.scales.x;
    if (!windowMs) {
      delete x.min;
      delete x.max;
      return;
    }
    const last = latestX();
    if (last == null) return;
    x.min = last - windowMs;
    x.max = last;
  }

  // Refresh WITHOUT replacing chart.options: the zoom plugin keeps its state in
  // there, so swapping the object wholesale on every new sample wiped the
  // user's zoom and collapsed the axis.
  function refresh() {
    if (!chart) {
      create();
      return;
    }
    let datasets;
    if (stacked) {
      const built = buildStacked(session, mode);
      datasets = built.datasets;
      stackedAxes = built.axes;
    } else {
      datasets = buildDatasets(session, mode, points, buckets);
    }
    chart.data.datasets = datasets;

    const chrome = chartChrome();
    if (stacked) {
      for (const a of stackedAxes) {
        const ax = chart.options.scales[a.id];
        if (!ax) continue;
        ax.type = yScaleType;
        ax.beginAtZero = zeroBased;
        ax.ticks.callback = (v) => a.unit.format(v);
      }
    } else {
      const y = chart.options.scales.y;
      y.type = yScaleType;
      y.beginAtZero = zeroBased;
      y.title.display = !!unit.label;
      y.title.text = unit.label;
      y.ticks.callback = (v) => unit.format(v);
      y.ticks.color = chrome.muted;
    }
    chart.options.plugins.legend.display = datasets.length > 1;
    chart.options.plugins.tooltip.callbacks.label = (ctx) =>
      ctx.dataset.label + ': ' + unit.format(ctx.parsed.y);

    applyWindow();
    chart.update('none');
    emitRange();
  }

  function goLive(propagate = true) {
    follow = true;
    if (chart) {
      // Drop any zoom state the plugin holds, then re-pin to the leading edge.
      programmaticZoom = true;
      try {
        chart.resetZoom('none');
      } catch (e) {
        /* no zoom applied yet */
      }
      programmaticZoom = false;
      applyWindow();
      chart.update('none');
      emitRange();
    }
    // Siblings of a small-multiple group follow the same timebase.
    if (propagate && syncGroup) broadcastLive(syncGroup, syncId, windowMs);
  }

  function setTimebase(ms) {
    windowMs = ms;
    goLive(); // changing the timebase re-arms live tracking, like a scope
  }

  // Re-render when any input shaping the chart changes. Everything read here is
  // referenced explicitly so Svelte actually tracks it.
  $: if (chart && (session || mode || points || buckets || yScaleType || zeroBased || labels || mibTree)) refresh();

  // Adding or removing an axis changes the chart's structure, which cannot be
  // patched in place — rebuild when the stacked OID set changes.
  let lastStructure = null;
  $: structure = (stacked ? stacked.join('|') : 'single') + '::' + mode;
  $: if (chart && structure !== lastStructure) {
    lastStructure = structure;
    chart.destroy();
    chart = null;
    tick().then(create);
  }

  // A theme switch changes every colour: rebuild from scratch.
  let lastTheme = theme;
  $: if (theme !== lastTheme) {
    lastTheme = theme;
    if (chart) {
      chart.destroy();
      chart = null;
      tick().then(create);
    }
  }

  onMount(() => {
    requestAnimationFrame(create);
  });

  onDestroy(() => {
    unsubscribeSync();
    if (chart) chart.destroy();
    chart = null;
  });

  // --- Toolbar actions ------------------------------------------------------

  function resetZoom() {
    goLive();
  }

  async function toggleFullscreen() {
    fullscreen = !fullscreen;
    await tick();
    chart?.resize();
  }

  function exportPng() {
    if (!chart) return;
    const url = chart.toBase64Image('image/png', 1);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'snmp-monitor-' + mode + '-' + new Date().toISOString().slice(0, 19).replace(/:/g, '-') + '.png';
    a.click();
    notificationStore.add(get(_)('monitor.pngExported'), 'success');
  }

  // Export exactly what is on screen: the visible x-window after zoom/pan.
  function exportVisibleCsv() {
    if (!chart) return;
    const x = chart.scales.x;
    const from = x.min;
    const to = x.max;
    const rows = [['timestamp', 'target', mode, 'unit'].join(',')];
    for (const ds of chart.data.datasets) {
      const source = ds._data || ds.data;
      for (const pt of source) {
        if (pt.x < from || pt.x > to) continue;
        rows.push([new Date(pt.x).toISOString(), ds.label, pt.y ?? '', unit.label].join(','));
      }
    }
    downloadFile(rows.join('\n'), 'snmp-monitor-' + mode + '.csv', 'text/csv');
    notificationStore.add(get(_)('monitor.csvExported'), 'success');
  }

  const MIN_H = 160;
  const MAX_H = 900;

  function startResize(event) {
    const startY = event.clientY;
    const startH = height;
    const move = (ev) => {
      height = Math.max(MIN_H, Math.min(MAX_H, startH + ev.clientY - startY));
      chart?.resize();
    };
    const up = () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
  }

  // Same bounds from the keyboard, for the reason given on the MIB panel's
  // handle: a divider that only a mouse can move is not a control.
  function resizeKey(event) {
    const step = event.shiftKey ? 40 : 10;
    let h = height;
    if (event.key === 'ArrowUp') h -= step;
    else if (event.key === 'ArrowDown') h += step;
    else if (event.key === 'Home') h = MIN_H;
    else if (event.key === 'End') h = MAX_H;
    else return;
    event.preventDefault();
    height = Math.max(MIN_H, Math.min(MAX_H, h));
    chart?.resize();
  }
</script>

<div class="chart-shell" class:fullscreen>
  <div class="chart-toolbar">
    {#if !isHistorical}
      <button class="ctl live" class:on={follow} on:click={goLive} title={$_('monitor.liveHint')}>
        <span class="dot" class:beating={follow}></span> {$_('monitor.live')}
      </button>
      <div class="ctl-group">
        {#each TIMEBASES as tb}
          <button
            class="ctl"
            class:on={windowMs === tb}
            on:click={() => setTimebase(tb)}
          >{tb === 0 ? $_('monitor.timebaseAll') : (tb < 3600000 ? tb / 60000 + ' min' : tb / 3600000 + ' h')}</button>
        {/each}
      </div>
    {/if}
    <button class="ctl" on:click={resetZoom} title={$_('monitor.resetZoom')}>
      <Icon name="refresh-cw" size={13} /> {$_('monitor.resetZoom')}
    </button>
    <div class="ctl-group">
      <button class="ctl" class:on={yScaleType === 'linear'} on:click={() => (yScaleType = 'linear')}>{$_('monitor.scaleLinear')}</button>
      <button class="ctl" class:on={yScaleType === 'logarithmic'} on:click={() => (yScaleType = 'logarithmic')}>{$_('monitor.scaleLog')}</button>
    </div>
    <button class="ctl" class:on={zeroBased} on:click={() => (zeroBased = !zeroBased)} title={$_('monitor.zeroBasedHint')}>
      {$_('monitor.zeroBased')}
    </button>
    <span class="spacer"></span>
    <button class="ctl" on:click={exportPng} title="PNG"><Icon name="download" size={13} /> PNG</button>
    <button class="ctl" on:click={exportVisibleCsv} title={$_('monitor.csvVisible')}><Icon name="download" size={13} /> CSV</button>
    <button class="ctl" on:click={toggleFullscreen} title={$_('monitor.fullscreen')}>
      <Icon name={fullscreen ? 'minimize' : 'maximize'} size={13} />
    </button>
  </div>

  <div class="chart-area" style="height: {fullscreen ? 'calc(100vh - 170px)' : height + 'px'}">
    <canvas bind:this={canvas}></canvas>
  </div>

  <div class="chart-footnote">
    {#if !follow && !isHistorical}
      <span class="browsing"><Icon name="search" size={11} /> {$_('monitor.browsing')}</span>
    {/if}
    <span>{$_('monitor.zoomHint')}</span>
    {#if hiddenSeriesNotice > 0}
      <span class="warn">{$_('monitor.seriesCapped', { values: { count: hiddenSeriesNotice, max: MAX_SERIES } })}</span>
    {/if}
  </div>

  {#if !fullscreen}
    <div
      class="resize-handle"
      role="slider"
      tabindex="0"
      aria-orientation="horizontal"
      aria-valuenow={height}
      aria-valuemin={MIN_H}
      aria-valuemax={MAX_H}
      aria-label={$_('monitor.resizeChart')}
      on:mousedown={startResize}
      on:keydown={resizeKey}
    ></div>
  {/if}
</div>

<style>
  .chart-shell {
    position: relative;
    border-top: 1px solid var(--border-color);
  }

  .chart-shell.fullscreen {
    position: fixed;
    inset: 0;
    z-index: 1200;
    background-color: var(--bg-light-color);
    padding: 16px;
    overflow: auto;
  }

  .chart-toolbar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    padding: 8px 12px 4px;
  }

  .ctl-group {
    display: inline-flex;
  }

  .ctl-group .ctl:first-child {
    border-radius: 4px 0 0 4px;
  }

  .ctl-group .ctl:last-child {
    border-radius: 0 4px 4px 0;
    margin-left: -1px;
  }

  .ctl {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 9px;
    font-size: 0.78em;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-light);
    cursor: pointer;
  }

  .ctl:hover {
    border-color: var(--accent-color);
    color: var(--text-color);
  }

  .ctl.on {
    color: var(--accent-color);
    border-color: var(--accent-border);
    background-color: var(--accent-subtle);
    font-weight: 600;
  }

  .spacer {
    flex: 1;
  }

  .ctl.live .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background-color: var(--text-muted);
    flex-shrink: 0;
  }

  .ctl.live.on .dot {
    background-color: var(--success-color);
  }

  .ctl.live.on .dot.beating {
    animation: live-pulse 1.6s ease-in-out infinite;
  }

  @keyframes live-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.35; }
  }

  .browsing {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--accent-color);
    font-weight: 600;
  }

  .chart-area {
    padding: 0 12px;
    position: relative;
  }

  .chart-area canvas {
    display: block;
    width: 100% !important;
    height: 100% !important;
  }

  .chart-footnote {
    display: flex;
    gap: 10px;
    padding: 4px 12px 8px;
    font-size: 0.75em;
    color: var(--text-muted);
  }

  .chart-footnote .warn {
    color: var(--warning-color);
  }

  .resize-handle {
    height: 6px;
    cursor: ns-resize;
    background: transparent;
  }

  .resize-handle:hover {
    background-color: var(--accent-subtle);
  }
</style>
