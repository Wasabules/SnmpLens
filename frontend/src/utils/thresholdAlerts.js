/**
 * NOTE: alert DETECTION now lives in Go (pkg/monitor/breach.go), so that it
 * keeps working with the window closed and survives a webview reload. What
 * remains used here is `classify`, purely for the instant red state on a metric
 * tile — the tile must answer without a round-trip. Both implement the same
 * rule; if one changes, change the other.
 *
 * Threshold breach tracking with a dwell time.
 *
 * "Below 5 for 30s" is a different statement from "below 5": the first is an
 * incident, the second is often a single noisy poll. So a breach is tracked as
 * an episode — it starts when the value leaves the band, and only becomes an
 * alert once it has held for the configured duration. It then stays silent
 * until the value comes back inside, so one incident logs once rather than on
 * every sample.
 */

/** Which bound, if any, the value is outside of. */
export function classify(value, threshold) {
  if (!threshold || threshold.alertEnabled === false) return null;
  if (value === null || value === undefined || Number.isNaN(Number(value))) return null;

  const { min, max } = threshold;
  if (min !== null && min !== undefined && min !== '' && Number(value) < Number(min)) {
    return { kind: 'below', bound: Number(min) };
  }
  if (max !== null && max !== undefined && max !== '' && Number(value) > Number(max)) {
    return { kind: 'above', bound: Number(max) };
  }
  return null;
}

/**
 * Advance one series' breach state by one sample.
 *
 * @param breach     previous state, or null/undefined when none is in progress
 * @param sample     {value, timestamp}
 * @param threshold  {min, max, forSeconds, alertEnabled}
 * @returns {{breach: object|null, alert: object|null}}
 *          `breach` is the state to carry forward (null closes the episode),
 *          `alert` is non-null exactly once per episode.
 */
export function evaluateBreach(breach, sample, threshold) {
  const cls = classify(sample.value, threshold);

  // Back inside the band (or unreadable): the episode is over.
  if (!cls) return { breach: null, alert: null };

  const at = Date.parse(sample.timestamp);
  // A flip from below to above is a new episode, not a continuation.
  const current =
    breach && breach.kind === cls.kind ? breach : { kind: cls.kind, start: at, fired: false };

  const requiredMs = (Number(threshold.forSeconds) || 0) * 1000;
  const heldMs = at - current.start;

  if (heldMs < requiredMs || current.fired) return { breach: current, alert: null };

  return {
    breach: { ...current, fired: true },
    alert: {
      kind: cls.kind,
      bound: cls.bound,
      forSeconds: Number(threshold.forSeconds) || 0,
      heldSeconds: Math.round(heldMs / 1000),
    },
  };
}
