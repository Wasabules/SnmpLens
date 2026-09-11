// The simulator's renderer-side rules: the device form, with its SNMPv3 users,
// and how a device becomes a target.
//
// The second is the part only the renderer can get wrong — a target lives in
// the settings — so it is checked THROUGH getEffectiveSettings, which is what
// every request is built from: a device added as a target must resolve to its
// own port and identifiers. On macOS every device shares 127.0.0.1, so that
// takes a target that names its port.
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
  blankV3,
  editableDevice,
  deviceProblems,
  devicePayload,
  preferredVersion,
  targetOf,
  addDeviceAsTarget,
  getEffectiveSettings,
  portOf,
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

const v3With = (users) => ({ ...fresh, name: 'x', versions: ['v3'], users });
const good = (name) => ({ ...blankV3(), user: name, authPass: 'authpass-1', privPass: 'privpass-1' });

const short = deviceProblems(v3With([{ ...blankV3(), authPass: 'short', privPass: 'short' }]));
check('a v3 user with short passphrases is refused on both',
  short.users?.[0]?.authPass === 'passphraseShort' && short.users?.[0]?.privPass === 'passphraseShort');
check('and v3 alone needs no community', !short.community);
check('a passphrase the level does not use is not asked for',
  !deviceProblems(v3With([{ ...good('ops'), secLevel: 'AuthNoPriv', privPass: '' }])).users);
check('two well-formed users are no problem', !deviceProblems(v3With([good('ops'), good('monitor')])).users);
const dup = deviceProblems(v3With([good('ops'), good(' ops ')]));
check('two users with one name are both told so',
  dup.users?.[0]?.user === 'userDuplicate' && dup.users?.[1]?.user === 'userDuplicate');
check('v3 with no user is refused', deviceProblems(v3With([])).noUser === 'userRequired');

/* --- what is sent -------------------------------------------------------- */

const sent = devicePayload({ ...fresh, name: ' srv ' });
check('v2c alone sends no user, and the name trimmed',
  sent.users.length === 0 && sent.community === 'public' && sent.name === 'srv');
check("the engine is not the renderer's to send", sent.engineId === '' && sent.engineBoots === 0);
const sent3 = devicePayload(v3With([good(' ops '), good('monitor')]));
check("v3 alone sends no community, and every user under Go's names",
  sent3.community === '' && sent3.users.length === 2 && sent3.users[0].name === 'ops' && sent3.users[1].privPass === 'privpass-1',
  JSON.stringify(sent3.users.map((u) => u.name)));

/* --- editing brings the credentials back --------------------------------- */

const view = {
  id: 'a1', name: 'srv', model: 'linux-server', address: '127.0.0.2', port: 161,
  versions: ['v2c', 'v3'],
  users: [
    { name: 'ops', secLevel: 'AuthPriv', authProto: 'SHA256', privProto: 'AES' },
    { name: 'monitor', secLevel: 'AuthNoPriv', authProto: 'SHA', privProto: 'AES' },
  ],
};
const creds = {
  community: 'c0mmunity',
  users: { ops: { authPass: 'authpass-1', privPass: 'privpass-1' }, monitor: { authPass: 'monitor-pass', privPass: '' } },
};
const form = editableDevice(view, creds);
check('the editor is given the community, and every user with its passphrases',
  form.community === 'c0mmunity' && form.users.length === 2 &&
  form.users[0].privPass === 'privpass-1' && form.users[1].authPass === 'monitor-pass');
check('and editing nothing sends back the same users',
  JSON.stringify(devicePayload(form).users) === JSON.stringify([
    { name: 'ops', secLevel: 'AuthPriv', authProto: 'SHA256', authPass: 'authpass-1', privProto: 'AES', privPass: 'privpass-1' },
    { name: 'monitor', secLevel: 'AuthNoPriv', authProto: 'SHA', authPass: 'monitor-pass', privProto: 'AES', privPass: '' },
  ]));

/* --- the port a target names --------------------------------------------- */

check('a target names its port in each form a person writes one',
  portOf('10.0.0.5:1161') === 1161 && portOf('switch-01:1161') === 1161 &&
  portOf('[2001:db8::5]:1161') === 1161 && portOf(' 127.0.0.1:1162 ') === 1162);
check('an address, a name or a bare IPv6 literal names none',
  portOf('10.0.0.5') === null && portOf('switch-01') === null && portOf('::1') === null &&
  portOf('2001:db8::5') === null && portOf('[::1]') === null);
check('what is not a port is not read as one',
  portOf('10.0.0.5:0') === null && portOf('10.0.0.5:70000') === null && portOf(':1161') === null);
check("a device's target carries its port unless it is 161",
  targetOf({ address: '127.0.0.2', port: 161 }) === '127.0.0.2' &&
  targetOf({ address: '127.0.0.1', port: 1162 }) === '127.0.0.1:1162' &&
  targetOf({ address: '::1', port: 1161 }) === '[::1]:1161');

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

// Windows and Linux: an address of its own, at 161.
const own = addDeviceAsTarget(settings, view, creds);
const effOwn = getEffectiveSettings(own.settings, own.target);
check('a device at 161 is a target by its address, named after it',
  own.added && own.target === '127.0.0.2' && own.settings.targets.split('\n').includes('127.0.0.2 # srv'));
check('the targets already there stay', own.settings.targets.startsWith('10.0.0.1 # router'));
check("it resolves to the device's first SNMPv3 user, at its port",
  effOwn.snmpVersion === 'v3' && effOwn.v3.user === 'ops' && effOwn.v3.secLevel === 'AuthPriv' &&
  effOwn.v3.privPass === 'privpass-1' && effOwn.port === 161,
  JSON.stringify({ version: effOwn.snmpVersion, user: effOwn.v3.user, port: effOwn.port }));
const again = addDeviceAsTarget(own.settings, view, creds);
check('a device added twice is listed once',
  !again.added && again.settings.targets.split('\n').filter((l) => l.startsWith('127.0.0.2')).length === 1);

// macOS: every device on 127.0.0.1, told apart by port alone.
const mac1 = { ...view, id: 'm1', name: 'mac-1', address: '127.0.0.1', port: 1161, versions: ['v2c'], users: [] };
const mac2 = { ...mac1, id: 'm2', name: 'mac-2', port: 1162 };
const r1 = addDeviceAsTarget(settings, mac1, creds);
const r2 = addDeviceAsTarget(r1.settings, mac2, { community: 'other', users: {} });
const e1 = getEffectiveSettings(r2.settings, '127.0.0.1:1161');
const e2 = getEffectiveSettings(r2.settings, '127.0.0.1:1162');
check('two devices sharing 127.0.0.1 are two targets',
  r1.added && r2.added && r2.settings.targets.split('\n').length === 3, r2.settings.targets);
check('each resolving to its own port and community',
  e1.port === 1161 && e1.community === 'c0mmunity' && e2.port === 1162 && e2.community === 'other',
  JSON.stringify({ one: [e1.port, e1.community], two: [e2.port, e2.community] }));
check('with no override of a port the target already names',
  r2.settings.targetOverrides['127.0.0.1:1162'].port === undefined);

const overruled = { ...settings, targetOverrides: { '127.0.0.1:1162': { port: 9999 } } };
check("a port the target names wins over an override's", getEffectiveSettings(overruled, '127.0.0.1:1162').port === 1162);
check('and over the default, with no override at all', getEffectiveSettings(settings, '127.0.0.1:1163').port === 1163);

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
