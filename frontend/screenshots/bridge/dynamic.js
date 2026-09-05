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

/**
 * A walk of ifTable, twelve interfaces deep.
 *
 * The specification's example carried two, which renders a table with two rows —
 * enough to prove the pivot works and not enough to show what it is FOR. The
 * point of the table view is that a walk of two hundred varbinds becomes
 * something readable, so the picture has to have two hundred varbinds in it.
 *
 * The columns and their types are the specification's; only the row count and
 * the plausibility of each device are ours.
 */
const IF_BASE = '.1.3.6.1.2.1.2.2.1';

const INTERFACES = [
  { descr: 'Loopback0', type: 24, mtu: 1500, speed: 0, mac: '', admin: 1, oper: 1 },
  { descr: 'GigabitEthernet0/0/1 - uplink to core-rtr-01 Te0/0/3, 1000BASE-LX single-mode', type: 6, mtu: 1500, speed: 1000000000, mac: '00:1B:21:3C:4D:5E', admin: 1, oper: 1 },
  { descr: 'GigabitEthernet0/0/2 - uplink to core-rtr-02 Te0/0/3', type: 6, mtu: 1500, speed: 1000000000, mac: '00:1B:21:3C:4D:5F', admin: 1, oper: 1 },
  { descr: 'GigabitEthernet0/1/1 - access, floor 2 north', type: 6, mtu: 1500, speed: 1000000000, mac: '00:1B:21:3C:4D:60', admin: 1, oper: 1 },
  { descr: 'GigabitEthernet0/1/2 - access, floor 2 south', type: 6, mtu: 1500, speed: 1000000000, mac: '00:1B:21:3C:4D:61', admin: 1, oper: 2 },
  { descr: 'GigabitEthernet0/1/3 - access, floor 3 north', type: 6, mtu: 1500, speed: 100000000, mac: '00:1B:21:3C:4D:62', admin: 1, oper: 1 },
  { descr: 'GigabitEthernet0/1/4 - spare', type: 6, mtu: 1500, speed: 0, mac: '00:1B:21:3C:4D:63', admin: 2, oper: 2 },
  { descr: 'TenGigabitEthernet1/0/1 - to spine-01', type: 6, mtu: 9216, speed: 10000000000, mac: '00:1B:21:3C:4D:70', admin: 1, oper: 1 },
  { descr: 'TenGigabitEthernet1/0/2 - to spine-02', type: 6, mtu: 9216, speed: 10000000000, mac: '00:1B:21:3C:4D:71', admin: 1, oper: 1 },
  { descr: 'Port-channel1 - LACP to spine pair', type: 161, mtu: 9216, speed: 20000000000, mac: '00:1B:21:3C:4D:72', admin: 1, oper: 1 },
  { descr: 'Vlan10 - management', type: 53, mtu: 1500, speed: 1000000000, mac: '00:1B:21:3C:4D:80', admin: 1, oper: 1 },
  { descr: 'Vlan240 - guest wireless', type: 53, mtu: 1500, speed: 1000000000, mac: '00:1B:21:3C:4D:81', admin: 1, oper: 1 },
];

function ifTableWalk() {
  const vars = [];
  const add = (col, index, type, value) =>
    vars.push({ oid: `${IF_BASE}.${col}.${index}`, type, value });

  INTERFACES.forEach((iface, i) => {
    const n = i + 1;
    add(1, n, 'Integer', n);
    add(2, n, 'OctetString', iface.descr);
    add(3, n, 'Integer', iface.type);
    add(4, n, 'Integer', iface.mtu);
    add(5, n, 'Gauge32', iface.speed);
    if (iface.mac) add(6, n, 'OctetString', iface.mac);
    add(7, n, 'Integer', iface.admin);
    add(8, n, 'Integer', iface.oper);
    add(9, n, 'TimeTicks', 106238300 - i * 41207);
    // The counters, which are what anyone actually walks ifTable for.
    add(10, n, 'Counter32', iface.oper === 1 ? 418923400 + i * 9137711 : 0);
    add(16, n, 'Counter32', iface.oper === 1 ? 391004822 + i * 8412093 : 0);
    add(14, n, 'Counter32', iface.oper === 1 ? i * 3 : 0);
    add(20, n, 'Counter32', iface.oper === 1 ? i : 0);
  });

  return [{
    target: '10.20.0.1',
    responseTimeMs: 412,
    result: { oid: '1.3.6.1.2.1.2.2', type: 'WalkResponse', value: vars },
  }];
}

let walk = null;
dynamic.SnmpWalk = () => {
  if (!walk) walk = ifTableWalk();
  return walk;
};
