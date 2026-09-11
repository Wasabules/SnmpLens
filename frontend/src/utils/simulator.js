/**
 * The simulator's rules on the renderer's side, kept out of the components so
 * they run under node (tests/simulator.test.mjs).
 *
 * Go is the authority on a device (simulator.Device.Validate, the loopback rule
 * included). What is here spares a round trip for the obvious, and decides the
 * one thing Go cannot: how a device becomes a TARGET, which lives in the
 * renderer's settings.
 *
 * The editor holds the SNMPv3 user in the shape UsmFields edits — `user`,
 * `secLevel`, the protocols and passphrases — and devicePayload turns it into
 * Go's `users` list.
 */
import { usesAuth, usesPriv } from './snmpSecurity.js';

/** The versions a device may answer, in the order they are shown. */
export const SIM_VERSIONS = ['v1', 'v2c', 'v3'];

/** The shortest passphrase a simulated user may have, as pkg/simulator holds it. */
export const MIN_PASSPHRASE = 8;

export function blankV3() {
  return { user: 'simulator', secLevel: 'AuthPriv', authProto: 'SHA256', authPass: '', privProto: 'AES', privPass: '', contextName: '' };
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
    v3: blankV3(),
  };
}

/**
 * A listed device in the editor's shape, its credentials filled in. The list
 * never carries them; they come from SimulatorDeviceCredentials.
 */
export function editableDevice(view, creds) {
  const u = view.users?.[0];
  return {
    id: view.id,
    name: view.name,
    model: view.model,
    address: view.address,
    port: view.port,
    versions: [...(view.versions || [])],
    community: creds?.community || '',
    v3: u
      ? {
          user: u.name,
          secLevel: u.secLevel,
          authProto: u.authProto || 'SHA256',
          authPass: creds?.users?.[u.name]?.authPass || '',
          privProto: u.privProto || 'AES',
          privPass: creds?.users?.[u.name]?.privPass || '',
          contextName: '',
        }
      : blankV3(),
  };
}

const usesCommunity = (versions) => versions.includes('v1') || versions.includes('v2c');

/**
 * What a device would be refused for, by field, as i18n key suffixes
 * (simulator.problem.<key>). Mirrors Device.Validate for what a form can get
 * wrong; Go still checks all of it, and the address.
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
    const u = d.v3 || {};
    if (!u.user?.trim()) problems.user = 'userRequired';
    if (usesAuth(u.secLevel) && (u.authPass || '').length < MIN_PASSPHRASE) problems.authPass = 'passphraseShort';
    if (usesPriv(u.secLevel) && (u.privPass || '').length < MIN_PASSPHRASE) problems.privPass = 'passphraseShort';
  }
  return problems;
}

/**
 * The device as SimulatorSaveDevice takes it: the community only if a version
 * uses one, the user only if v3 is answered. The engine is the backend's to
 * give, and is not sent.
 */
export function devicePayload(d) {
  const versions = SIM_VERSIONS.filter((v) => d.versions.includes(v));
  const u = d.v3 || {};
  return {
    id: d.id || '',
    name: d.name.trim(),
    model: d.model,
    address: d.address.trim(),
    port: Number(d.port),
    versions,
    community: usesCommunity(versions) ? d.community : '',
    users: versions.includes('v3')
      ? [{ name: (u.user || '').trim(), secLevel: u.secLevel, authProto: u.authProto, authPass: u.authPass, privProto: u.privProto, privPass: u.privPass }]
      : [],
    engineId: '',
    engineBoots: 0,
  };
}

/** The version a target reaches a device in: the most secure one it answers. */
export function preferredVersion(versions = []) {
  return ['v3', 'v2c', 'v1'].find((v) => versions.includes(v)) || 'v2c';
}

function addressOf(line) {
  return line.trim().replace(/^\/\//, '').split('#')[0].trim();
}

/**
 * The settings with a device added as a target: its address in the list, named
 * after it, and an override carrying its port and its identifiers in the most
 * secure version it answers.
 *
 * A target IS an address, so a device on an address already listed gives that
 * target its identifiers rather than adding a second line; `added` says which.
 */
export function addDeviceAsTarget(settings, device, creds) {
  const version = preferredVersion(device.versions);
  const override = { port: device.port, snmpVersion: version };
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
  const added = !lines.some((l) => addressOf(l) === device.address);
  const targets = added ? [...lines, `${device.address} # ${device.name}`].join('\n') : settings.targets;
  return {
    settings: { ...settings, targets, targetOverrides: { ...(settings.targetOverrides || {}), [device.address]: override } },
    added,
    version,
  };
}
