// One rule decides what a reading SAYS and what colour it is.
//
// Two widgets show a state: the wall of cells and the map. They must agree —
// the same port cannot be green in one and grey in the other on the same
// dashboard, and that is exactly what two copies of a regular expression
// produce the first time one of them gains a word. `series.test.mjs` exists for
// the same class of defect on chart colours, where there were four rules and
// they disagreed whenever OIDs had uneven target counts.
//
// The label map decides the WORD; the number decides the colour only when the
// author gave no word for it. A state nobody named is shown as its number
// rather than as a guess.

/** The most recent point for one OID, or null. */
export function latestFor(oid, points) {
  let best = null;
  for (const p of points || []) {
    if (p.oid !== oid) continue;
    if (!best || p.timestamp > best.timestamp) best = p;
  }
  return best;
}

/** What a point is, before any naming: a value, an error, or nothing yet. */
export function stateOf(point) {
  if (!point) return { key: '', text: '—', kind: 'unknown' };
  if (point.error) return { key: '', text: point.error, kind: 'failed' };
  if (point.value === null || point.value === undefined) {
    return { key: '', text: '—', kind: 'unknown' };
  }
  return { key: String(Math.round(point.value)), text: '', kind: 'value' };
}

/** The word to show, through the author's own label map. */
export function stateText(point, labels) {
  const st = stateOf(point);
  if (st.kind !== 'value') return st.text;
  return (labels || {})[st.key] || st.key;
}

/**
 * The class that colours it.
 *
 * Read off the WORD the author chose, not off the number: 1 is up on
 * ifOperStatus and unknown on upsBatteryStatus, so a number cannot mean
 * anything on its own. A word nobody here recognises is `value` — shown, and
 * not claimed to be good or bad.
 */
export function stateKind(point, labels) {
  const st = stateOf(point);
  if (st.kind !== 'value') return st.kind;
  const word = ((labels || {})[st.key] || '').toLowerCase();
  if (/^(up|ok|on|active|normal|enabled|good|running)$/.test(word)) return 'good';
  if (/^(down|off|fail|failed|error|critical|inactive|disabled)$/.test(word)) return 'bad';
  if (/^(testing|unknown|dormant|warning|degraded)$/.test(word)) return 'warn';
  return 'value';
}
