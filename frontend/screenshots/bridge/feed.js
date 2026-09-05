/**
 * Live feeds: runtime events that keep arriving, rather than a fixed script.
 *
 * `scene().events` is a list of payloads with a delay each, which is right for
 * "a trap arrives once, four seconds in". It cannot express "a sample per
 * series, every interval, for as long as anyone is watching" — and that is the
 * whole subject of the monitoring screen. A still can be seeded with six hours
 * of history; only a feed can show a chart MOVING.
 *
 * A feed is declared by a scene as `feed: { from: 'monitorSamples',
 * everyMs: 420 }`. The stubbed runtime starts it once, after the first
 * subscriber registers, and dispatches whatever it returns to the handlers for
 * each event name.
 */

import { ANCHOR, CPU_SESSION, shapeAt, isSilent } from './series.js';

/** Mulberry32 again — deterministic, so two recordings differ only in the code. */
function rng(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const SPAN = CPU_SESSION.hours * 3600 * 1000;
const noise = rng(9001);

/** The value each series was last given, so `delta` and `rate` mean something. */
const previous = new Map();

/**
 * One tick: a fresh sample for every series of the running session.
 *
 * Computed with the SAME shape function as the seeded history, at
 * `progress > 1`. That is what makes it a continuation rather than a second
 * series drawn beside the first — the earlier version generated its own values
 * from its own baselines and welded a dense jagged block onto the right-hand
 * edge of a smooth curve.
 *
 * The timestamp advances by the session's own interval per tick, not by wall
 * time, so ten seconds of clip shows what four hours of polling would.
 */
export function monitorSamples(tick) {
  const at = ANCHOR + (tick + 1) * CPU_SESSION.intervalMs;
  const progress = (at - (ANCHOR - SPAN)) / SPAN;
  const points = [];

  for (const t of CPU_SESSION.targets) {
    if (isSilent(t, progress)) continue;

    const value = Math.round(shapeAt(t, progress, noise() - 0.5) * 10) / 10;
    const was = previous.get(t.address);
    previous.set(t.address, value);

    points.push({
      target: t.address,
      timestamp: new Date(at).toISOString(),
      value,
      delta: was === undefined ? 0 : Math.round((value - was) * 10) / 10,
      rate: was === undefined
        ? 0
        : Math.round(((value - was) / (CPU_SESSION.intervalMs / 1000)) * 1000) / 1000,
      responseTimeMs: Math.round(11 + noise() * 26),
      error: null,
      snmpType: 'Gauge32',
      oid: CPU_SESSION.oid,
    });
  }

  return [{
    name: 'monitor:samples',
    payload: { sessionId: CPU_SESSION.sessionId, points },
  }];
}

export const FEEDS = { monitorSamples };
