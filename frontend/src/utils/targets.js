import { anonymizeIp } from './anonymize';
import { DEFAULT_REF, findProfile, hasCustomCredentials, profileIdentity } from './credentialProfiles.js';

/**
 * Parse the multi-line targets string from settings into an array of IPs.
 * Skips empty lines, lines starting with // (disabled), and strips # labels.
 * @param {string} targetsString
 * @returns {string[]}
 */
export function getTargetsAsArray(targetsString) {
  if (!targetsString) return [];
  return targetsString.split('\n')
    .map(t => t.trim())
    .filter(t => t.length > 0 && !t.startsWith('//'))
    .map(t => t.split('#')[0].trim());
}

/**
 * Every configured target — enabled or not — with its label, once per address.
 *
 * A read-only view for the places that list targets without editing the text
 * they live in: the credential profiles editor, where a disabled target can
 * still be given a profile.
 * @param {string} targetsString
 * @returns {{ address: string, label: string, enabled: boolean }[]}
 */
export function parseTargetLines(targetsString) {
  if (!targetsString) return [];
  const out = [];
  const seen = new Set();
  for (const raw of targetsString.split('\n')) {
    const line = raw.trim();
    if (!line) continue;
    const enabled = !line.startsWith('//');
    const body = enabled ? line : line.slice(2).trim();
    const hash = body.indexOf('#');
    const address = (hash < 0 ? body : body.slice(0, hash)).trim();
    if (!address || seen.has(address)) continue;
    seen.add(address);
    out.push({ address, label: hash < 0 ? '' : body.slice(hash + 1).trim(), enabled });
  }
  return out;
}

/**
 * Map every configured target address to its label, when it has one.
 * Labels live in the same settings string as the addresses, as
 * "address # label"; disabled targets (prefixed //) keep their label so a
 * running session can still name them.
 * @param {string} targetsString
 * @returns {Record<string,string>}
 */
export function getTargetLabels(targetsString) {
  const labels = {};
  if (!targetsString) return labels;
  for (const raw of targetsString.split('\n')) {
    const line = raw.trim().replace(/^\/\//, '').trim();
    if (!line) continue;
    const hash = line.indexOf('#');
    if (hash < 0) continue;
    const address = line.slice(0, hash).trim();
    const label = line.slice(hash + 1).trim();
    if (address && label) labels[address] = label;
  }
  return labels;
}

/**
 * How a target address should READ on screen.
 *
 * An operator reads "core-sw-01" far faster than "10.20.0.1", and they gave that
 * name for a reason. The address is not thrown away — every caller puts it in
 * the element's `title`, so hovering still answers "which box is that".
 *
 * Anonymous Mode wins over both. A label names a site at least as plainly as an
 * address does, so masked output stays masked; showing "core-sw-01" while
 * hiding 10.20.0.1 would defeat the whole feature.
 *
 * This lived twice inside the monitoring components, which is why the labels
 * appeared on the charts and nowhere else: a walk's results, a trap's source and
 * a history entry's targets all showed raw addresses for configured devices that
 * had a name.
 *
 * @param {string} address
 * @param {Record<string,string>} labels  from the targetLabels store
 * @param {boolean} anon                  from the anonMode store
 */
export function displayTarget(address, labels, anon) {
  if (anon) return anonymizeIp(address);
  return (labels && labels[address]) || address;
}

/** What belongs in the `title` beside it: the address, or its mask. */
export function targetTitle(address, anon) {
  return anon ? anonymizeIp(address) : address;
}

/**
 * The settings a request to one target is built from.
 *
 * Identity first — who the request says it is — then transport. The identity
 * comes from exactly one place, in this order: the target's credential profile,
 * its own overrides (community, version, v3), or the default identifiers. A
 * profile id that names nothing — deleted while something still held the id —
 * falls back to the defaults, which is what the target would have had it never
 * been given one.
 *
 * `credentialRef` says which of the three it was, so a monitoring session can
 * record it and follow the profile afterwards: 'default', a profile id, or ''
 * for the target's own overrides.
 *
 * @param {object} settings - The full $settingsStore value
 * @param {string} address - Target address
 * @returns {object} Merged settings
 */
export function getEffectiveSettings(settings, address) {
  const overrides = settings.targetOverrides?.[address];
  if (!overrides) return { ...settings, credentialRef: DEFAULT_REF };

  const profile = findProfile(settings, overrides.profile);
  let identity;
  if (profile) {
    identity = { ...profileIdentity(profile), credentialRef: profile.id };
  } else if (hasCustomCredentials(overrides)) {
    identity = {
      ...(overrides.community !== undefined && { community: overrides.community }),
      ...(overrides.snmpVersion !== undefined && { snmpVersion: overrides.snmpVersion }),
      v3: { ...settings.v3, ...(overrides.v3 || {}) },
      credentialRef: '',
    };
  } else {
    identity = { credentialRef: DEFAULT_REF };
  }

  return {
    ...settings,
    ...identity,
    ...(overrides.port !== undefined && { port: overrides.port }),
    ...(overrides.timeout !== undefined && { timeout: overrides.timeout }),
    ...(overrides.retries !== undefined && { retries: overrides.retries }),
  };
}

/**
 * Group addresses by the connection a request to each would use, so that each
 * group can be sent as one backend request.
 *
 * The identity reference is part of the key, not only the values it resolves
 * to: two profiles holding the same credentials are still two groups, because a
 * monitoring session records which one it follows.
 *
 * @param {object} settings - The full $settingsStore value
 * @param {string[]} addresses
 * @returns {{ targets: string[], effectiveSettings: object }[]}
 */
export function groupTargets(settings, addresses) {
  const groups = new Map();

  for (const addr of addresses) {
    const eff = getEffectiveSettings(settings, addr);
    const key = JSON.stringify({
      ref: eff.credentialRef,
      community: eff.community,
      snmpVersion: eff.snmpVersion,
      port: eff.port,
      timeout: eff.timeout,
      retries: eff.retries,
      v3: eff.v3,
    });
    if (!groups.has(key)) {
      groups.set(key, { targets: [], effectiveSettings: eff });
    }
    groups.get(key).targets.push(addr);
  }

  return [...groups.values()];
}

/**
 * Whether any of these targets authenticates with something other than the
 * default identifiers — a profile or overrides of its own.
 * @param {object} settings
 * @param {string[]} addresses
 */
export function usesOwnIdentifiers(settings, addresses) {
  return addresses.some((address) => getEffectiveSettings(settings, address).credentialRef !== DEFAULT_REF);
}

/**
 * Group the enabled targets by their effective SNMP config.
 * @param {object} settings - The full $settingsStore value
 * @returns {{ targets: string[], effectiveSettings: object }[]}
 */
export function groupTargetsByConfig(settings) {
  return groupTargets(settings, getTargetsAsArray(settings.targets));
}
