/**
 * The simulator's rules on the renderer's side, kept out of the components so
 * they run under node (tests/simulator.test.mjs).
 *
 * Go is the authority on a device (simulator.Device.Validate, the loopback rule
 * included). What is here spares a round trip for the obvious, and decides the
 * one thing Go cannot: how a device becomes a TARGET, which lives in the
 * renderer's settings.
 *
 * The editor holds each SNMPv3 user in the shape UsmFields edits — `user`,
 * `secLevel`, the protocols and passphrases — and devicePayload turns them into
 * Go's `users` list. What the device sends, `traps`, is Go's shape already, its
 * destinations' communities filled in from the credential store by ID.
 */
import { usesAuth, usesPriv } from './snmpSecurity.js';
import { buildSnmpRequest } from './snmpParams.js';
import { getEffectiveSettings, getTargetsAsArray } from './targets.js';

/** The versions a device may answer, in the order they are shown. */
export const SIM_VERSIONS = ['v1', 'v2c', 'v3'];

/** The shortest passphrase a simulated user may have, as pkg/simulator holds it. */
export const MIN_PASSPHRASE = 8;

/** The longest a schedule waits between two notifications, as pkg/simulator holds it: a day. */
export const MAX_EVERY = 86400;

/** How many destinations and schedules one device keeps, as pkg/simulator bounds them. */
export const MAX_DESTINATIONS = 8;
export const MAX_SCHEDULES = 8;

/** What a new device sends: nothing, to nowhere. */
export function blankTraps() {
  return { destinations: [], onStart: false, onAuthFailure: false, schedules: [] };
}

/**
 * A new destination: SnmpLens on this machine, at the port its trap listener
 * takes, in v2c — the first place a person testing SnmpLens wants traps sent.
 */
export function blankDestination(trapPort) {
  return {
    id: '',
    host: '127.0.0.1',
    port: Number(trapPort) || 162,
    version: 'v2c',
    inform: false,
    community: 'public',
    user: '',
    engineId: '',
  };
}

/** A new schedule: a notification at random, every minute. */
export function blankSchedule() {
  return { notification: '', every: 60, irregular: false };
}

/** A new SNMPv3 user; `n` numbers the ones after the first. */
export function blankV3(n = 1) {
  return {
    user: n === 1 ? 'simulator' : `user${n}`,
    secLevel: 'AuthPriv',
    authProto: 'SHA256',
    authPass: '',
    privProto: 'AES',
    privPass: '',
    contextName: '',
  };
}

/**
 * A new device, where the backend suggested it answer. v2c alone, with the
 * community every tool tries first: a first device should answer before anyone
 * has typed a passphrase.
 */
export function blankDevice(model, suggestion) {
  return {
    id: '',
    name: '',
    model,
    address: suggestion?.address || '127.0.0.2',
    port: suggestion?.port || 1161,
    versions: ['v2c'],
    community: 'public',
    users: [blankV3()],
    traps: blankTraps(),
  };
}

/**
 * A listed device in the editor's shape, its credentials filled in. The list
 * never carries them; they come from SimulatorDeviceCredentials, by user name.
 */
export function editableDevice(view, creds) {
  const users = (view.users || []).map((u) => ({
    user: u.name,
    secLevel: u.secLevel,
    authProto: u.authProto || 'SHA256',
    authPass: creds?.users?.[u.name]?.authPass || '',
    privProto: u.privProto || 'AES',
    privPass: creds?.users?.[u.name]?.privPass || '',
    contextName: '',
  }));
  return {
    id: view.id,
    name: view.name,
    model: view.model,
    address: view.address,
    port: view.port,
    versions: [...(view.versions || [])],
    community: creds?.community || '',
    users: users.length ? users : [blankV3()],
    traps: {
      destinations: (view.traps?.destinations || []).map((t) => ({
        id: t.id,
        host: t.host,
        port: t.port,
        version: t.version,
        inform: !!t.inform,
        community: creds?.destinations?.[t.id] || '',
        user: t.user || '',
        engineId: t.engineId || '',
      })),
      onStart: !!view.traps?.onStart,
      onAuthFailure: !!view.traps?.onAuthFailure,
      schedules: (view.traps?.schedules || []).map((s) => ({ ...s })),
    },
  };
}

const usesCommunity = (versions) => versions.includes('v1') || versions.includes('v2c');

/**
 * What a device would be refused for, as i18n key suffixes
 * (simulator.problem.<key>): by field, and under `users`, one entry per SNMPv3
 * user — present only when one of them has something wrong. Mirrors
 * Device.Validate for what a form can get wrong; Go still checks all of it, and
 * the address.
 */
