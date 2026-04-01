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
  },
  polling: {
    retentionDays: 30,
    autoResume: false,
  },
  monitor: {
    systemNotifications: false,
    alertSound: false,
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
const initialSettings = raw ? { ...defaults, ...raw, v3: { ...defaults.v3, ...(raw.v3 || {}) }, monitor: { ...defaults.monitor, ...(raw.monitor || {}) } } : defaults;
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
