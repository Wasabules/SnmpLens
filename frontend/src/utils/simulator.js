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
 * Go's `users` list.
 */
import { usesAuth, usesPriv } from './snmpSecurity.js';

/** The versions a device may answer, in the order they are shown. */
export const SIM_VERSIONS = ['v1', 'v2c', 'v3'];

/** The shortest passphrase a simulated user may have, as pkg/simulator holds it. */
export const MIN_PASSPHRASE = 8;

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
  return problems;
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
  };
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