export function deviceProblems(d) {
  const problems = {};
  const versions = d.versions || [];
  if (!d.name?.trim()) problems.name = 'nameRequired';
  const port = Number(d.port);
  if (!Number.isInteger(port) || port < 1 || port > 65535) problems.port = 'port';
  if (!versions.length) problems.versions = 'versionRequired';
  if (usesCommunity(versions) && !d.community) problems.community = 'communityRequired';
  if (versions.includes('v3')) {
    const users = d.users || [];
    if (!users.length) problems.noUser = 'userRequired';
    const count = new Map();
    for (const u of users) {
      const name = (u.user || '').trim();
      count.set(name, (count.get(name) || 0) + 1);
    }
    const perUser = users.map((u) => {
      const p = {};
      const name = (u.user || '').trim();
      if (!name) p.user = 'userRequired';
      else if (count.get(name) > 1) p.user = 'userDuplicate';
      if (usesAuth(u.secLevel) && (u.authPass || '').length < MIN_PASSPHRASE) p.authPass = 'passphraseShort';
      if (usesPriv(u.secLevel) && (u.privPass || '').length < MIN_PASSPHRASE) p.privPass = 'passphraseShort';
      return p;
    });
    if (perUser.some((p) => Object.keys(p).length)) problems.users = perUser;
  }
  const answersV3 = versions.includes('v3');
  const userNames = answersV3 ? (d.users || []).map((u) => (u.user || '').trim()) : [];
  const perDestination = (d.traps?.destinations || []).map((t) => destinationProblems(t, userNames, answersV3));
  if (perDestination.some((p) => Object.keys(p).length)) problems.destinations = perDestination;
  const perSchedule = (d.traps?.schedules || []).map((s) => {
    const every = Number(s.every);
    return Number.isInteger(every) && every >= 1 && every <= MAX_EVERY ? {} : { every: 'every' };
  });
  if (perSchedule.some((p) => Object.keys(p).length)) problems.schedules = perSchedule;
  return problems;
}

/**
 * What one destination would be refused for. A v3 notification is sent as one
 * of the device's SNMPv3 users, and a device has users only when it answers v3.
 */
function destinationProblems(t, userNames, answersV3) {
  const p = {};
  if (!hostOf(t.host)) p.host = 'hostRequired';
  const port = Number(t.port);
  if (!Number.isInteger(port) || port < 1 || port > 65535) p.port = 'port';
  if (t.version === 'v3') {
    if (!answersV3) p.user = 'trapNeedsV3';
    else if (!userNames.includes((t.user || '').trim())) p.user = 'trapUser';
    if (t.inform && t.engineId && !/^([0-9a-f]{2}){5,32}$/.test(engineIdOf(t.engineId))) p.engineId = 'engineId';
  } else if (!t.community) {
    p.community = 'communityRequired';
  }
  return p;
}

/** A host as Go takes it: without the brackets people put round an IPv6 literal. */
function hostOf(host) {
  return (host || '').trim().replace(/^\[(.*)\]$/, '$1');
}

/** An engine ID as Go takes it: hex, without a 0x, colons or spaces. */
export function engineIdOf(s) {
  return (s || '').trim().replace(/^0x/i, '').replace(/[\s:]/g, '').toLowerCase();
}

/**
 * The device as SimulatorSaveDevice takes it: the community only if a version
 * uses one, the users only if v3 is answered. The engine is the backend's to
 * give, and is not sent.
 */
export function devicePayload(d) {
  const versions = SIM_VERSIONS.filter((v) => d.versions.includes(v));
  return {
    id: d.id || '',
    name: d.name.trim(),
    model: d.model,
    address: d.address.trim(),
    port: Number(d.port),
    versions,
    community: usesCommunity(versions) ? d.community : '',
    users: versions.includes('v3')
      ? (d.users || []).map((u) => ({
          name: (u.user || '').trim(),
          secLevel: u.secLevel,
          authProto: u.authProto,
          authPass: u.authPass,
          privProto: u.privProto,
          privPass: u.privPass,
        }))
      : [],
    engineId: '',
    engineBoots: 0,
    // Each destination with only what its version uses, as the users are.
    traps: {
      destinations: (d.traps?.destinations || []).map((t) => {
        const v3 = t.version === 'v3';
        const inform = t.version !== 'v1' && !!t.inform;
        return {
          id: t.id || '',
          host: hostOf(t.host),
          port: Number(t.port),
          version: t.version,
          inform,
          community: v3 ? '' : t.community,
          user: v3 ? (t.user || '').trim() : '',
          engineId: v3 && inform ? engineIdOf(t.engineId) : '',
        };
      }),
      onStart: !!d.traps?.onStart,
      onAuthFailure: !!d.traps?.onAuthFailure,
      schedules: (d.traps?.schedules || []).map((s) => ({
        notification: s.notification || '',
        every: Number(s.every),
        irregular: !!s.irregular,
      })),
    },
  };
}

