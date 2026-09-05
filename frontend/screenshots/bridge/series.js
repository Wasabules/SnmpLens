/**
 * Time series for the monitoring charts.
 *
 * The specification gave the SHAPE of a data point but only a handful of them,
 * which draws an axis, a legend, a threshold line — and no curves. A chart with
 * two points per series is indistinguishable from a broken one, and it is the
 * thing the site most needs to show.
 *
 * So the points are generated rather than listed. Three properties matter:
 *
 *   DETERMINISTIC. A seeded generator, never Math.random, so re-running the
 *   harness produces the same image and a diff in git means something changed.
 *
 *   RELATIVE TO NOW. The chart's default range is anchored to the present, so
 *   fixed timestamps drift out of view and the curves silently vanish some time
 *   after they were written.
 *
 *   SHAPED LIKE REAL TRAFFIC. A flat line proves the chart renders; it does not
 *   show what the tool is for. These have a daily rhythm, per-device baselines,
 *   one device climbing through its threshold, and one dropping out entirely.
 */

/** Mulberry32: small, fast, and the same everywhere. */
function rng(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/**
 * @param {object} opts
 * @param {string} opts.sessionId
 * @param {string} opts.oid
 * @param {number} opts.hours       how far back to go
 * @param {number} opts.intervalMs  the poll interval
 * @param {Array}  opts.targets     {address, base, swing, seed, story}
 */
/**
 * The moment every series ends, fixed once at module load.
 *
 * Both the seeded history and the live feed have to agree on it. Reading
 * Date.now() in each would put them minutes apart — the history ends when the
 * page loads, the feed starts when the first subscriber registers — and the
 * chart would show a gap, or worse, an overlap where two lines disagree about
 * the same instant.
 */
export const ANCHOR = Date.now();

/**
 * The shape of one series at one moment, as a function of `progress`, where 0 is
 * the start of the seeded window and 1 is now.
 *
 * Exported and shared with the live feed, which calls it with progress > 1. That
 * is the whole point of it being a function: a continuation computed by
 * different code from the history it continues shows a visible seam, and the
 * first version of the feed did exactly that — a dense jagged block welded onto
 * the right-hand edge of a smooth curve.
 */
export function shapeAt(t, progress, jitter) {
  // A daily rhythm, plus a slower swell, so no two peaks look alike.
  const daily = Math.sin(progress * Math.PI * 1.6 + t.phase);
  const swell = Math.sin(progress * Math.PI * 5.1 + t.phase * 2) * 0.35;
  let value = t.base + (daily + swell) * t.swing;

  // The stories the chart is there to tell.
  if (t.story === 'climbing' && progress > 0.62) {
    // Crosses the threshold and stays over it: an incident, not a blip. The
    // climb SATURATES rather than continuing for ever — unbounded, the live
    // continuation pinned it against the 100 % ceiling within a minute and drew
    // a flat line, which says nothing at all about a device in trouble.
    value += Math.min(progress - 0.62, 0.34) * 120;
  }
  if (t.story === 'spike') {
    // A shaped burst, not a rectangle: it climbs, peaks and decays. A
    // constant added over a range draws a box, which reads as a rendering
    // artefact rather than as traffic.
    const centre = 0.43;
    const width = 0.035;
    const d = (progress - centre) / width;
    value += 34 * Math.exp(-d * d) * (d < 0 ? 1 : 0.72);
  }

  return Math.max(0, Math.min(100, value + jitter * 3.2));
}

/** True while a 'silent' device is not answering. */
export function isSilent(t, progress) {
  return t.story === 'silent' && progress > 0.78 && progress < 0.86;
}

export function series({ sessionId, oid, hours = 6, intervalMs = 60000, targets }) {
  const end = ANCHOR;
  const start = end - hours * 3600 * 1000;
  const points = [];

  for (const t of targets) {
    const rand = rng(t.seed);
    let previous = null;

    for (let at = start; at <= end; at += intervalMs) {
      const progress = (at - start) / (end - start);

      if (isSilent(t, progress)) {
        // The device stopped answering. A gap, not a zero — plotting a zero
        // would read as "the counter fell off a cliff", which is a different
        // and much more alarming thing.
        previous = null;
        continue;
      }

      const value = shapeAt(t, progress, rand() - 0.5);
      const rounded = Math.round(value * 10) / 10;

      points.push({
        sessionId,
        target: t.address,
        oid,
        timestamp: new Date(at).toISOString(),
        value: rounded,
        delta: previous === null ? 0 : Math.round((rounded - previous) * 10) / 10,
        rate: previous === null ? 0 : Math.round(((rounded - previous) / (intervalMs / 1000)) * 1000) / 1000,
        responseTimeMs: Math.round(11 + rand() * 26),
        snmpType: 'Gauge32',
      });
      previous = rounded;
    }
  }

  // Chronological across all targets, the way the database returns them.
  points.sort((a, b) => a.timestamp.localeCompare(b.timestamp));
  return points;
}

/** The session the site's monitoring screenshot shows. */
export const CPU_SESSION = {
  sessionId: '7c1f9a24-3d5e-4b81-9f02-6ab3c1de5f40',
  oid: '1.3.6.1.2.1.25.3.3.1.2.1',
  hours: 6,
  intervalMs: 60000,
  targets: [
    { address: '10.12.4.1', base: 38, swing: 9, phase: 0.0, seed: 101, story: 'silent' },
    { address: '10.12.4.2', base: 31, swing: 7, phase: 2.1, seed: 202, story: 'spike' },
    { address: '10.12.4.7', base: 46, swing: 11, phase: 4.3, seed: 303, story: 'climbing' },
    // Two more, so the chart shows what watching a rack looks like rather than
    // what watching three devices looks like. Different phases, so the curves
    // separate instead of moving as one.
    { address: '10.12.4.3', base: 24, swing: 6, phase: 1.2, seed: 404, story: null },
    { address: '10.12.4.9', base: 57, swing: 8, phase: 5.6, seed: 505, story: null },
  ],
};
