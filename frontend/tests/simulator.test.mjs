// The simulator's renderer-side rules: the device form, and how a device
// becomes a target.
//
// The second is the part only the renderer can get wrong — a target lives in
// the settings — so it is checked THROUGH getEffectiveSettings, which is what
// every request is built from: a device added as a target must resolve to its
// own port and identifiers, in the most secure version it answers.
import * as esbuild from 'esbuild';
import { readFileSync, readdirSync, writeFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

// targets.js reaches the settings store, and the store the Wails bridge, so the
// rules are bundled with the bridge stubbed out — as tests/profiles.test.mjs is.
const dir = mkdtempSync(join(tmpdir(), 'snmplens-simulator-'));
writeFileSync(join(dir, 'stub.js'), `
export const SettingsKeyStatus = async () => ({ backend: 'test', available: true, hasKey: false });
export const SettingsSeal = async (v) => v;
export const SettingsOpen = async (v) => v;
export const SettingsAdoptKey = async () => {};
export const SettingsForgetKey = async () => {};
export const EventsOn = () => {};
`);
const bundle = join(dir, 'bundle.mjs');
await esbuild.build({
  entryPoints: [new URL('./fixtures/simulator-entry.js', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1')],
  bundle: true, format: 'esm', outfile: bundle, platform: 'node', logLevel: 'silent',
  plugins: [{
    name: 'alias',
    setup(b) {
      b.onResolve({ filter: /wailsjs[/](go[/]main[/]App|runtime[/]runtime)$/ }, () => ({ path: join(dir, 'stub.js') }));
    },
  }],
});
const mem = new Map();
globalThis.localStorage = {
  getItem: (k) => (mem.has(k) ? mem.get(k) : null),
  setItem: (k, v) => mem.set(k, String(v)),
  removeItem: (k) => mem.delete(k),
};
globalThis.window = { addEventListener() {}, matchMedia: () => ({ matches: false, addEventListener() {} }) };
if (!globalThis.navigator) {
  Object.defineProperty(globalThis, 'navigator', { value: { language: 'en' }, configurable: true });
}

const {
  blankDevice,
  editableDevice,
  deviceProblems,
  devicePayload,
  preferredVersion,
  addDeviceAsTarget,
  getEffectiveSettings,
} = await import(pathToFileURL(bundle).href);

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

/* --- the form ------------------------------------------------------------ */

const fresh = blankDevice('linux-server', { address: '127.0.0.4', port: 161 });
check('a new device answers v2c, where the backend suggested',
  fresh.address === '127.0.0.4' && fresh.port === 161 && fresh.versions.join() === 'v2c');
check('its one problem is its missing name',
  JSON.stringify(deviceProblems(fresh)) === JSON.stringify({ name: 'nameRequired' }),
  JSON.stringify(deviceProblems(fresh)));

const v3only = { ...fresh, name: 'x', versions: ['v3'], v3: { ...fresh.v3, authPass: 'short', privPass: 'short' } };
const p3 = deviceProblems(v3only);
check('a v3 user with short passphrases is refused on both', p3.authPass === 'passphraseShort' && p3.privPass === 'passphraseShort');
check('and v3 alone needs no community', !p3.community);
const authOnly = { ...v3only, v3: { ...v3only.v3, secLevel: 'AuthNoPriv', authPass: 'long-enough' } };
check('a passphrase the level does not use is not asked for', !deviceProblems(authOnly).privPass);

/* --- what is sent -------------------------------------------------------- */

const sent = devicePayload({ ...fresh, name: ' srv ' });
check('v2c alone sends no user, and the name trimmed',
  sent.users.length === 0 && sent.community === 'public' && sent.name === 'srv');
check("the engine is not the renderer's to send", sent.engineId === '' && sent.engineBoots === 0);
const sent3 = devicePayload({ ...v3only, v3: { ...v3only.v3, authPass: 'authpass-1', privPass: 'privpass-1' } });
check("v3 alone sends no community, and the user under Go's names",
  sent3.community === '' && sent3.users[0].name === 'simulator' && sent3.users[0].privPass === 'privpass-1',
  JSON.stringify(sent3.users));

/* --- editing brings the credentials back --------------------------------- */

const view = {
  id: 'a1', name: 'srv', model: 'linux-server', address: '127.0.0.2', port: 161,
  versions: ['v2c', 'v3'], users: [{ name: 'ops', secLevel: 'AuthPriv', authProto: 'SHA256', privProto: 'AES' }],
};
const creds = { community: 'c0mmunity', users: { ops: { authPass: 'authpass-1', privPass: 'privpass-1' } } };
const form = editableDevice(view, creds);
check('the editor is given the community and the passphrases',
  form.community === 'c0mmunity' && form.v3.user === 'ops' && form.v3.privPass === 'privpass-1');
check('and editing nothing sends back the same user',
  JSON.stringify(devicePayload(form).users[0]) === JSON.stringify({
    name: 'ops', secLevel: 'AuthPriv', authProto: 'SHA256', authPass: 'authpass-1', privProto: 'AES', privPass: 'privpass-1',
  }));

/* --- a device as a target, resolved as every request resolves it ---------- */

check('a target speaks the most secure version the device answers',
  preferredVersion(['v1', 'v2c', 'v3']) === 'v3' && preferredVersion(['v1', 'v2c']) === 'v2c' && preferredVersion(['v1']) === 'v1');

const settings = {
  targets: '10.0.0.1 # router',
  targetOverrides: {},
  community: 'default-community',
  snmpVersion: 'v2c',
  port: 161,
  timeout: 5,
  retries: 1,
  credentialProfiles: [],
  v3: { user: 'default-user', authPass: '', authProto: 'SHA', privPass: '', privProto: 'AES', secLevel: 'NoAuthNoPriv', contextName: '' },
};

const v2cDevice = { ...view, versions: ['v2c'], users: [], port: 1161 };
const first = addDeviceAsTarget(settings, v2cDevice, creds);
const eff1 = getEffectiveSettings(first.settings, '127.0.0.2');
check('the device joins the targets, named after it',
  first.added && first.settings.targets.split('\n').includes('127.0.0.2 # srv'));
check('the targets already there stay', first.settings.targets.startsWith('10.0.0.1 # router'));
check('it resolves to its own port and community, in v2c',
  eff1.port === 1161 && eff1.community === 'c0mmunity' && eff1.snmpVersion === 'v2c',
  JSON.stringify({ port: eff1.port, community: eff1.community, version: eff1.snmpVersion }));

const second = addDeviceAsTarget(first.settings, view, creds);
const eff2 = getEffectiveSettings(second.settings, '127.0.0.2');
check('an address already listed is not listed twice',
  !second.added && second.settings.targets.split('\n').filter((l) => l.startsWith('127.0.0.2')).length === 1);
check("and it takes the device's SNMPv3 identity",
  eff2.snmpVersion === 'v3' && eff2.v3.user === 'ops' && eff2.v3.secLevel === 'AuthPriv' &&
  eff2.v3.privPass === 'privpass-1' && eff2.port === 161,
  JSON.stringify({ version: eff2.snmpVersion, user: eff2.v3.user, port: eff2.port }));

/* --- every model Go serves is named and described ------------------------ */

const en = JSON.parse(readFileSync(new URL('../src/i18n/en.json', import.meta.url), 'utf8'));
const goDir = new URL('../../pkg/simulator/', import.meta.url);
const ids = new Set();
for (const file of readdirSync(goDir)) {
  if (!file.endsWith('.go') || file.endsWith('_test.go')) continue;
  for (const m of readFileSync(new URL(file, goDir), 'utf8').matchAll(/ModelInfo\{ID:\s*"([^"]+)"/g)) ids.add(m[1]);
}
check('the models were found in pkg/simulator', ids.size >= 1, [...ids].join(', '));
for (const id of ids) {
  check(`en.json names and describes the model ${id}`,
    Boolean(en.simulator?.model?.[id]?.name && en.simulator?.model?.[id]?.description));
}

process.exit(failures ? 1 : 0);