/**
 * The models grouped by category, in the order Go lists them: a category comes
 * where its first model does.
 */
export function modelGroups(models) {
  const groups = [];
  for (const m of models || []) {
    let group = groups.find((g) => g.category === m.category);
    if (!group) {
      group = { category: m.category, models: [] };
      groups.push(group);
    }
    group.models.push(m);
  }
  return groups;
}

/** The categories the models are in, in the order Go lists them. */
export function categoriesOf(models) {
  return modelGroups(models).map((g) => g.category);
}

/**
 * The Lucide icon a model is drawn with when it has no picture of its own: its
 * category's. A custom model that came with an icon is drawn with that.
 */
export const CATEGORY_ICONS = {
  server: 'server',
  network: 'network',
  security: 'shield',
  wireless: 'wifi',
  storage: 'hard-drive',
  power: 'battery-charging',
  printing: 'printer',
  environment: 'thermometer',
  other: 'package',
};

export function categoryIcon(category) {
  return CATEGORY_ICONS[category] || CATEGORY_ICONS.other;
}

/** The model of that ID — undefined for a custom model since deleted. */
export function modelOf(models, id) {
  return (models || []).find((m) => m.id === id);
}

/**
 * What a model is called: a built-in one in the locale, a custom one in the
 * words of its file, which no locale knows. A model that is gone is called by
 * the ID its devices still carry.
 */
export function modelName(model, t, id = '') {
  if (!model) return id.replace(/^custom:/, '');
  return model.custom ? model.name : t(`simulator.model.${model.id}.name`);
}

export function modelDescription(model, t) {
  if (!model) return '';
  return model.custom ? model.description || '' : t(`simulator.model.${model.id}.description`);
}

/** Text as a search compares it: lower case, accents dropped — "ecran" finds "écran". */
export function fold(s) {
  return String(s ?? '').normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase();
}

/**
 * The models a search keeps, in the order given: those of the category, when
 * one is chosen, where every word of the query is found in what describes the
 * model — its name, vendor, description, category, ID, or a notification it
 * sends. "cisco bgp" finds the router; "onduleur" finds the UPS in French.
 */
export function filterModels(models, query, category, t) {
  const words = fold(query).split(/\s+/).filter(Boolean);
  return (models || []).filter((m) => {
    if (category && m.category !== category) return false;
    if (!words.length) return true;
    const text = fold([
      modelName(m, t), m.vendor, modelDescription(m, t), t(`simulator.category.${m.category}`), m.id,
      ...(m.notifications || []).map((n) => n.name),
    ].join(' '));
    return words.every((w) => text.includes(w));
  });
}

/** The devices made from a model: while there is one, the model is not deleted. */
export function devicesOfModel(devices, id) {
  return (devices || []).filter((d) => d.model === id);
}

/**
 * What to say about an import, as i18n keys: a line per model imported, updated
 * or refused, and one per warning about a model imported anyway.
 */
export function importReport(results) {
  const out = [];
  for (const r of results || []) {
    if (!r.success) {
      out.push({ key: 'simulator.models.failed', values: { file: r.file, error: r.error }, level: 'error' });
      continue;
    }
    out.push({
      key: r.replaced ? 'simulator.models.replaced' : 'simulator.models.imported',
      values: { name: r.name },
      level: 'success',
    });
    for (const w of r.warnings || []) {
      out.push({ key: `simulator.models.warning.${w.key}`, values: { name: r.name, detail: w.detail }, level: 'warning' });
    }
  }
  return out;
}

/** The first model an import brought in, which the picker then selects. */
export function firstImported(results) {
  return (results || []).find((r) => r.success)?.modelId || '';
}

/**
 * What to say about an import of simulated devices, as i18n keys: a line per
 * device imported or refused — or for the file, when it was refused whole —
 * and one per warning about a device imported anyway.
 */
export function deviceImportReport(results) {
  const out = [];
  for (const r of results || []) {
    if (!r.success) {
      out.push({ key: 'simulator.devices.failed', values: { name: r.name, error: r.error }, level: 'error' });
      continue;
    }
    out.push({ key: 'simulator.devices.imported', values: { name: r.name }, level: 'success' });
    for (const w of r.warnings || []) {
      out.push({ key: `simulator.devices.warning.${w.key}`, values: { name: r.name, detail: w.detail }, level: 'warning' });
    }
  }
  return out;
}

