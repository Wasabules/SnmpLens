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
