/**
 * Format an ISO timestamp as a full date/time string (French locale).
 * @param {string} isoString
 * @returns {string}
 */
export function formatTimestamp(isoString) {
  const date = new Date(isoString);
  return date.toLocaleString('fr-FR', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  });
}

/**
 * Format a short time-only string (for charts / monitoring).
 * @param {string|number} ts
 * @returns {string}
 */
export function formatTimeShort(ts) {
  return new Date(ts).toLocaleTimeString();
}

/**
 * Format a duration in ms to a human-readable string.
 * @param {number} ms
 * @returns {string}
 */
export function formatDuration(ms) {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

/**
 * Format SNMP TimeTicks (hundredths of a second) to human-readable duration.
 * e.g. 1099992310 → "127d 3h 46m 23s"
 * @param {number} centiseconds
 * @returns {string}
 */
export function formatTimeTicks(centiseconds) {
  if (centiseconds == null || isNaN(centiseconds)) return String(centiseconds);
  const totalSeconds = Math.floor(Number(centiseconds) / 100);
  if (totalSeconds < 0) return String(centiseconds);

  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  const parts = [];
  if (days > 0) parts.push(`${days}d`);
  if (hours > 0 || days > 0) parts.push(`${hours}h`);
  if (minutes > 0 || hours > 0 || days > 0) parts.push(`${minutes}m`);
  parts.push(`${seconds}s`);

  return parts.join(' ');
}

/**
 * Format a large number with locale-aware thousand separators.
 * e.g. 1099992310 → "1,099,992,310"
 * @param {number} num
 * @returns {string}
 */
export function formatLargeNumber(num) {
  if (num == null || isNaN(num)) return String(num);
  return Number(num).toLocaleString();
}

/**
 * Apply smart formatting to an SNMP value based on its type.
 * Returns null if no special formatting applies (caller should fall back).
 * @param {*} value
 * @param {string} snmpType - e.g. "TimeTicks", "Counter32", "Gauge32"
 * @returns {string|null}
 */
export function formatBySnmpType(value, snmpType) {
  if (value == null || snmpType == null) return null;

  const num = Number(value);
  if (isNaN(num)) return null;

  if (snmpType === 'TimeTicks') {
    return formatTimeTicks(num) + ` (${formatLargeNumber(num)})`;
  }

  // Large numeric values get thousand separators for readability
  if (Math.abs(num) >= 10000) {
    return formatLargeNumber(num);
  }

  return null;
}
