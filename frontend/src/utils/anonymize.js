import { derived } from 'svelte/store';
import { settingsStore } from '../stores/settingsStore';

/**
 * Derived store: true when anonymous mode is active.
 */
export const anonMode = derived(settingsStore, $s => !!$s.anonymousMode);

// Stable mappings (reset on page reload — by design)
const ipMap = new Map();
const hostMap = new Map();
let ipCounter = 1;
let hostCounter = 1;

/**
 * Reset all anonymous mappings (useful for testing).
 */
export function resetMappings() {
  ipMap.clear();
  hostMap.clear();
  ipCounter = 1;
  hostCounter = 1;
}

/**
 * Anonymize an IP address with a stable label.
 * Same IP always maps to the same "Device-N" within a session.
 */
export function anonymizeIp(ip) {
  if (!ip) return ip;
  if (!ipMap.has(ip)) {
    ipMap.set(ip, `Device-${ipCounter++}`);
  }
  return ipMap.get(ip);
}

/**
 * Anonymize a hostname with a stable label.
 */
export function anonymizeHost(name) {
  if (!name) return name;
  if (!hostMap.has(name)) {
    hostMap.set(name, `Host-${hostCounter++}`);
  }
  return hostMap.get(name);
}

/**
 * Replace all IPv4 addresses in free text with their anonymized labels.
 */
export function anonymizeText(text) {
  if (!text) return text;
  return String(text).replace(/\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b/g, (match) => {
    return anonymizeIp(match);
  });
}

/**
 * Mask a sensitive string (community, username, etc.).
 */
export function maskString(str) {
  if (!str) return str;
  return '\u2022\u2022\u2022\u2022\u2022\u2022';
}

/**
 * Anonymize a CIDR notation (mask the network part, keep prefix length).
 */
export function anonymizeCidr(cidr) {
  if (!cidr) return cidr;
  const parts = cidr.split('/');
  return anonymizeIp(parts[0]) + (parts[1] ? '/' + parts[1] : '');
}

/**
 * Mask sysDescr (device description that may reveal model/vendor).
 */
export function maskSysDescr(descr) {
  if (!descr) return descr;
  return '[device description]';
}