/** The categories a recorded model may be filed under: Go's customCategories. */
export const CUSTOM_CATEGORIES = Object.keys(CATEGORY_ICONS);

/** The targets a device may be recorded from: the addresses the settings list. */
export function recordableTargets(settings) {
  return getTargetsAsArray(settings?.targets || '');
}

/**
 * What SimulatorRecordDevice is asked: the device, reached as every request to
 * that target is — its profile, its own overrides or the defaults, through
 * getEffectiveSettings —, and what the model it makes is to be called.
 */
export function recordRequest(settings, target, { name, description = '', vendor = '', category = 'other' }) {
  return {
    ...buildSnmpRequest(getEffectiveSettings(settings, target), [target], ''),
    name,
    description,
    vendor,
    category,
  };
}

/** What a device of the model can send, as Go lists it. */
export function notificationsOf(models, modelId) {
  return (models || []).find((m) => m.id === modelId)?.notifications || [];
}

/** What a running device has sent, over all its destinations. */
export function trapActivity(traps) {
  const out = { sent: 0, failed: 0, dropped: 0, lastError: '' };
  for (const d of traps?.destinations || []) {
    out.sent += d.sent || 0;
    out.failed += d.failed || 0;
    out.dropped += d.dropped || 0;
    if (d.lastError) out.lastError = d.lastError;
  }
  return out;
}

/**
 * What to say about one notification sent on request, as i18n keys: one line
 * for everywhere it went, and one for each destination it did not reach — an
 * INFORM nobody acknowledged is one of those.
 */
export function deliveryReport(name, deliveries) {
  const ok = (deliveries || []).filter((d) => !d.error);
  const out = (deliveries || [])
    .filter((d) => d.error)
    .map((d) => ({ key: 'simulator.traps.failed', values: { name, destination: d.destination, error: d.error }, level: 'error' }));
  if (ok.length === 1) {
    const d = ok[0];
    out.unshift({
      key: d.acknowledged ? 'simulator.traps.acknowledged' : 'simulator.traps.sent',
      values: { name, destination: d.destination },
      level: 'success',
    });
  } else if (ok.length > 1) {
    out.unshift({ key: 'simulator.traps.sentAll', values: { name, count: ok.length }, level: 'success' });
  }
  return out;
}

/** The version a target reaches a device in: the most secure one it answers. */
export function preferredVersion(versions = []) {
  return ['v3', 'v2c', 'v1'].find((v) => versions.includes(v)) || 'v2c';
}

/**
 * The target a device is reached at: its address, and its port too unless that
 * is SNMP's own 161 — "127.0.0.1:1162", "[::1]:1161". On macOS, where every
 * device shares 127.0.0.1, the port is all that tells two devices apart, and a
 * target that names it is a target of its own.
 */
export function targetOf(device) {
  if (device.port === 161) return device.address;
  const host = device.address.includes(':') ? `[${device.address}]` : device.address;
  return `${host}:${device.port}`;
}

function targetOfLine(line) {
  return line.trim().replace(/^\/\//, '').split('#')[0].trim();
}

/**
 * The settings with a device added as a target: the target in the list, named
 * after the device, and an override carrying its identifiers in the most secure
 * version it answers — with its FIRST user for v3.
 *
 * The port travels in the target when it is not 161, and the override then
 * leaves it out: the target's would win over it anyway. A device added again
 * gives its target its identifiers once more instead of listing it twice;
 * `added` says which.
 */
export function addDeviceAsTarget(settings, device, creds) {
  const target = targetOf(device);
  const version = preferredVersion(device.versions);
  const override = { snmpVersion: version };
  if (target === device.address) override.port = device.port;
  if (version === 'v3') {
    const u = device.users[0];
    override.v3 = {
      user: u.name,
      secLevel: u.secLevel,
      authProto: u.authProto,
      authPass: creds?.users?.[u.name]?.authPass || '',
      privProto: u.privProto,
      privPass: creds?.users?.[u.name]?.privPass || '',
      contextName: '',
    };
  } else {
    override.community = creds?.community || '';
  }
  const lines = (settings.targets || '').split('\n').filter((l) => l.trim());
  const added = !lines.some((l) => targetOfLine(l) === target);
  const targets = added ? [...lines, `${target} # ${device.name}`].join('\n') : settings.targets;
  return {
    settings: { ...settings, targets, targetOverrides: { ...(settings.targetOverrides || {}), [target]: override } },
    added,
    version,
    target,
  };
}
