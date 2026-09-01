/**
 * Descriptive statistics for one monitored series.
 *
 * Computed over whichever points the caller passes in — that is what makes the
 * "visible window vs all data" choice meaningful: the same series answers
 * different questions depending on the span you ask about (a p95 over the last
 * minute is an incident, over a day it is a baseline).
 */

/** Stats offered in the picker, in display order. */
export const STAT_KEYS = ['last', 'avg', 'min', 'max', 'p95', 'stddev', 'count', 'errors'];

/** Sensible default: the value now, and the shape of what came before. */
export const DEFAULT_STATS = ['last', 'avg', 'min', 'max'];

// Linear-interpolated percentile over an already-sorted array.
function percentile(sorted, p) {
  if (!sorted.length) return null;
  if (sorted.length === 1) return sorted[0];
  const idx = (sorted.length - 1) * p;
  const lo = Math.floor(idx);
  const hi = Math.ceil(idx);
  if (lo === hi) return sorted[lo];
  return sorted[lo] + (sorted[hi] - sorted[lo]) * (idx - lo);
}

/**
 * @param points  data points of a single series, chronological
 * @param field   'value' | 'delta' | 'rate' | 'responseTimeMs'
 */
export function computeStats(points, field) {
  const values = [];
  let errors = 0;
  for (const p of points) {
    if (p.error || p.value === null) errors++;
    const v = p[field];
    if (v !== null && v !== undefined && !Number.isNaN(Number(v))) values.push(Number(v));
  }

  if (!values.length) {
    return { last: null, avg: null, min: null, max: null, p95: null, stddev: null, count: 0, errors };
  }

  const sorted = [...values].sort((a, b) => a - b);
  const avg = values.reduce((a, b) => a + b, 0) / values.length;
  const variance = values.reduce((a, v) => a + (v - avg) * (v - avg), 0) / values.length;

  return {
    last: values[values.length - 1],
    avg,
    min: sorted[0],
    max: sorted[sorted.length - 1],
    p95: percentile(sorted, 0.95),
    stddev: Math.sqrt(variance),
    count: values.length,
    errors,
  };
}

/** Keep only the points inside a visible x-window (ms epoch bounds). */
export function pointsInRange(points, range) {
  if (!range || range.min == null || range.max == null) return points;
  return points.filter((p) => {
    const t = Date.parse(p.timestamp);
    return t >= range.min && t <= range.max;
  });
}
