import { anonymizeIp } from './anonymize';

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
 * Get effective settings for a specific target, merging global settings with per-target overrides.
 * @param {object} settings - The full $settingsStore value
 * @param {string} address - Target address
 * @returns {object} Merged settings
 */
export function getEffectiveSettings(settings, address) {
  const overrides = settings.targetOverrides?.[address];
  if (!overrides) return settings;
  return {
    ...settings,
    ...(overrides.community !== undefined && { community: overrides.community }),
    ...(overrides.snmpVersion !== undefined && { snmpVersion: overrides.snmpVersion }),
    ...(overrides.port !== undefined && { port: overrides.port }),
    ...(overrides.timeout !== undefined && { timeout: overrides.timeout }),
    ...(overrides.retries !== undefined && { retries: overrides.retries }),
    v3: { ...settings.v3, ...(overrides.v3 || {}) },
  };
}

/**
 * Group enabled targets by their effective SNMP config.
 * Returns groups that can each be sent as a single backend request.
 * @param {object} settings - The full $settingsStore value
 * @returns {{ targets: string[], effectiveSettings: object }[]}
 */
export function groupTargetsByConfig(settings) {
  const addresses = getTargetsAsArray(settings.targets);
  const groups = new Map();

  for (const addr of addresses) {
    const eff = getEffectiveSettings(settings, addr);
    const key = JSON.stringify({
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
