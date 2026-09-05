/**
 * Bindings whose answer has to be COMPUTED rather than listed.
 *
 * Hand-written, unlike App.js. Anything here takes precedence over the
 * generated fixtures, which is how a chart gets six hours of points anchored to
 * the present without six hours of JSON in the repository.
 */

import { series, CPU_SESSION } from './series.js';

let cpu = null;

export const dynamic = {
  // Generated once per page load, so every chart on the page agrees with itself
  // and with the statistics computed from it.
  MonitorLoadSessionData() {
    if (!cpu) cpu = series(CPU_SESSION);
    return cpu;
  },

  MonitorLoadHistoricalData() {
    if (!cpu) cpu = series(CPU_SESSION);
    return cpu;
  },
};

/**
 * The event journal, lengthened and anchored to the present.
 *
 * The specification gave eight events — the right MIX, which is the hard part,
 * but eight rows leave the lower half of a 1600x1000 capture empty, and an
 * application that looks unused is the one thing these pictures must not show.
 *
 * So the eight are cycled with fresh identities and descending timestamps. The
 * variety is the specification's; only the quantity is ours.
 */
import { EVENT_SEED } from './eventseed.js';

let journal = null;

function buildJournal() {
  const template = EVENT_SEED.items || [];
  if (!template.length) return EVENT_SEED;

  const items = [];
  const now = Date.now();
  let seq = 18422;

  for (let i = 0; i < 22; i++) {
    const base = template[i % template.length];
    // Irregular gaps: evenly spaced timestamps read as generated, because they
    // are — real events do not arrive on a metronome.
    const minutesBack = i * 7 + ((i * 13) % 11) + (i % 3) * 4;
    // Vary the device as well as the clock. Cycling eight templates unchanged
    // repeats a visible pattern down the column, which reads as generated.
    const fleet = ['10.20.4.11', '10.20.4.1', '10.20.4.77', '10.20.4.23',
                   '10.20.5.8', '192.168.30.5', '10.20.4.42', '10.20.7.3'];
    const source = base.source ? fleet[(i * 3 + 1) % fleet.length] : base.source;

    items.push({
      ...base,
      source,
      summary: base.source ? String(base.summary).split(base.source).join(source) : base.summary,
      seq: seq--,
      id: `${base.id.slice(0, 24)}-${String(i).padStart(2, '0')}`,
      ts: new Date(now - minutesBack * 60000).toISOString(),
      acked: i > 15,
    });
  }
  return { ...EVENT_SEED, items };
}

dynamic.EventsQuery = () => {
  if (!journal) journal = buildJournal();
  return journal;
};
