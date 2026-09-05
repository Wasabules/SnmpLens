import { writable } from 'svelte/store';
import { encryptSettings, decryptSettings, forgetCredentials, initCredentialState } from '../utils/crypto';

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
      // `saved` is never null here — withDefaults is only called when it is —
      // so the old `saved && saved[key]` guard could not fail, and CodeQL was
      // right to call it trivial. What it did NOT guard against is the case that
      // matters: a stored value of the wrong TYPE. localStorage holds whatever
      // was last written there, and spreading a string gives {0:'a',1:'b'} — a
      // settings group silently replaced by its own characters.
      const over = saved[key];
      const usable = over && typeof over === 'object' && !Array.isArray(over);
      merged[key] = usable ? { ...value, ...over } : { ...value };
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
  } else {
    // No stored blob to open — but the store's state still has to be known.
    // Without this a fresh profile kept credentialState at its 'ok' default,
    // so no banner appeared on a machine with no credential store at all, and
    // the first save believed it could seal.
    initCredentialState();
  }

  return {
    subscribe,
    save: async (settings) => {
      set(settings); // Update store immediately with plaintext
      try {
        // The blob as it stands on disk. encryptSettings needs it to carry
        // the sealed credentials forward untouched when the store cannot seal
        // — otherwise a save of an unrelated setting writes blanks over them.
        const stored = JSON.parse(localStorage.getItem('settings') || 'null');
        const encrypted = await encryptSettings(settings, stored);
        localStorage.setItem('settings', JSON.stringify(encrypted));
      } catch (e) {
        // Sealing failed — a locked keychain, no store on this machine.
        //
        // This session keeps working: `set(settings)` above already published
        // the plaintext. What must NOT happen is writing that plaintext to
        // localStorage as a fallback, or writing a half-sealed object over
        // ciphertext that is still good. Leaving the stored blob untouched
        // means the credentials are still there when the store comes back.
        console.warn('settings not saved: credentials could not be sealed:', e);
      }
    },
    reset: async () => {
      localStorage.removeItem('settings');
      // The key too. Leaving it behind kept custody of credentials that no
      // longer exist, and the next save would then seal under an old key.
      await forgetCredentials();
      set({ ...defaults });
    }
  };
}

export const settingsStore = createSettingsStore();
