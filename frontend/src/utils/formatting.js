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
