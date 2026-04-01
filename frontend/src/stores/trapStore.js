import { writable, get } from 'svelte/store';
import { _ } from 'svelte-i18n';
import { StartTrapListener, StopTrapListener } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { notificationStore } from './notifications';
import { settingsStore } from './settingsStore';
import { buildTrapListenerRequest } from '../utils/snmpParams';

const STORAGE_KEY = 'trapHistory';
const MAX_TRAPS_DEFAULT = 1000;

// Helper function to request notification permission
async function requestNotificationPermission() {
  if (!('Notification' in window)) {
    notificationStore.add('This browser does not support desktop notification', 'error');
    return;
  }
  if (Notification.permission === 'default') {
    await Notification.requestPermission();
  }
}

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
    await requestNotificationPermission();
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

    // Show DESKTOP notification if window is not focused
    if (!storeState.isWindowFocused && Notification.permission === 'granted') {
      const trapOidVar = trap.variables.find(v => v.oid === 'snmpTrapOID.0');
      const body = trapOidVar ? `OID: ${trapOidVar.value}` : 'Check application for details.';
      new Notification(`SNMP ${trap.pduType || 'Trap'} from ${trap.source}`, {
        body: body,
      });
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
