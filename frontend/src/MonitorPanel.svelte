<script>
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';
  import { onDestroy, afterUpdate, tick } from 'svelte';
  import { Chart, registerables } from 'chart.js';
  import 'chartjs-adapter-date-fns';
  import { MonitorGetStats, MonitorLoadHistoricalData } from '../wailsjs/go/main/App';
  import { pollingStore } from './stores/pollingStore';
  import { settingsStore } from './stores/settingsStore';
  import { notificationStore } from './stores/notifications';
  import { getTargetsAsArray } from './utils/targets';
  import { formatTimeShort } from './utils/formatting';
  import { anonMode, anonymizeIp } from './utils/anonymize';
  import { copyToClipboard } from './utils/clipboard';

  Chart.register(...registerables);

  // Read CSS variables once for Chart.js (which can't use var() directly)
  function getCssVar(name) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  }

  // Destroy and recreate charts only when the THEME actually changes
  let lastTheme = $settingsStore.theme;
  $: {
    const currentTheme = $settingsStore.theme;
    if (currentTheme !== lastTheme) {
      lastTheme = currentTheme;
      for (const id of Object.keys(charts)) {
        charts[id].destroy();
        delete charts[id];
      }
    }
  }

  let pollOid = '';
  let pollInterval = 5000;
  let pollVersion = 'v2c'; // 'v1', 'v2c', 'v3'
  let charts = {};
  let canvasElements = {};
  let viewModes = {}; // sessionId -> 'raw' | 'delta' | 'rate' | 'latency'
  let displayModes = {}; // sessionId -> 'graph' | 'table'
  let sessionStats = {};    // sessionId -> stats object
  let showStats = {};       // sessionId -> boolean
  let historyFrom = {};     // sessionId -> datetime string
  let historyTo = {};       // sessionId -> datetime string
  let loadingHistory = {};  // sessionId -> boolean
  let historicalResults = {}; // sessionId -> DataPoint[]

  // Threshold form state
  let thresholdEnabled = false;
  let thresholdMin = '';
  let thresholdMax = '';

  function handleStartPolling() {
    const t = get(_);
    if (!pollOid.trim()) {
      notificationStore.add(t('monitor.enterOid'), 'error');
      return;
    }
    const targets = getTargetsAsArray($settingsStore.targets);
    if (targets.length === 0) {
      notificationStore.add(t('monitor.configureTarget'), 'error');
      return;
    }
    const thresholds = thresholdEnabled ? {
      min: thresholdMin !== '' ? Number(thresholdMin) : null,
      max: thresholdMax !== '' ? Number(thresholdMax) : null,
      alertEnabled: true,
    } : null;
    const id = pollingStore.startPolling(pollOid.trim(), targets, pollInterval, thresholds, pollVersion);
    viewModes[id] = 'raw';
    displayModes[id] = 'graph';
    notificationStore.add(t('monitor.pollingStarted', { values: { oid: pollOid, version: pollVersion, interval: pollInterval/1000, thresholds: thresholds ? t('monitor.withThresholds') : '' } }), 'success');
  }

  function handleStop(id) {
    pollingStore.stopPolling(id);
  }

  function handleResume(id) {
    pollingStore.resumeSession(id);
  }

  function handleRemove(id) {
    if (charts[id]) {
      charts[id].destroy();
      delete charts[id];
    }
    delete viewModes[id];
    delete displayModes[id];
    pollingStore.removeSession(id);
  }

  function handleStopAll() {
    const t = get(_);
    pollingStore.stopAll();
    notificationStore.add(t('monitor.allStopped'), 'info');
  }

  function setViewMode(sessionId, mode) {
    viewModes[sessionId] = mode;
    viewModes = viewModes; // trigger reactivity
  }

  function setDisplayMode(sessionId, mode) {
    displayModes[sessionId] = mode;
    displayModes = displayModes; // trigger reactivity
  }

  async function loadStats(sessionId) {
    try {
      sessionStats[sessionId] = await MonitorGetStats(sessionId);
      sessionStats = sessionStats;
    } catch (e) {
      console.warn('Failed to load stats:', e);
    }
  }

  async function toggleStats(sessionId) {
    showStats[sessionId] = !showStats[sessionId];
    showStats = showStats;
    if (showStats[sessionId] && !sessionStats[sessionId]) {
      await loadStats(sessionId);
    }
  }

  async function loadHistorical(sessionId) {
    const from = historyFrom[sessionId];
    const to = historyTo[sessionId];
    if (!from || !to) return;
    loadingHistory[sessionId] = true;
    loadingHistory = loadingHistory;
    try {
      const points = await MonitorLoadHistoricalData(sessionId, new Date(from).toISOString(), new Date(to).toISOString());
      if (!points || points.length === 0) {
        const t = get(_);
        notificationStore.add(t('monitor.noHistoricalData'), 'info');
        return;
      }
      historicalResults[sessionId] = points;
      historicalResults = historicalResults;
      const t = get(_);
      notificationStore.add(t('monitor.historicalLoaded', { values: { count: points.length } }), 'success');
    } catch (e) {
      console.warn('Failed to load historical data:', e);
    } finally {
      loadingHistory[sessionId] = false;
      loadingHistory = loadingHistory;
    }
  }

  // Build chart data from session results
  function getChartData(session, mode) {
    const targets = [...new Set(session.results.map(r => r.target))];
    const colors = [
      getCssVar('--accent-color'), getCssVar('--success-color'), getCssVar('--warning-color'),
      '#e91e63', '#9c27b0', '#00bcd4', '#ff5722', '#795548',
    ];

    const datasets = targets.map((target, idx) => {
      const points = session.results.filter(r => r.target === target);
      return {
        label: get(anonMode) ? anonymizeIp(target) : target,
        data: points.map(p => ({
          x: new Date(p.timestamp),
          y: mode === 'delta' ? p.delta : mode === 'rate' ? p.rate : mode === 'latency' ? p.responseTimeMs : p.value,
        })).filter(p => p.y !== null),
        borderColor: colors[idx % colors.length],
        backgroundColor: colors[idx % colors.length] + '33',
        fill: false,
        tension: 0.3,
        pointRadius: 2,
        borderWidth: 2,
      };
    });

    return { datasets };
  }

  // Build threshold line plugin for a session
  function buildThresholdPlugin(session, mode) {
    return {
      id: 'thresholdLines_' + session.id,
      afterDraw(chart) {
        if (!session.thresholds || mode !== 'raw') return;
        const { min, max } = session.thresholds;
        const yScale = chart.scales.y;
        const ctx = chart.ctx;
        ctx.save();
        ctx.setLineDash([6, 4]);
        ctx.lineWidth = 1.5;

        if (min !== null && min !== undefined) {
          const yPos = yScale.getPixelForValue(Number(min));
          if (yPos >= yScale.top && yPos <= yScale.bottom) {
            ctx.strokeStyle = getCssVar('--warning-color');
            ctx.beginPath();
            ctx.moveTo(chart.chartArea.left, yPos);
            ctx.lineTo(chart.chartArea.right, yPos);
            ctx.stroke();
            ctx.fillStyle = getCssVar('--warning-color');
            ctx.font = '10px sans-serif';
            ctx.fillText(`min: ${min}`, chart.chartArea.left + 4, yPos - 4);
          }
        }
        if (max !== null && max !== undefined) {
          const yPos = yScale.getPixelForValue(Number(max));
          if (yPos >= yScale.top && yPos <= yScale.bottom) {
            ctx.strokeStyle = getCssVar('--error-color');
            ctx.beginPath();
            ctx.moveTo(chart.chartArea.left, yPos);
            ctx.lineTo(chart.chartArea.right, yPos);
            ctx.stroke();
            ctx.fillStyle = getCssVar('--error-color');
            ctx.font = '10px sans-serif';
            ctx.fillText(`max: ${max}`, chart.chartArea.left + 4, yPos - 4);
          }
        }
        ctx.restore();
      }
    };
  }

  // Create a new Chart.js instance on a canvas
  function createChart(canvas, data, mode, session) {
    // Ensure canvas has actual dimensions (Chart.js needs them for responsive mode)
    const container = canvas.parentElement;
    if (!container || container.clientWidth === 0 || container.clientHeight === 0) {
      console.warn('Chart container has no dimensions, deferring creation');
      return null;
    }

    try {
      return new Chart(canvas, {
        type: 'line',
        data,
        options: {
          responsive: true,
          maintainAspectRatio: false,
          animation: false,
          scales: {
            x: {
              type: 'time',
              time: { tooltipFormat: 'HH:mm:ss' },
              title: { display: true, text: get(_)('monitor.tableTime'), color: getCssVar('--text-muted') },
              ticks: { color: getCssVar('--text-muted') },
              grid: { color: getCssVar('--bg-lighter-color') },
            },
            y: {
              title: { display: true, text: get(_)('monitor.chartValue'), color: getCssVar('--text-muted') },
              ticks: { color: getCssVar('--text-muted') },
              grid: { color: getCssVar('--bg-lighter-color') },
            }
          },
          plugins: {
            legend: {
              labels: { color: getCssVar('--text-light') },
            }
          }
        },
        plugins: [buildThresholdPlugin(session, mode)],
      });
    } catch (e) {
      console.error('Failed to create chart:', e);
      return null;
    }
  }

  // Pending chart creations deferred to next frame
  let pendingChartCreations = new Set();

  // Update or create charts after data changes
  afterUpdate(() => {
    for (const session of $pollingStore) {
      // Skip chart creation/update if in table mode
      if (displayModes[session.id] === 'table') {
        if (charts[session.id]) {
          charts[session.id].destroy();
          delete charts[session.id];
        }
        continue;
      }

      const canvas = canvasElements[session.id];
      if (!canvas) continue;

      const mode = viewModes[session.id] || 'raw';
      const data = getChartData(session, mode);

      if (charts[session.id]) {
        // Update existing chart — mutate datasets in place for reliable Chart.js update
        try {
          const chart = charts[session.id];
          chart.data.datasets = data.datasets;
          chart.options.scales.y.title.text = mode === 'rate' ? get(_)('monitor.chartRate') : mode === 'delta' ? get(_)('monitor.chartDelta') : mode === 'latency' ? get(_)('monitor.chartLatency') : get(_)('monitor.chartValue');
          chart.update('none');
        } catch (e) {
          console.error('Failed to update chart:', e);
          // Destroy broken chart so it gets recreated
          charts[session.id].destroy();
          delete charts[session.id];
        }
      } else if (!pendingChartCreations.has(session.id)) {
        // Defer initial chart creation to next animation frame so the canvas
        // has had a full layout pass and has real dimensions.
        pendingChartCreations.add(session.id);
        const sessionSnapshot = { ...session };
        requestAnimationFrame(() => {
          pendingChartCreations.delete(sessionSnapshot.id);
          const c = canvasElements[sessionSnapshot.id];
          if (!c || charts[sessionSnapshot.id] || displayModes[sessionSnapshot.id] === 'table') return;
          const currentMode = viewModes[sessionSnapshot.id] || 'raw';
          // Re-read latest session data from the store
          const currentSessions = get(pollingStore);
          const latestSession = currentSessions.find(s => s.id === sessionSnapshot.id);
          const latestData = latestSession ? getChartData(latestSession, currentMode) : data;
          charts[sessionSnapshot.id] = createChart(c, latestData, currentMode, latestSession || sessionSnapshot);
        });
      }
    }

    // Cleanup charts for removed sessions
    const activeIds = new Set($pollingStore.map(s => s.id));
    for (const id of Object.keys(charts)) {
      if (!activeIds.has(id)) {
        charts[id].destroy();
        delete charts[id];
      }
    }
  });

  onDestroy(() => {
    for (const chart of Object.values(charts)) {
      chart.destroy();
    }
    charts = {};
  });
