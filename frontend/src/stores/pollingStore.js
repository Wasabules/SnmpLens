import { writable, get } from 'svelte/store';
import { _ } from 'svelte-i18n';
import {
  MonitorCreateSession,
  MonitorStart,
  MonitorStop,
  MonitorRunning,
  MonitorSaveDataPoints,
  MonitorLoadSessions,
  MonitorLoadSessionData,
  MonitorDeleteSession,
} from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { settingsStore } from './settingsStore';
import { notificationStore } from './notifications';
import { buildMonitorConnection } from '../utils/snmpParams';

// In-memory scope buffer, per (target x OID) series. The chart draws a sliding
// window over this buffer and lets you travel back through it, so it has to be
// deep enough to browse — decimation keeps drawing cheap, and every point is
// also persisted to SQLite for ranges older than the buffer.
const MAX_DATA_POINTS = 5000;

// One warning per (session, OID) that returns something ungraphable: repeating
// it on every tick would bury the UI under identical toasts.
const warnedNonNumeric = new Set();

/**
 * One sample, in the shape the rest of the interface expects.
 *
 * ONE function because there are two ways in — live samples pushed by the Go
 * scheduler, and stored ones read back on startup — and they had drifted. The
 * live path wrote `value: p.value ?? null`; the restored path wrote `p.value`
 * bare, and Go's `omitempty` leaves that `undefined`. Consumers test for null:
 * `MetricTiles.svelte:106` decides the failing indicator with
 * `lastPoint.value === null`, `MonitorChart.svelte:243` counts a failed sample
 * the same way. So after any restart a restored session stopped showing which
 * of its points had failed — silently, and only for sessions that had been
 * reloaded, which is why it survived.
 *
 * `delta`, `rate` and `responseTimeMs` had drifted the same way. Two places
 * building the same shape is the defect; the fix is that there is one.
 */
export function normalisePoint(p) {
  return {
    target: p.target,
    timestamp: p.timestamp,
    value: p.value ?? null,
    delta: p.delta ?? null,
    rate: p.rate ?? null,
    responseTimeMs: p.responseTimeMs || 0,
    error: p.error || null,
    snmpType: p.snmpType || '',
    oid: p.oid || '',
  };
}

