import { writable } from 'svelte/store';
import { encryptSettings, decryptSettings } from '../utils/crypto';

// Default settings
const defaults = {
  targets: '127.0.0.1',
  snmpVersion: 'v2c',
  port: 161,
  trapPort: 162,
  timeout: 5,
  retries: 1,
  community: 'public',
  autoGetEnabled: false,
  autoFillSetEnabled: false,
  skipSetConfirm: false,
  locale: '',
  theme: 'system',
  targetOverrides: {},
  targetGroups: [
    { id: 'default', name: 'Default' }
  ],
  targetGroupAssignments: {},
  traps: {
    persist: false,
    maxCount: 1000,
    nativeNotifications: true,
  },
  polling: {
    retentionDays: 30,
    autoResume: false,
  },
  monitor: {
    systemNotifications: false,
    alertSound: false,
  },
  updates: {
    autoCheck: true,
  },
  v3: {
    user: '',
    authProto: 'MD5',
    authPass: '',
    privProto: 'DES',
    privPass: '',
    secLevel: 'NoAuthNoPriv',
    contextName: '',
  }
};

// Load synchronously with defaults, then decrypt async
const raw = JSON.parse(localStorage.getItem('settings') || 'null');

// Merge saved settings over the defaults, one level deep into EVERY nested
// object. Listing the sub-objects by hand meant any group added later silently
// lost its new defaults for existing users — `polling` was already in that
// state. Arrays and primitives are taken from the saved value as-is.
function withDefaults(base, saved) {
  const merged = { ...base, ...saved };
  for (const [key, value] of Object.entries(base)) {
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      merged[key] = { ...value, ...(saved && saved[key] ? saved[key] : {}) };
    }
  }
  return merged;
}

const initialSettings = raw ? withDefaults(defaults, raw) : { ...defaults };
// Anonymous mode is always off on startup (intentionally non-persistent)
initialSettings.anonymousMode = false;

function createSettingsStore() {
  const { subscribe, set } = writable(initialSettings);

  // Decrypt on startup (async, updates store once done)
  if (raw) {
    decryptSettings(initialSettings).then(decrypted => {
      set(decrypted);
    });
  }

  return {
    subscribe,
    save: async (settings) => {
      set(settings); // Update store immediately with plaintext
      const encrypted = await encryptSettings(settings);
      localStorage.setItem('settings', JSON.stringify(encrypted));
    },
    reset: () => {
      localStorage.removeItem('settings');
      set(defaults);
    }
  };
}

export const settingsStore = createSettingsStore();