</script>

<div class="panel">
  <div class="setup-form">
    <div class="form-group">
      <label for="poll-oid">{$_('monitor.oidLabel')}</label>
      <input id="poll-oid" type="text" bind:value={pollOid} placeholder={$_('monitor.oidPlaceholder')} />
    </div>
    <div class="form-row">
      <div class="form-group compact">
        <label for="poll-version">{$_('monitor.versionLabel')}</label>
        <select id="poll-version" bind:value={pollVersion}>
          <option value="v1">v1</option>
          <option value="v2c">v2c</option>
          <option value="v3">v3</option>
        </select>
      </div>
      <div class="form-group compact">
        <label for="poll-interval">{$_('monitor.intervalLabel')}</label>
        <select id="poll-interval" bind:value={pollInterval}>
          <option value={1000}>1s</option>
          <option value={5000}>5s</option>
          <option value={10000}>10s</option>
          <option value={30000}>30s</option>
          <option value={60000}>60s</option>
        </select>
      </div>
      <button class="btn btn-primary" on:click={handleStartPolling}>{$_('monitor.startPolling')}</button>
      {#if $pollingStore.some(s => s.running)}
        <button class="btn btn-danger" on:click={handleStopAll}>{$_('monitor.stopAll')}</button>
      {/if}
    </div>
    <div class="threshold-section">
      <label class="toggle-label">
        <input type="checkbox" bind:checked={thresholdEnabled} />
        <span>{$_('monitor.enableThresholds')}</span>
      </label>
      {#if thresholdEnabled}
        <div class="threshold-inputs">
          <div class="form-group compact">
            <label for="threshold-min">{$_('monitor.minLabel')}</label>
            <input id="threshold-min" type="number" bind:value={thresholdMin} />
          </div>
          <div class="form-group compact">
            <label for="threshold-max">{$_('monitor.maxLabel')}</label>
            <input id="threshold-max" type="number" bind:value={thresholdMax} />
          </div>
        </div>
        <div class="notification-options">
          <label class="toggle-label">
            <input
              type="checkbox"
              checked={$settingsStore.monitor?.systemNotifications}
              on:change={(e) => {
                const enabled = e.target.checked;
                settingsStore.save({
                  ...$settingsStore,
                  monitor: { ...$settingsStore.monitor, systemNotifications: enabled }
                });
              }}
            />
            <span>{$_('monitor.systemNotifications')}</span>
          </label>
          <label class="toggle-label">
            <input
              type="checkbox"
              checked={$settingsStore.monitor?.alertSound}
              on:change={(e) => settingsStore.save({
                ...$settingsStore,
                monitor: { ...$settingsStore.monitor, alertSound: e.target.checked }
              })}
            />
            <span>{$_('monitor.alertSound')}</span>
          </label>
        </div>
      {/if}
    </div>
  </div>

  {#if $pollingStore.length === 0}
    <div class="empty-state">
      <p>{$_('monitor.empty')}</p>
      <p class="hint">{$_('monitor.emptyHint')}</p>
    </div>
  {:else}
    <div class="sessions">
      {#each $pollingStore as session (session.id)}
        <div class="session-card">
          <div class="session-header">
            <div class="session-info">
              <span class="session-oid">{session.oid}</span>
              <button class="btn-copy-small" on:click|stopPropagation={() => copyToClipboard(session.oid, 'OID')} title="Copy OID">📋</button>
              <span class="session-status" class:running={session.running}>
                {session.running ? $_('monitor.running') : $_('monitor.stopped')}
              </span>
              <span class="session-meta">{session.snmpVersion} / {session.targets.length} target(s) / {session.interval/1000}s</span>
              <span class="session-meta">{session.results.length} data point(s)</span>
              {#if session.thresholds}
                <span class="threshold-badge" title="Thresholds: min={session.thresholds.min ?? 'none'}, max={session.thresholds.max ?? 'none'}">
                  {$_('monitor.thresholdsBadge')}
                </span>
              {/if}
            </div>
            <div class="session-actions">
              <div class="display-mode-toggle">
                <button class="btn-mode" class:active={displayModes[session.id] !== 'table'} on:click={() => setDisplayMode(session.id, 'graph')}>{$_('monitor.graph')}</button>
                <button class="btn-mode" class:active={displayModes[session.id] === 'table'} on:click={() => setDisplayMode(session.id, 'table')}>{$_('monitor.table')}</button>
              </div>
              {#if displayModes[session.id] !== 'table'}
                <div class="view-mode-toggle">
                  <button class="btn-mode" class:active={viewModes[session.id] === 'raw'} on:click={() => setViewMode(session.id, 'raw')}>{$_('monitor.raw')}</button>
                  <button class="btn-mode" class:active={viewModes[session.id] === 'delta'} on:click={() => setViewMode(session.id, 'delta')}>{$_('monitor.delta')}</button>
                  <button class="btn-mode" class:active={viewModes[session.id] === 'rate'} on:click={() => setViewMode(session.id, 'rate')}>{$_('monitor.rate')}</button>
                  <button class="btn-mode" class:active={viewModes[session.id] === 'latency'} on:click={() => setViewMode(session.id, 'latency')}>{$_('monitor.latency')}</button>
                </div>
              {/if}
              {#if session.running}
                <button class="btn btn-small" on:click={() => handleStop(session.id)}>{$_('common.stop')}</button>
              {:else}
                <button class="btn btn-small" on:click={() => handleResume(session.id)}>{$_('monitor.resume')}</button>
              {/if}
              <button class="btn btn-small btn-danger" on:click={() => handleRemove(session.id)}>{$_('monitor.remove')}</button>
              <button class="btn btn-small" on:click={() => toggleStats(session.id)}>
                {$_('monitor.stats')}
              </button>
            </div>
          </div>
          {#if showStats[session.id] && sessionStats[session.id]}
            <div class="stats-panel">
              <div class="stats-grid">
                <div class="stat-item">
                  <span class="stat-label">{$_('monitor.statsTotalPoints')}</span>
                  <span class="stat-value">{sessionStats[session.id].totalPoints}</span>
                </div>
                <div class="stat-item">
                  <span class="stat-label">{$_('monitor.statsDateRange')}</span>
                  <span class="stat-value">{sessionStats[session.id].firstTimestamp?.slice(0,16)} — {sessionStats[session.id].lastTimestamp?.slice(0,16)}</span>
                </div>
                <div class="stat-item">
                  <span class="stat-label">{$_('monitor.statsMinValue')}</span>
                  <span class="stat-value">{sessionStats[session.id].minValue ?? '—'}</span>
                </div>
                <div class="stat-item">
                  <span class="stat-label">{$_('monitor.statsMaxValue')}</span>
                  <span class="stat-value">{sessionStats[session.id].maxValue ?? '—'}</span>
                </div>
                <div class="stat-item">
                  <span class="stat-label">{$_('monitor.statsAvgValue')}</span>
                  <span class="stat-value">{sessionStats[session.id].avgValue != null ? sessionStats[session.id].avgValue.toFixed(2) : '—'}</span>
                </div>
                <div class="stat-item">
                  <span class="stat-label">{$_('monitor.statsAvgLatency')}</span>
                  <span class="stat-value">{sessionStats[session.id].avgLatency != null ? sessionStats[session.id].avgLatency.toFixed(1) + ' ms' : '—'}</span>
                </div>
                <div class="stat-item">
                  <span class="stat-label">{$_('monitor.statsErrorCount')}</span>
                  <span class="stat-value">{sessionStats[session.id].errorCount}</span>
                </div>
              </div>
            </div>
          {/if}
          <div class="history-controls">
            <input type="datetime-local" bind:value={historyFrom[session.id]} />
            <span>→</span>
            <input type="datetime-local" bind:value={historyTo[session.id]} />
            <button class="btn btn-small" on:click={() => loadHistorical(session.id)} disabled={loadingHistory[session.id]}>
              {loadingHistory[session.id] ? '...' : $_('monitor.loadHistory')}
            </button>
          </div>
          {#if historicalResults[session.id] && historicalResults[session.id].length > 0}
            <div class="table-container">
              <div class="historical-header">{$_('monitor.historicalData')} ({historicalResults[session.id].length} points)</div>
              <table class="data-table">
                <thead>
                  <tr>
                    <th>{$_('monitor.tableTime')}</th>
                    <th>{$_('monitor.tableTarget')}</th>
                    <th>{$_('monitor.tableValue')}</th>
                    <th>{$_('monitor.tableDelta')}</th>
                    <th>{$_('monitor.tableRate')}</th>
                    <th>{$_('monitor.tableLatency')}</th>
                    <th>{$_('monitor.tableError')}</th>
                  </tr>
                </thead>
                <tbody>
                  {#each historicalResults[session.id] as point}
                    <tr class:error-row={point.error}>
                      <td class="mono">{formatTimeShort(point.timestamp)}</td>
                      <td>{$anonMode ? anonymizeIp(point.target) : point.target}</td>
                      <td class="mono">
                        {point.value !== null ? point.value : '-'}
                        {#if point.value !== null}
                          <button class="btn-copy-small" on:click={() => copyToClipboard(String(point.value), $_('monitor.tableValue'))} title={$_('monitor.tableValue')}>📋</button>
                        {/if}
                      </td>
                      <td class="mono">{point.delta !== null ? point.delta : '-'}</td>
                      <td class="mono">{point.rate !== null ? point.rate.toFixed(2) : '-'}</td>
                      <td class="mono">{point.responseTimeMs}</td>
                      <td class="error-cell">{point.error || ''}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
          {#if displayModes[session.id] === 'table'}
            <div class="table-container">
              <table class="data-table">
                <thead>
                  <tr>
                    <th>{$_('monitor.tableTime')}</th>
                    <th>{$_('monitor.tableTarget')}</th>
                    <th>{$_('monitor.tableValue')}</th>
                    <th>{$_('monitor.tableDelta')}</th>
                    <th>{$_('monitor.tableRate')}</th>
                    <th>{$_('monitor.tableLatency')}</th>
                    <th>{$_('monitor.tableError')}</th>
                  </tr>
                </thead>
                <tbody>
                  {#each [...session.results].reverse().slice(0, 100) as point}
                    <tr class:error-row={point.error}>
                      <td class="mono">{formatTimeShort(point.timestamp)}</td>
                      <td>{$anonMode ? anonymizeIp(point.target) : point.target}</td>
                      <td class="mono">
                        {point.value !== null ? point.value : '-'}
                        {#if point.value !== null}
                          <button class="btn-copy-small" on:click={() => copyToClipboard(String(point.value), $_('monitor.tableValue'))} title={$_('monitor.tableValue')}>📋</button>
                        {/if}
                      </td>
                      <td class="mono">{point.delta !== null ? point.delta : '-'}</td>
                      <td class="mono">{point.rate !== null ? point.rate.toFixed(2) : '-'}</td>
                      <td class="mono">{point.responseTimeMs}</td>
                      <td class="error-cell">{point.error || ''}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
              {#if session.results.length > 100}
                <div class="table-info">{$_('monitor.showingLast', { values: { count: session.results.length } })}</div>
              {/if}
            </div>
          {:else}
            <div class="chart-container">
              <canvas bind:this={canvasElements[session.id]}></canvas>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .setup-form {
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 15px;
    margin-bottom: 15px;
  }

  .form-row {
    display: flex;
    gap: 10px;
    align-items: center;
  }

  .form-group.compact {
    margin-bottom: 0;
    flex: 0 0 auto;
  }

  .form-group.compact select {
    width: 80px;
  }

  .empty-state {
    text-align: center;
    padding: 40px 20px;
    color: var(--text-muted);
  }

  .hint { font-size: 0.9em; font-style: italic; }

  .sessions {
    display: flex;
    flex-direction: column;
    gap: 15px;
  }

  .session-card {
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
    background-color: var(--bg-lighter-color);
  }

  .session-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 15px;
    background-color: var(--shadow-color);
    border-bottom: 1px solid var(--border-color);
    flex-wrap: wrap;
    gap: 8px;
  }

  .session-info {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }

  .session-oid {
    font-family: 'Courier New', monospace;
    font-weight: 600;
    color: var(--oid-color);
  }

  .session-status {
    padding: 2px 8px;
    border-radius: 3px;
    font-size: 0.8em;
    font-weight: 600;
    background-color: var(--error-subtle-strong);
    color: var(--error-color);
  }

  .session-status.running {
    background-color: var(--success-subtle-strong);
    color: var(--success-color);
    animation: pulse 2s infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.6; }
  }

  .session-meta {
    font-size: 0.85em;
    color: var(--text-muted);
  }

  .session-actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .view-mode-toggle {
    display: flex;
    gap: 2px;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    overflow: hidden;
  }

  .btn-mode {
    padding: 4px 10px;
    font-size: 0.8em;
    background-color: transparent;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.2s;
  }

  .btn-mode:hover { background-color: var(--hover-overlay); color: var(--text-color); }
  .btn-mode.active { background-color: var(--accent-color); color: white; }

  .chart-container {
    height: 250px;
    padding: 10px 15px;
    position: relative;
  }

  .chart-container canvas {
    display: block;
    width: 100% !important;
    height: 100% !important;
  }

  /* Threshold UI */
  .threshold-section {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border-color);
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 0.9em;
    user-select: none;
  }

  .toggle-label input[type="checkbox"] {
    width: 16px;
    height: 16px;
    accent-color: var(--accent-color);
    cursor: pointer;
  }

  .threshold-inputs {
    display: flex;
    gap: 15px;
    margin-top: 8px;
  }

  .threshold-inputs .form-group {
    flex: 1;
  }

  .threshold-inputs input[type="number"] {
    width: 100px;
  }

  .threshold-badge {
    padding: 2px 8px;
    border-radius: 3px;
    font-size: 0.75em;
    font-weight: 600;
    background-color: var(--warning-subtle);
    color: var(--warning-color);
  }

  .notification-options {
    margin-top: 8px;
    display: flex;
    gap: 16px;
    flex-wrap: wrap;
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 0.85em;
    color: var(--text-dimmed);
    cursor: pointer;
    user-select: none;
  }

  .toggle-label input[type="checkbox"] {
    accent-color: var(--accent-color);
    cursor: pointer;
  }

  .display-mode-toggle {
    display: flex;
    gap: 2px;
    border: 1px solid var(--accent-color);
    border-radius: 4px;
    overflow: hidden;
  }

  .table-container {
    max-height: 300px;
    overflow-y: auto;
    padding: 10px 15px;
  }

  .data-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85em;
  }

  .data-table th,
  .data-table td {
    padding: 6px 10px;
    text-align: left;
    border-bottom: 1px solid var(--border-color);
  }

  .data-table th {
    background-color: var(--shadow-color-strong);
    font-weight: 600;
    position: sticky;
    top: 0;
    z-index: 1;
  }

  .data-table tbody tr:hover {
    background-color: var(--hover-overlay);
  }

  .data-table .mono {
    font-family: 'Courier New', monospace;
  }

  .data-table .error-row {
    background-color: var(--error-subtle-medium);
  }

  .data-table .error-cell {
    color: var(--error-color);
    font-size: 0.85em;
  }

  .table-info {
    text-align: center;
    padding: 8px;
    font-size: 0.85em;
    color: var(--text-muted);
    font-style: italic;
  }

  .stats-panel {
    padding: 10px 15px;
    background-color: var(--bg-color);
    border-radius: 4px;
    margin-top: 8px;
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 8px;
  }

  .stat-item {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .stat-label {
    font-size: 0.75em;
    color: var(--text-muted);
    font-weight: 500;
  }

  .stat-value {
    font-size: 0.9em;
    font-weight: 600;
    color: var(--text-color);
    font-family: 'Courier New', monospace;
  }

  .history-controls {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 15px;
    font-size: 0.85em;
  }

  .history-controls input[type="datetime-local"] {
    padding: 4px 8px;
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-color);
    font-size: 0.9em;
  }

  .history-controls span {
    color: var(--text-muted);
  }

  .historical-header {
    font-size: 0.85em;
    font-weight: 600;
    color: var(--text-muted);
    padding: 4px 0 8px;
    border-bottom: 1px solid var(--border-color);
    margin-bottom: 4px;
  }
</style>
