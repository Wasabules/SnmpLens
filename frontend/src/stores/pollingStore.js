import { writable, get } from 'svelte/store';
import { _ } from 'svelte-i18n';
import { SnmpGet, MonitorCreateSession, MonitorSaveDataPoints, MonitorLoadSessions, MonitorLoadSessionData, MonitorDeleteSession, MonitorUpdateSession } from '../../wailsjs/go/main/App';
import { settingsStore } from './settingsStore';
import { notificationStore } from './notifications';
import { buildSnmpRequest } from '../utils/snmpParams';
import { sendNativeNotification } from '../utils/nativeNotify';

const MAX_DATA_POINTS = 500;
const ALERT_COOLDOWN_MS = 30000;

const warnedNonNumeric = new Set();

// Alert sound via Web Audio API
function playAlertSound() {
  try {
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.frequency.value = 880;
    osc.type = 'sine';
    gain.gain.value = 0.3;
    osc.start();
    osc.stop(ctx.currentTime + 0.2);
  } catch (e) { /* ignore */ }
}

// Send native OS notification for threshold alerts
function sendDesktopNotification(title, body) {
  const settings = get(settingsStore);
  if (!settings.monitor?.systemNotifications) return;
  sendNativeNotification(title, body);
  if (settings.monitor?.alertSound) playAlertSound();
}