// The poll clock lives in Go.
//
// It used to be a setInterval here, which meant monitoring only ran while the
// window was open: closing it silently stopped every session, and with them the
// thresholds, the event journal and every notification route that depends on
// them. This store is now a view onto a loop that runs whether or not anyone is
// watching — it starts and stops sessions, and receives samples as they happen.
function createPollingStore() {
  const { subscribe, update, set } = writable([]);

  // Samples pushed by the Go scheduler. Arrive whenever a poll completes, at
  // whatever cadence each session was configured with.
  EventsOn('monitor:samples', (payload) => {
    if (!payload || !payload.sessionId || !Array.isArray(payload.points)) return;
    const { sessionId, points } = payload;

    update((sessions) => sessions.map((s) => {
      if (s.id !== sessionId) return s;

      const incoming = points.map(normalisePoint);

      warnAboutUngraphableValues(sessionId, incoming);

      // The buffer is capped PER SERIES, not per session: with 8 curves a
      // shared cap would leave each one only an eighth of the history.
      const cap = MAX_DATA_POINTS * Math.max(1, s.targets.length) * Math.max(1, (s.oids || [s.oid]).length);
      let results = [...s.results, ...incoming];
      if (results.length > cap) results = results.slice(-cap);
      return { ...s, results, running: true };
    }));
  });

  // A point with neither a value nor an error came back as a type that cannot
  // be plotted — a string, an OID, an address. Say so once, rather than drawing
  // an empty chart and leaving the user to guess.
  function warnAboutUngraphableValues(sessionId, points) {
    for (const p of points) {
      if (p.value !== null || p.error) continue;
      const key = sessionId + '|' + p.oid;
      if (warnedNonNumeric.has(key)) continue;
      warnedNonNumeric.add(key);
      notificationStore.add(
        get(_)('monitor.nonNumericWarning', { values: { oid: p.oid, type: p.snmpType || '?' } }),
        'warning',
      );
    }
  }

  // `oid` accepts a single OID or a list: a session can watch several at once,
  // each rendered as its own small multiple (different OIDs have different
  // scales, so they must never share one plot).
  async function startPolling(oid, targets, intervalMs, thresholds = null, snmpVersion = 'v2c', name = '') {
    // Deduplicate: the same OID twice would poll twice, draw two identical
    // curves and collide as a key in the channel picker.
    const oidList = [...new Set((Array.isArray(oid) ? oid : [oid]).map((o) => String(o).trim()).filter(Boolean))];
    targets = [...new Set(targets)];
    // Persisted joined so a reloaded session restores the whole list.
    const oidKey = oidList.join(',');

    // thresholds is keyed by OID. Normalise every band and drop the ones that
    // set no bound at all.
    const byOid = {};
    for (const [tOid, t] of Object.entries(thresholds || {})) {
      if (!t) continue;
      const min = t.min !== null && t.min !== undefined && t.min !== '' ? Number(t.min) : null;
      const max = t.max !== null && t.max !== undefined && t.max !== '' ? Number(t.max) : null;
      if (min === null && max === null) continue;
      byOid[tOid] = {
        min,
        max,
        forSeconds: Number(t.forSeconds) || 0,
        alertEnabled: t.alertEnabled !== false,
      };
    }
    const thresholdsPayload = Object.keys(byOid).length ? byOid : null;

    // The connection is persisted WITH the session: a background poll has no
    // renderer to ask for it, and after a restart there is no renderer at all.
    const settings = get(settingsStore);
    const conn = buildMonitorConnection({ ...settings, snmpVersion });

    let id;
    try {
      id = await MonitorCreateSession(oidKey, targets, intervalMs, snmpVersion, thresholdsPayload, name, conn);
    } catch (e) {
      notificationStore.add(String(e), 'error');
      throw e;
    }

    update((sessions) => [...sessions, {
      id,
      name: (name || '').trim(),
      oid: oidList[0],
      oids: oidList,
      targets,
      interval: intervalMs,
      snmpVersion,
      results: [],
      running: true,
      startedAt: new Date().toISOString(),
      thresholds: thresholdsPayload || {},
    }]);

    try {
      await MonitorStart(id);
    } catch (e) {
      notificationStore.add(String(e), 'error');
      markRunning(id, false);
    }
    return id;
  }

  function markRunning(sessionId, running) {
    update((sessions) => sessions.map((s) => (s.id === sessionId ? { ...s, running } : s)));
  }

  async function resumeSession(sessionId) {
    markRunning(sessionId, true);
    try {
      await MonitorStart(sessionId);
    } catch (e) {
      notificationStore.add(String(e), 'error');
      markRunning(sessionId, false);
    }
  }

  async function stopPolling(sessionId) {
    markRunning(sessionId, false);
    try {
      await MonitorStop(sessionId);
    } catch (e) {
      console.warn('Failed to stop session:', e);
    }
  }

  async function removeSession(sessionId) {
    try {
      await MonitorStop(sessionId);
    } catch (e) {
      console.warn('Failed to stop session before deleting it:', e);
    }
    update((sessions) => {
      // Keys are `sessionId|oid`, so drop every key belonging to the session.
      for (const key of [...warnedNonNumeric]) {
        if (key.startsWith(sessionId + '|')) warnedNonNumeric.delete(key);
      }
      return sessions.filter((s) => s.id !== sessionId);
    });
    MonitorDeleteSession(sessionId).catch((e) => console.warn('Failed to delete session:', e));
  }

  async function stopAll() {
    const ids = get({ subscribe }).filter((s) => s.running).map((s) => s.id);
    update((sessions) => sessions.map((s) => ({ ...s, running: false })));
    await Promise.allSettled(ids.map((id) => MonitorStop(id)));
  }

  // Load persisted sessions, then ask Go which ones are actually polling. That
  // second question matters: with the window closed the scheduler kept running,
  // so the database's `active` flag is a record of intent while MonitorRunning
  // is the truth.
  async function initFromBackend() {
    try {
      const sessions = await MonitorLoadSessions();
      if (!sessions || sessions.length === 0) {
        await migrateLegacyData();
        return;
      }
      const loaded = [];
      for (const s of sessions) {
        const oids = (s.oid || '').split(',').map((o) => o.trim()).filter(Boolean);
        let results = [];
        try {
          const points = await MonitorLoadSessionData(s.id, MAX_DATA_POINTS * (s.targets?.length || 1));
          results = (points || []).map(normalisePoint);
        } catch (e) {
          console.warn('Failed to load session data:', e);
        }
        loaded.push({
          id: s.id,
          name: s.name || '',
          oid: oids[0] || '',
          oids,
          targets: s.targets || [],
          interval: s.intervalMs,
          snmpVersion: s.snmpVersion,
          results,
          running: false,
          startedAt: s.startedAt,
          thresholds: s.thresholds || {},
          // A session stored before the connection was persisted cannot be
          // polled from Go; the UI offers to re-arm it with the current
          // settings rather than failing silently.
          needsConnection: !s.conn,
        });
      }
      set(loaded);
    } catch (e) {
      console.warn('Failed to load sessions from backend:', e);
      return;
    }

    await reconcileWithScheduler();
  }

  // Reflect what the Go scheduler is really doing, and resume anything the user
  // asked to have resumed.
  async function reconcileWithScheduler() {
    let running = [];
    try {
      running = (await MonitorRunning()) || [];
    } catch (e) {
      console.warn('Failed to read the running sessions:', e);
    }
    const live = new Set(running);
    update((sessions) => sessions.map((s) => ({ ...s, running: live.has(s.id) })));

    const settings = get(settingsStore);
    if (!settings.polling?.autoResume) return;
    for (const s of get({ subscribe })) {
      if (!s.running && !s.needsConnection && s.results.length > 0) {
        resumeSession(s.id);
      }
    }
  }

  // One-time migration from localStorage.
  async function migrateLegacyData() {
    const stored = localStorage.getItem('pollingHistory');
    if (!stored) return;
    try {
      const legacySessions = JSON.parse(stored);
      if (!Array.isArray(legacySessions) || legacySessions.length === 0) {
        localStorage.removeItem('pollingHistory');
        return;
      }
      const settings = get(settingsStore);
      for (const ls of legacySessions) {
        try {
          const id = await MonitorCreateSession(
            ls.oid, ls.targets, ls.interval, ls.snmpVersion || 'v2c', ls.thresholds || null, ls.name || '',
            buildMonitorConnection({ ...settings, snmpVersion: ls.snmpVersion || 'v2c' }),
          );
          if (ls.results && ls.results.length > 0) {
            await MonitorSaveDataPoints(ls.results.map((r) => ({
              sessionId: id,
              target: r.target,
              timestamp: r.timestamp,
              value: r.value,
              delta: r.delta,
              rate: r.rate,
              responseTimeMs: r.responseTimeMs || 0,
              error: r.error || '',
              snmpType: r.snmpType || '',
              oid: r.oid || '',
            })));
          }
        } catch (e) {
          console.warn('Failed to migrate session:', e);
        }
      }
      localStorage.removeItem('pollingHistory');
      await initFromBackend();
    } catch (e) {
      console.warn('Legacy migration failed:', e);
      localStorage.removeItem('pollingHistory');
    }
  }

  // Deferred initialization
  setTimeout(initFromBackend, 300);

  return {
    subscribe,
    startPolling,
    resumeSession,
    stopPolling,
    removeSession,
    stopAll,
    reconcileWithScheduler,
  };
}

export const pollingStore = createPollingStore();
