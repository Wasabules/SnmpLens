// The renderer's half of credential custody, run against a stubbed bridge.
//
// The key used to sit in the same localStorage as the ciphertext it protected,
// so the encryption was decorative. It now lives in the OS store and the
// renderer never holds it — which introduces failure modes localStorage never
// had: a locked keychain, a machine with no store, a profile moved between
// Windows accounts.
//
// Every case below is about what happens THEN, because the dangerous answers
// are the tempting ones: fall back to writing the plaintext, or blank the
// stored value and call it clean. The first defeats the whole change; the
// second turns one bad second into permanently lost credentials.
import * as esbuild from 'esbuild';
import { writeFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

// --- a localStorage good enough to observe ---
function makeStorage(initial = {}) {
  const data = { ...initial };
  return {
    data,
    getItem: (k) => (k in data ? data[k] : null),
    setItem: (k, v) => { data[k] = String(v); },
    removeItem: (k) => { delete data[k]; },
    clear: () => { for (const k of Object.keys(data)) delete data[k]; },
  };
}

const stubSource = `const s = globalThis.__stub;
export const SettingsKeyStatus = (...a) => s.SettingsKeyStatus(...a);
export const SettingsSeal = (...a) => s.SettingsSeal(...a);
export const SettingsOpen = (...a) => s.SettingsOpen(...a);
export const SettingsAdoptKey = (...a) => s.SettingsAdoptKey(...a);
export const SettingsForgetKey = (...a) => s.SettingsForgetKey(...a);
export const EventsOn = () => {};
`;

const dir = mkdtempSync(join(tmpdir(), 'snmplens-cred-'));
writeFileSync(join(dir, 'stub.js'), stubSource);

const bundle = await esbuild.build({
  entryPoints: [join(process.cwd(), 'src/utils/crypto.js')],
  bundle: true,
  format: 'esm',
  write: false,
  logLevel: 'silent',
  plugins: [{
    name: 'alias',
    setup(b) {
      b.onResolve({ filter: /wailsjs[/]go[/]main[/]App$/ }, () => ({ path: join(dir, 'stub.js') }));
    },
  }],
});
writeFileSync(join(dir, 'crypto.mjs'), bundle.outputFiles[0].text);

/** A fresh module instance per scenario — the state stores are module-level. */
async function load(stub, storage) {
  globalThis.__stub = stub;
  globalThis.localStorage = storage;
  // A distinct query string forces a new module instance.
  const url = pathToFileURL(join(dir, 'crypto.mjs')).href + '?n=' + load.n++;
  return import(url);
}
load.n = 0;

const settings = () => ({
  community: 'enc:SEALED-COMMUNITY',
  v3: { authPass: 'enc:SEALED-AUTH', privPass: '' },
  targetOverrides: {
    '10.0.0.1': { community: 'enc:SEALED-OVERRIDE', v3: { authPass: '', privPass: '' } },
  },
});

const working = {
  SettingsKeyStatus: async () => ({ backend: 'test-store', available: true, hasKey: true }),
  SettingsSeal: async (v) => v.map((x) => (x && !x.startsWith('enc:') ? 'enc:' + x : x)),
  SettingsOpen: async (v) => v.map((x) => (typeof x === 'string' && x.startsWith('enc:') ? x.slice(4) : x)),
  SettingsAdoptKey: async () => {},
  SettingsForgetKey: async () => {},
};

// --- the ordinary path ---
{
  const store = makeStorage();
  const m = await load(working, store);
  const s = settings();
  await m.decryptSettings(s);

  check('sealed values are opened', s.community === 'SEALED-COMMUNITY', s.community);
  check('nested values are opened', s.v3.authPass === 'SEALED-AUTH');
  check('per-target overrides are opened',
    s.targetOverrides['10.0.0.1'].community === 'SEALED-OVERRIDE');
  check('an empty field stays empty', s.v3.privPass === '');

  const sealed = await m.encryptSettings(s);
  check('sealing covers every field',
    sealed.community.startsWith('enc:') &&
    sealed.v3.authPass.startsWith('enc:') &&
    sealed.targetOverrides['10.0.0.1'].community.startsWith('enc:'));
  check('an empty field is not sealed', sealed.v3.privPass === '');
  check('the original object is not mutated by sealing', s.community === 'SEALED-COMMUNITY');
}

// --- no OS store: work for this session, save nothing, leak nothing ---
{
  const store = makeStorage();
  const m = await load({
    ...working,
    SettingsKeyStatus: async () => ({ backend: 'unavailable', available: false, hasKey: false }),
    SettingsSeal: async () => { throw new Error('no secret store'); },
    SettingsOpen: async () => { throw new Error('no secret store'); },
  }, store);

  const s = settings();
  await m.decryptSettings(s);
  check('with no store the state says so', m.getState() === 'nostore', m.getState());
  check('an enc: string never survives into a usable field',
    s.community === '' && s.v3.authPass === '',
    JSON.stringify([s.community, s.v3.authPass]));

  let threw = false;
  try { await m.encryptSettings({ community: 'public', v3: {}, targetOverrides: {} }); }
  catch (e) { threw = true; }
  check('sealing fails loudly rather than writing plaintext', threw);
  check('nothing was written to localStorage', Object.keys(store.data).length === 0,
    JSON.stringify(store.data));
}

// --- locked store: the stored values must be left ALONE ---
{
  const store = makeStorage({ settings: JSON.stringify(settings()) });
  const before = store.data.settings;
  const m = await load({
    ...working,
    SettingsKeyStatus: async () => ({ backend: 'windows-dpapi', available: true, hasKey: true }),
    SettingsOpen: async () => { throw new Error('cannot be decrypted with the stored key'); },
  }, store);

  const s = settings();
  await m.decryptSettings(s);
  check('a locked store is reported as locked', m.getState() === 'locked', m.getState());
  check('the in-memory credential is blanked', s.community === '');
  check('the STORED blob is untouched', store.data.settings === before);
  check('the backend is named for the banner', m.getBackend() === 'windows-dpapi', m.getBackend());
}

// --- migration ---
{
  const legacy = JSON.stringify({ kty: 'oct', k: 'AAAA', alg: 'A256GCM' });
  const store = makeStorage({ _snmplens_ek: legacy });
  const adopted = [];
  const m = await load({
    ...working,
    SettingsAdoptKey: async (k) => { adopted.push(k); },
  }, store);

  await m.decryptSettings(settings());
  check('the legacy key is handed to the store', adopted.join() === 'AAAA', adopted.join());
  check('the legacy key is removed once adoption is verified',
    store.getItem('_snmplens_ek') === null);
}

// The old key must survive a failed adoption, or the credentials are lost.
{
  const legacy = JSON.stringify({ kty: 'oct', k: 'AAAA' });
  const store = makeStorage({ _snmplens_ek: legacy });
  const m = await load({
    ...working,
    SettingsAdoptKey: async () => { throw new Error('a settings key is already stored'); },
  }, store);

  await m.decryptSettings(settings());
  check('a failed adoption keeps the local key', store.getItem('_snmplens_ek') === legacy);
}

// Adoption that is accepted but does not actually open the blob must NOT be
// treated as success: GCM authenticates, so a failed open means a wrong key.
{
  const legacy = JSON.stringify({ kty: 'oct', k: 'AAAA' });
  const store = makeStorage({ _snmplens_ek: legacy });
  const m = await load({
    ...working,
    SettingsOpen: async () => { throw new Error('cannot be decrypted with the stored key'); },
  }, store);

  await m.decryptSettings(settings());
  check('a key that does not open the blob is not trusted',
    store.getItem('_snmplens_ek') === legacy);
}

// --- reset ---
{
  const store = makeStorage({ _snmplens_ek: '{"k":"AAAA"}', settings: '{}' });
  let forgotten = false;
  const m = await load({ ...working, SettingsForgetKey: async () => { forgotten = true; } }, store);

  await m.forgetCredentials();
  check('reset forgets the key in the store', forgotten);
  check('reset removes the legacy key too', store.getItem('_snmplens_ek') === null);
}

process.exit(failures ? 1 : 0);