function createPollingStore() {
  const { subscribe, update, set } = writable([]);
  const lastAlertTime = {};

  function createPollFn(id, oid, targets, intervalMs, snmpVersion) {
    return async function poll() {
      const settings = get(settingsStore);
      try {
        const bulkResults = await SnmpGet(buildSnmpRequest({...settings, snmpVersion}, targets, oid));
        const timestamp = new Date().toISOString();

        // Non-numeric SNMP types that should not be graphed
        const nonNumericTypes = ['OctetString', 'ObjectIdentifier', 'IPAddress', 'Opaque', 'NsapAddress', 'BitString'];
        // Error-like sentinel values returned by the backend for missing OIDs
        const errorSentinels = ['noSuchObject', 'noSuchInstance', 'endOfMibView'];

        // Build data points outside update() so we can persist them to SQLite
        let newDataPoints = [];
        const currentSessions = get({ subscribe });
        const currentSession = currentSessions.find(s => s.id === id);

        for (const res of bulkResults) {
          const rawValue = res.error ? null : res.result?.value;
          const snmpType = res.result?.type || '';
          const isNonNumericType = nonNumericTypes.some(t => snmpType.toLowerCase().includes(t.toLowerCase()));
          const isErrorSentinel = rawValue !== null && errorSentinels.includes(rawValue);

          let value = null;
          if (rawValue !== null && !isNonNumericType && !isErrorSentinel) {
            const numValue = Number(rawValue);
            value = isNaN(numValue) ? null : numValue;
          }

          if (rawValue !== null && (isNonNumericType || isErrorSentinel) && !warnedNonNumeric.has(id)) {
            warnedNonNumeric.add(id);
            const t = get(_);
            notificationStore.add(
              t('monitor.nonNumericWarning', { values: { oid, type: snmpType } }),
              'warning'
            );
          }

          const prevResults = currentSession ? currentSession.results : [];
          const prevPoint = [...prevResults].reverse().find(
            p => p.target === res.target && p.value !== null
          );

          const delta = prevPoint && value !== null ? value - prevPoint.value : null;
          const rate = delta !== null && intervalMs > 0 ? delta / (intervalMs / 1000) : null;

          newDataPoints.push({
            target: res.target,
            timestamp,
            value,
            delta,
            rate,
            responseTimeMs: res.responseTimeMs || 0,
            error: isErrorSentinel ? String(rawValue) : (res.error || null),
          });
        }

        update(sessions => sessions.map(s => {
          if (s.id !== id || !s.running) return s;

          // Threshold alert checks
          if (s.thresholds && s.thresholds.alertEnabled) {
            const now = Date.now();
            const lastAlert = lastAlertTime[id] || 0;
            if (now - lastAlert > ALERT_COOLDOWN_MS) {
              for (const dp of newDataPoints) {
                if (dp.value === null) continue;
                const { min, max } = s.thresholds;
                if (min !== null && min !== undefined && min !== '' && dp.value < Number(min)) {
                  const t = get(_);
                  const msg = t('monitor.thresholdBelow', { values: { target: dp.target, value: dp.value, min, oid } });
                  notificationStore.add(msg, 'error');
                  sendDesktopNotification(t('monitor.thresholdAlertTitle'), msg);
                  lastAlertTime[id] = now;
                  break;
                }
                if (max !== null && max !== undefined && max !== '' && dp.value > Number(max)) {
                  const t = get(_);
                  const msg = t('monitor.thresholdAbove', { values: { target: dp.target, value: dp.value, max, oid } });
                  notificationStore.add(msg, 'error');
                  sendDesktopNotification(t('monitor.thresholdAlertTitle'), msg);
                  lastAlertTime[id] = now;
                  break;
                }
              }
            }
          }

          let updatedResults = [...s.results, ...newDataPoints];
          if (updatedResults.length > MAX_DATA_POINTS * targets.length) {
            updatedResults = updatedResults.slice(-MAX_DATA_POINTS * targets.length);
          }

          return { ...s, results: updatedResults };
        }));

        // Persist data points to SQLite (fire-and-forget)
        MonitorSaveDataPoints(newDataPoints.map(dp => ({
          sessionId: id,
          target: dp.target,
          timestamp: dp.timestamp,
          value: dp.value,
          delta: dp.delta,
          rate: dp.rate,
          responseTimeMs: dp.responseTimeMs,
          error: dp.error || '',
        }))).catch(e => console.warn('Failed to save data points:', e));

      } catch (err) {
        console.error('Polling error:', err);
      }
    };
  }

  async function startPolling(oid, targets, intervalMs, thresholds = null, snmpVersion = '2c') {
    // Create persistent session in backend
    let persistentId;
    try {
      const thresholdsPayload = thresholds ? {
        min: thresholds.min !== null && thresholds.min !== '' ? Number(thresholds.min) : null,
        max: thresholds.max !== null && thresholds.max !== '' ? Number(thresholds.max) : null,
        alertEnabled: !!thresholds.alertEnabled,
      } : null;
      persistentId = await MonitorCreateSession(oid, targets, intervalMs, snmpVersion, thresholdsPayload);
    } catch (e) {
      console.warn('Failed to create persistent session:', e);
      persistentId = 'local-' + Date.now();
    }

    const session = {
      id: persistentId,
      oid,
      targets,
      interval: intervalMs,
      snmpVersion,
      results: [],
      running: true,
      startedAt: new Date().toISOString(),
      timerId: null,
      thresholds,
    };

    const poll = createPollFn(session.id, oid, targets, intervalMs, snmpVersion);
    poll();
    session.timerId = setInterval(poll, intervalMs);

    update(sessions => [...sessions, session]);
    return session.id;
  }

  function resumeSession(sessionId) {
    update(sessions => sessions.map(s => {
      if (s.id !== sessionId || s.running) return s;
      const poll = createPollFn(s.id, s.oid, s.targets, s.interval, s.snmpVersion);
      poll();
      MonitorUpdateSession(s.id, true, '').catch(() => {});
      return { ...s, running: true, timerId: setInterval(poll, s.interval) };
    }));
  }

  function stopPolling(sessionId) {
    update(sessions => sessions.map(s => {
      if (s.id === sessionId && s.running) {
        clearInterval(s.timerId);
        MonitorUpdateSession(s.id, false, new Date().toISOString()).catch(() => {});
        return { ...s, running: false, timerId: null };
      }
      return s;
    }));
  }

  function removeSession(sessionId) {
    update(sessions => {
      const session = sessions.find(s => s.id === sessionId);
      if (session && session.timerId) {
        clearInterval(session.timerId);
      }
      warnedNonNumeric.delete(sessionId);
      return sessions.filter(s => s.id !== sessionId);
    });
    MonitorDeleteSession(sessionId).catch(e => console.warn('Failed to delete session:', e));
  }

  function stopAll() {
    update(sessions => {
      for (const s of sessions) {
        if (s.timerId) clearInterval(s.timerId);
        MonitorUpdateSession(s.id, false, new Date().toISOString()).catch(() => {});
      }
      return sessions.map(s => ({ ...s, running: false, timerId: null }));
    });
  }

  // Load persisted sessions from SQLite backend
  async function initFromBackend() {
    try {
      const sessions = await MonitorLoadSessions();
      if (!sessions || sessions.length === 0) {
        // Try localStorage migration
        await migrateLegacyData();
        return;
      }
      const loaded = [];
      for (const s of sessions) {
        let results = [];
        try {
          const points = await MonitorLoadSessionData(s.id, MAX_DATA_POINTS * (s.targets?.length || 1));
          results = (points || []).map(p => ({
            target: p.target,
            timestamp: p.timestamp,
            value: p.value,
            delta: p.delta,
            rate: p.rate,
            responseTimeMs: p.responseTimeMs,
            error: p.error || null,
          }));
        } catch (e) {
          console.warn('Failed to load session data:', e);
        }
        loaded.push({
          id: s.id,
          oid: s.oid,
          targets: s.targets || [],
          interval: s.intervalMs,
          snmpVersion: s.snmpVersion,
          results,
          running: false,
          startedAt: s.startedAt,
          timerId: null,
          thresholds: s.thresholds,
        });
      }
      set(loaded);
    } catch (e) {
      console.warn('Failed to load sessions from backend:', e);
    }

    // Auto-resume if enabled
    const settings = get(settingsStore);
    if (settings.polling?.autoResume) {
      const sessions = get(pollingStore);
      for (const s of sessions) {
        if (!s.running && s.results.length > 0) {
          resumeSession(s.id);
        }
      }
    }
  }

  // One-time migration from localStorage
  async function migrateLegacyData() {
    const stored = localStorage.getItem('pollingHistory');
    if (!stored) return;
    try {
      const legacySessions = JSON.parse(stored);
      if (!Array.isArray(legacySessions) || legacySessions.length === 0) {
        localStorage.removeItem('pollingHistory');
        return;
      }
      // Re-create sessions in backend
      for (const ls of legacySessions) {
        try {
          const id = await MonitorCreateSession(
            ls.oid, ls.targets, ls.interval, ls.snmpVersion || 'v2c', ls.thresholds || null
          );
          if (ls.results && ls.results.length > 0) {
            await MonitorSaveDataPoints(ls.results.map(r => ({
              sessionId: id,
              target: r.target,
              timestamp: r.timestamp,
              value: r.value,
              delta: r.delta,
              rate: r.rate,
              responseTimeMs: r.responseTimeMs || 0,
              error: r.error || '',
            })));
          }
        } catch (e) {
          console.warn('Failed to migrate session:', e);
        }
      }
      localStorage.removeItem('pollingHistory');
      // Reload from backend
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
  };
}

export const pollingStore = createPollingStore();
