import { writable, get } from 'svelte/store';
import { _ } from 'svelte-i18n';
import { StartTrapListener, StopTrapListener, GetOidDetails } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { notificationStore } from './notifications';
import { settingsStore } from './settingsStore';
import { buildTrapListenerRequest } from '../utils/snmpParams';
import { sendNativeNotification } from '../utils/nativeNotify';

const STORAGE_KEY = 'trapHistory';
const MAX_TRAPS_DEFAULT = 1000;

function loadPersistedTraps() {
  const settings = get(settingsStore);
  if (settings.traps?.persist) {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored) return JSON.parse(stored);
    } catch (e) {
      console.warn('Failed to load persisted traps:', e);
    }
  }
  return [];
}

function persistTraps(traps) {
  const settings = get(settingsStore);
  if (settings.traps?.persist) {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(traps));
    } catch (e) {
      console.warn('Failed to persist traps:', e);
    }
  }
}

function createTrapStore() {
  const initialTraps = loadPersistedTraps();

  const { subscribe, update } = writable({
    isListening: false,
    traps: initialTraps,
    isPanelVisible: false,
    isWindowFocused: true,
  });

  async function start() {
    const currentSettings = get(settingsStore);

    try {
      await StartTrapListener(buildTrapListenerRequest(currentSettings));
      update(store => ({ ...store, isListening: true }));
      const t = get(_);
      notificationStore.add(t('traps.listenerStarted', { values: { port: currentSettings.trapPort } }), 'success');
    } catch (err) {
      const t = get(_);
      notificationStore.add(t('traps.listenerStartFailed', { values: { error: err } }), 'error');
    }
  }

  async function stop() {
    try {
      await StopTrapListener();
      update(store => ({ ...store, isListening: false }));
      const t = get(_);
      notificationStore.add(t('traps.listenerStopped2'), 'info');
    } catch (err) {
      const t = get(_);
      notificationStore.add(t('traps.listenerStopFailed', { values: { error: err } }), 'error');
    }
  }

  function clearTraps() {
    update(s => {
      const newTraps = [];
      persistTraps(newTraps);
      return { ...s, traps: newTraps };
    });
    const t = get(_);
    notificationStore.add(t('traps.allCleared'), 'info');
  }

  // Listen for incoming traps from the backend
  EventsOn('newTrap', (trap) => {
    const storeState = get(trapStore);

    // Show internal notification if panel is hidden
    if (!storeState.isPanelVisible) {
      const t = get(_);
      notificationStore.add(t('traps.newTrapReceived', { values: { type: trap.pduType || 'Trap', source: trap.source } }), 'info');
    }

    // Send native OS notification (Windows toast / macOS / Linux)
    const settings = get(settingsStore);
    if (settings.traps?.nativeNotifications && !storeState.isWindowFocused) {
      (async () => {
        const trapOidVar = trap.variables?.find(v =>
          v.oid === 'snmpTrapOID.0' ||
          v.oid === '.1.3.6.1.6.3.1.1.4.1.0' ||
          v.oid === '1.3.6.1.6.3.1.1.4.1.0' ||
          (v.oid && v.oid.endsWith('.1.6.3.1.1.4.1.0'))
        );
        let body = '';
        if (trapOidVar) {
          const oidValue = String(trapOidVar.value).replace(/^\./, '');
          try {
            const details = await GetOidDetails(oidValue);
            body = details.name ? `${details.name} (${oidValue})` : oidValue;
          } catch {
            body = oidValue;
          }
        }
        sendNativeNotification(
          `SNMP ${trap.pduType || 'Trap'} from ${trap.source}`,
          body || 'Check application for details.'
        );
      })();
    }

    const enrichedTrap = {
      ...trap,
      pduType: trap.pduType || 'Trap',
      timestamp: trap.timestamp || new Date().toISOString(),
    };

    update(s => {
      const settings = get(settingsStore);
      const maxCount = settings.traps?.maxCount || MAX_TRAPS_DEFAULT;
      let newTraps = [enrichedTrap, ...s.traps];
      if (newTraps.length > maxCount) {
        newTraps = newTraps.slice(0, maxCount);
      }
      persistTraps(newTraps);
      return { ...s, traps: newTraps };
    });
  });

  // Listen for backend errors
  EventsOn('trapError', (error) => {
    update(store => ({ ...store, isListening: false }));
    const t = get(_);
    notificationStore.add(t('traps.listenerError', { values: { error } }), 'error');
  });

  return {
    subscribe,
    start,
    stop,
    clearTraps,
    setPanelVisibility: (isVisible) => update(store => ({ ...store, isPanelVisible: isVisible })),
    setWindowFocus: (isFocused) => update(store => ({ ...store, isWindowFocused: isFocused })),
  };
}

export const trapStore = createTrapStore();
