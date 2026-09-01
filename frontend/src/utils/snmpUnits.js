/**
 * SNMP-aware maths and formatting for the monitoring charts.
 *
 * Two things make monitoring numbers correct rather than merely plotted:
 *  - counters wrap (a Counter32 restarts at 0 past 2^32) and reset (agent
 *    reboot). Subtracting blindly turns both into huge negative spikes that
 *    wreck the chart scale, so `correctedDelta` fixes the wrap and reports a
 *    reset as null (a gap) instead of a spike.
 *  - a rate must divide by the time that ACTUALLY elapsed between two samples,
 *    not by the configured interval: polling jitter, slow agents and timeouts
 *    make the two differ, and the error goes straight into the rate.
 */

export const COUNTER32_MAX = 4294967296; // 2^32
export const COUNTER64_MAX = 18446744073709551616; // 2^64

export function isCounterType(type) {
  return /counter/i.test(type || '');
}

export function counterModulus(type) {
  return /64/.test(type || '') ? COUNTER64_MAX : COUNTER32_MAX;
}

/**
 * Delta between two samples, correcting a counter wrap.
 * Returns null when the change can only be explained by a counter reset —
 * the caller renders that as a gap rather than inventing a value.
 */
export function correctedDelta(prevValue, value, type) {
  const d = value - prevValue;
  if (d >= 0) return d;
  // Gauges legitimately decrease; only counters wrap.
  if (!isCounterType(type)) return d;
  const mod = counterModulus(type);
  const wrapped = d + mod;
  // A genuine wrap leaves a small positive delta. Anything approaching the
  // modulus means the counter was reset, not wrapped.
  return wrapped > 0 && wrapped < mod / 2 ? wrapped : null;
}

/** Seconds actually elapsed between two ISO timestamps (null if unusable). */
export function elapsedSeconds(prevTimestamp, timestamp) {
  const dt = (new Date(timestamp) - new Date(prevTimestamp)) / 1000;
  return Number.isFinite(dt) && dt > 0 ? dt : null;
}

// --- Units -----------------------------------------------------------------

const DECIMAL = ['', 'k', 'M', 'G', 'T', 'P'];
const BINARY = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];

function scale(value, steps, divisor, suffix, digits) {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  const sign = value < 0 ? '-' : '';
  let n = Math.abs(value);
  let i = 0;
  while (n >= divisor && i < steps.length - 1) {
    n /= divisor;
    i++;
  }
  const shown = i === 0 && Number.isInteger(n) ? String(n) : n.toFixed(digits);
  return `${sign}${shown} ${steps[i]}${suffix}`;
}

/** Human duration from TimeTicks (hundredths of a second). */
function formatTimeTicks(ticks) {
  if (ticks === null || ticks === undefined || Number.isNaN(ticks)) return '—';
  let s = Math.floor(Math.abs(ticks) / 100);
  const d = Math.floor(s / 86400); s %= 86400;
  const h = Math.floor(s / 3600); s %= 3600;
  const m = Math.floor(s / 60); s %= 60;
  if (d) return `${d}j ${h}h ${m}m`;
  if (h) return `${h}h ${m}m ${s}s`;
  if (m) return `${m}m ${s}s`;
  return `${s}s`;
}

const OCTET_RE = /octets|bytes|bandwidth/i;
const PERCENT_RE = /percent|pct|utili|usage|load/i;

/**
 * Decide how to label and format a metric from its OID name, SNMP type and the
 * current view mode ('raw' | 'delta' | 'rate' | 'latency').
 * Returns { label, format(value) }.
 */
export function inferUnit(oidName = '', snmpType = '', mode = 'raw') {
  const name = String(oidName);

  if (mode === 'latency') {
    return { label: 'ms', format: (v) => (v == null ? '—' : `${Number(v).toFixed(0)} ms`) };
  }

  if (/timeticks/i.test(snmpType) && mode === 'raw') {
    return { label: 'durée', format: formatTimeTicks };
  }

  if (OCTET_RE.test(name)) {
    if (mode === 'rate') {
      // Octets per second read as a link rate: show bits, the unit interfaces
      // are actually rated in.
      return { label: 'bit/s', format: (v) => (v == null ? '—' : scale(v * 8, DECIMAL, 1000, 'bit/s', 1)) };
    }
    if (mode === 'delta') {
      return { label: 'octets', format: (v) => (v == null ? '—' : scale(v, BINARY, 1024, '', 1)) };
    }
    return { label: 'octets', format: (v) => (v == null ? '—' : scale(v, BINARY, 1024, '', 1)) };
  }

  if (PERCENT_RE.test(name) && mode === 'raw') {
    return { label: '%', format: (v) => (v == null ? '—' : `${Number(v).toFixed(1)} %`) };
  }

  if (mode === 'rate') {
    return { label: '/s', format: (v) => (v == null ? '—' : scale(v, DECIMAL, 1000, '/s', 2)) };
  }

  return { label: '', format: (v) => (v == null ? '—' : scale(v, DECIMAL, 1000, '', 1)) };
}
