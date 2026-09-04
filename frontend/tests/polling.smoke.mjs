// Exercises the real pollingStore against a stubbed Wails bridge, under node.
//
// There is no browser and no Go process here: the point is to prove that the
// store's half of the contract holds — that it creates a session with a usable
// connection, starts the Go clock, and turns pushed samples into the buffer the
// charts read. A ReferenceError in this file's path once silently disabled all
// monitoring, which is exactly the class of bug this catches.
import * as esbuild from 'esbuild';
import { writeFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const calls = { created: [], started: [], stopped: [], saved: [] };
const handlers = {};

globalThis.__stub = {
  MonitorCreateSession: async (...a) => { calls.created.push(a); return 'sess-1'; },
  MonitorStart: async (id) => { calls.started.push(id); },
  MonitorStop: async (id) => { calls.stopped.push(id); },
  MonitorRunning: async () => calls.started.filter((id) => !calls.stopped.includes(id)),
  MonitorSaveDataPoints: async (pts) => { calls.saved.push(pts); },
  MonitorLoadSessions: async () => [],
  MonitorLoadSessionData: async () => [],
  MonitorDeleteSession: async () => {},
  EventsOn: (name, fn) => { handlers[name] = fn; },
  // The settings store now seals its credentials through the bridge, and
  // pollingStore imports it. A store that is present and empty is the shape
  // this test wants: nothing sealed, nothing to open.
  SettingsKeyStatus: async () => ({ backend: 'test', available: true, hasKey: false }),
  SettingsSeal: async (values) => values.map((v) => (v ? 'enc:' + v : v)),
  SettingsOpen: async (values) => values.map((v) => (typeof v === 'string' && v.startsWith('enc:') ? v.slice(4) : v)),
  SettingsAdoptKey: async () => {},
  SettingsForgetKey: async () => {},
};

const stub = `const s = globalThis.__stub;
export const MonitorCreateSession = (...a) => s.MonitorCreateSession(...a);
export const MonitorStart = (...a) => s.MonitorStart(...a);
export const MonitorStop = (...a) => s.MonitorStop(...a);
export const MonitorRunning = (...a) => s.MonitorRunning(...a);
export const MonitorSaveDataPoints = (...a) => s.MonitorSaveDataPoints(...a);
export const MonitorLoadSessions = (...a) => s.MonitorLoadSessions(...a);
export const MonitorLoadSessionData = (...a) => s.MonitorLoadSessionData(...a);
export const MonitorDeleteSession = (...a) => s.MonitorDeleteSession(...a);
export const EventsOn = (...a) => s.EventsOn(...a);
export const ListMibFiles = async () => [];
export const SettingsKeyStatus = (...a) => s.SettingsKeyStatus(...a);
export const SettingsSeal = (...a) => s.SettingsSeal(...a);
export const SettingsOpen = (...a) => s.SettingsOpen(...a);
export const SettingsAdoptKey = (...a) => s.SettingsAdoptKey(...a);
export const SettingsForgetKey = (...a) => s.SettingsForgetKey(...a);`;

const dir = mkdtempSync(join(tmpdir(), 'snmplens-smoke-'));
writeFileSync(join(dir, 'stub.js'), stub);

const alias = {
  name: 'alias',
  setup(b) {
    b.onResolve({ filter: /wailsjs[/]go[/]main[/]App$/ }, () => ({ path: join(dir, 'stub.js') }));
    b.onResolve({ filter: /wailsjs[/]runtime[/]runtime$/ }, () => ({ path: join(dir, 'stub.js') }));
  },
};

const entry = new URL('../src/stores/pollingStore.js', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');
const out = join(dir, 'bundle.mjs');
await esbuild.build({
  entryPoints: [entry],
  bundle: true, format: 'esm', outfile: out, platform: 'node',
  plugins: [alias], logLevel: 'silent',
});

// localStorage is touched by settingsStore and by the legacy migration.
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

const { pollingStore } = await import(pathToFileURL(out).href);
const { get } = await import('svelte/store');

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const OIDS = ['1.3.6.1.2.1.1.3.0', '1.3.6.1.2.1.2.2.1.10.1'];
const TARGETS = ['10.0.0.1', '10.0.0.2'];

const id = await pollingStore.startPolling(
  OIDS, TARGETS, 5000,
  { [OIDS[0]]: { min: 0, max: 5, forSeconds: 30 } }, 'v2c', 'smoke',
);

check('the session was created in Go', calls.created.length === 1);
check('the Go clock was started', calls.started.includes(id));
check('the store subscribed to pushed samples', typeof handlers['monitor:samples'] === 'function');

const [oidKey, targets, interval, version, thresholds, name, conn] = calls.created[0] || [];
check('both OIDs were sent, joined', oidKey === OIDS.join(','), oidKey);
check('the targets were sent', Array.isArray(targets) && targets.length === 2);
check('the interval was sent', interval === 5000);
check('the session name was sent', name === 'smoke', name);
check('thresholds were normalised and kept', !!thresholds && thresholds[OIDS[0]].forSeconds === 30);
check('a connection profile was sent', !!conn && typeof conn === 'object');
check('the connection carries what Go needs to authenticate',
  !!conn && 'community' in conn && !!conn.v3 && 'port' in conn);
check('the SNMP version was sent', version === 'v2c', version);

// Push two rounds of samples, as the Go scheduler would.
const push = (n) => handlers['monitor:samples']({
  sessionId: id,
  points: OIDS.flatMap((oid) => TARGETS.map((target, i) => ({
    target, oid, timestamp: new Date(Date.now() + n * 1000).toISOString(),
    value: 100 * n + i, delta: n > 1 ? 100 : null, rate: n > 1 ? 20 : null,
    responseTimeMs: 4, snmpType: 'Counter32', error: '',
  }))),
});
push(1);
push(2);

let s = get(pollingStore).find((x) => x.id === id);
check('pushed samples reached the buffer', !!s && s.results.length === 8, `results=${s ? s.results.length : 'n/a'}`);
check('every OID is represented', !!s && new Set(s.results.map((r) => r.oid)).size === 2);
check('every target is represented', !!s && new Set(s.results.map((r) => r.target)).size === 2);
check('the derived rate survived the bridge', !!s && s.results.some((r) => r.rate === 20));
check('the session reads as running', !!s && s.running === true);

// A sample for another session must not land in this one.
handlers['monitor:samples']({ sessionId: 'other', points: [{ target: 'x', oid: 'y', value: 1 }] });
s = get(pollingStore).find((x) => x.id === id);
check('samples are routed by session id', s.results.length === 8);

// Malformed payloads must be ignored rather than throwing inside the handler.
let threw = false;
try {
  handlers['monitor:samples'](null);
  handlers['monitor:samples']({ sessionId: id });
  handlers['monitor:samples']({ points: [] });
} catch (e) {
  threw = true;
}
check('a malformed push is ignored, not thrown', !threw);

await pollingStore.stopPolling(id);
s = get(pollingStore).find((x) => x.id === id);
check('stopping reaches Go', calls.stopped.includes(id));
check('the session reads as stopped', !!s && s.running === false);

process.exit(failures ? 1 : 0);
