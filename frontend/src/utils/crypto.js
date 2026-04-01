const KEY_STORAGE = '_snmplens_ek';
const PREFIX = 'enc:';

/**
 * Get or create the AES-GCM encryption key, stored as JWK in localStorage.
 * @returns {Promise<CryptoKey>}
 */
async function getOrCreateKey() {
  const stored = localStorage.getItem(KEY_STORAGE);
  if (stored) {
    try {
      const jwk = JSON.parse(stored);
      return await crypto.subtle.importKey('jwk', jwk, { name: 'AES-GCM' }, true, ['encrypt', 'decrypt']);
    } catch (e) {
      console.warn('Failed to import encryption key, generating new one:', e);
    }
  }
  const key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, true, ['encrypt', 'decrypt']);
  const jwk = await crypto.subtle.exportKey('jwk', key);
  localStorage.setItem(KEY_STORAGE, JSON.stringify(jwk));
  return key;
}

/**
 * Encrypt a plaintext string.
 * @param {CryptoKey} key
 * @param {string} plaintext
 * @returns {Promise<string>} Prefixed base64 string
 */
async function encrypt(key, plaintext) {
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const encoded = new TextEncoder().encode(plaintext);
  const ciphertext = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, encoded);
  const combined = new Uint8Array(iv.length + ciphertext.byteLength);
  combined.set(iv);
  combined.set(new Uint8Array(ciphertext), iv.length);
  return PREFIX + btoa(String.fromCharCode(...combined));
}

/**
 * Decrypt a prefixed encrypted string.
 * @param {CryptoKey} key
 * @param {string} encoded
 * @returns {Promise<string>} Plaintext
 */
async function decrypt(key, encoded) {
  if (!encoded || !encoded.startsWith(PREFIX)) return encoded;
  const data = atob(encoded.slice(PREFIX.length));
  const bytes = new Uint8Array(data.length);
  for (let i = 0; i < data.length; i++) bytes[i] = data.charCodeAt(i);
  const iv = bytes.slice(0, 12);
  const ciphertext = bytes.slice(12);
  const decrypted = await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, key, ciphertext);
  return new TextDecoder().decode(decrypted);
}

function isEncrypted(value) {
  return typeof value === 'string' && value.startsWith(PREFIX);
}

// Paths to sensitive fields in the settings object
const SENSITIVE_PATHS = [
  ['community'],
  ['v3', 'authPass'],
  ['v3', 'privPass'],
];

/**
 * Encrypt sensitive fields in a settings object (returns a deep clone).
 * Also encrypts per-target override credentials.
 * @param {object} settings
 * @returns {Promise<object>}
 */
export async function encryptSettings(settings) {
  const key = await getOrCreateKey();
  const clone = JSON.parse(JSON.stringify(settings));

  for (const path of SENSITIVE_PATHS) {
    const value = getNestedValue(clone, path);
    if (value && typeof value === 'string' && !isEncrypted(value)) {
      setNestedValue(clone, path, await encrypt(key, value));
    }
  }

  // Per-target overrides
  if (clone.targetOverrides) {
    for (const addr of Object.keys(clone.targetOverrides)) {
      const ov = clone.targetOverrides[addr];
      if (ov.community && !isEncrypted(ov.community)) {
        ov.community = await encrypt(key, ov.community);
      }
      if (ov.v3) {
        if (ov.v3.authPass && !isEncrypted(ov.v3.authPass)) {
          ov.v3.authPass = await encrypt(key, ov.v3.authPass);
        }
        if (ov.v3.privPass && !isEncrypted(ov.v3.privPass)) {
          ov.v3.privPass = await encrypt(key, ov.v3.privPass);
        }
      }
    }
  }

  return clone;
}

/**
 * Decrypt sensitive fields in a settings object (mutates in place).
 * @param {object} settings
 * @returns {Promise<object>}
 */
export async function decryptSettings(settings) {
  let key;
  try {
    key = await getOrCreateKey();
  } catch (e) {
    console.warn('Cannot get decryption key:', e);
    return settings;
  }

  for (const path of SENSITIVE_PATHS) {
    const value = getNestedValue(settings, path);
    if (isEncrypted(value)) {
      try {
        setNestedValue(settings, path, await decrypt(key, value));
      } catch (e) {
        console.warn(`Failed to decrypt ${path.join('.')}, resetting to empty:`, e);
        setNestedValue(settings, path, '');
      }
    }
  }

  if (settings.targetOverrides) {
    for (const addr of Object.keys(settings.targetOverrides)) {
      const ov = settings.targetOverrides[addr];
      if (isEncrypted(ov.community)) {
        try { ov.community = await decrypt(key, ov.community); }
        catch (e) { ov.community = ''; }
      }
      if (ov.v3) {
        if (isEncrypted(ov.v3.authPass)) {
          try { ov.v3.authPass = await decrypt(key, ov.v3.authPass); }
          catch (e) { ov.v3.authPass = ''; }
        }
        if (isEncrypted(ov.v3.privPass)) {
          try { ov.v3.privPass = await decrypt(key, ov.v3.privPass); }
          catch (e) { ov.v3.privPass = ''; }
        }
      }
    }
  }

  return settings;
}

function getNestedValue(obj, path) {
  let current = obj;
  for (const key of path) {
    if (current == null) return undefined;
    current = current[key];
  }
  return current;
}

function setNestedValue(obj, path, value) {
  let current = obj;
  for (let i = 0; i < path.length - 1; i++) {
    if (current[path[i]] == null) current[path[i]] = {};
    current = current[path[i]];
  }
  current[path[path.length - 1]] = value;
}
