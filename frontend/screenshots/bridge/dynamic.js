/**
 * Bindings whose answer has to be COMPUTED rather than listed.
 *
 * Hand-written, unlike App.js. Anything here takes precedence over the
 * generated fixtures, which is how a chart gets six hours of points anchored to
 * the present without six hours of JSON in the repository.
 */

import { series, CPU_SESSION } from './series.js';

/**
 * The generated fixture table, handed over by App.js at module init.
 *
 * A computed answer sometimes needs to AMEND a fixture rather than replace it:
 * the history list wants a second walk added to diff against and its other four
 * entries left exactly as they are. Importing App.js here would be a cycle, and
 * copying the entries would leave two versions of them to keep in step.
 */
let FIXTURES = {};
export function setFixtures(table) {
  FIXTURES = table || {};
}

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

function ifTableWalk(after = 0) {
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

    // `after` is minutes elapsed since the first walk. The later walk moves
    // exactly what a later walk would: the access port on floor 2 south was
    // down and has come back up, its counters start moving with it, and the
    // rest of the table is byte-for-byte identical. That last part is what
    // makes the diff READABLE — a picture in which every row changed says
    // nothing about what the feature is for, and this one reports 36 modified
    // against 119 identical.
    const recovered = after > 0 && iface.descr.indexOf('floor 2 south') > 0;
    const up = iface.oper === 1 || recovered;
    const secs = after * 60;

    // The row the whole picture is about: 2 (down) becoming 1 (up). It is
    // column 8, so the diff lists it FIRST, above the counters it caused.
    add(8, n, 'Integer', up ? 1 : iface.oper);
    add(9, n, 'TimeTicks', recovered ? 41207 : 106238300 + secs * 100 - i * 41207);
    // The counters, which are what anyone actually walks ifTable for.
    add(10, n, 'Counter32', up ? 418923400 + i * 9137711 + secs * 121400 : 0);
    add(16, n, 'Counter32', up ? 391004822 + i * 8412093 + secs * 98750 : 0);
    add(14, n, 'Counter32', up ? i * 3 : 0);
    add(20, n, 'Counter32', up ? i : 0);
  });

  return [{
    target: '10.20.0.1',
    responseTimeMs: after ? 388 : 412,
    result: { oid: '1.3.6.1.2.1.2.2', type: 'WalkResponse', value: vars },
  }];
}

let walk = null;
dynamic.SnmpWalk = () => {
  if (!walk) walk = ifTableWalk();
  return walk;
};

/**
 * The history list, with a second walk to diff the first against.
 *
 * The fixture carried one WALK, and `isDiffEligible` accepts only WALK and
 * GETBULK — so Diff Mode could select exactly one entry and Compare never
 * appeared. `DiffModal` reads `entry.results` straight off each entry, which the
 * fixture's walk did not have either.
 *
 * The pair is forty minutes apart on the same OID and the same target, because
 * that is the question the feature answers: what changed on this device since I
 * last looked.
 */
dynamic.LoadHistory = () => {
  const base = FIXTURES.LoadHistory || [];
  const first = base.find((e) => e.operation === 'WALK');
  if (!first) return undefined;

  const withResults = (entry, minutes, id, when) => ({
    ...entry,
    id,
    timestamp: when,
    targets: ['10.20.0.1'],
    duration: minutes ? 388 : 412,
    results: ifTableWalk(minutes),
    totalResults: 155,
  });

  const t0 = new Date(first.timestamp).getTime();
  return [
    withResults(first, 40, `${first.id}-b`, new Date(t0 + 40 * 60000).toISOString()),
    withResults(first, 0, first.id, first.timestamp),
    ...base.filter((e) => e !== first),
  ];
};

/**
 * The MIB the editor screenshot has open.
 *
 * The specification's `content` was a DESCRIPTION of the file rather than the
 * file — "<<the 90-line ACME-POE-MIB printed verbatim in visualDirection>>" —
 * so opening it showed that sentence in a syntax highlighter. The text is here
 * for real, imported rather than pasted into a JS string, because the diagnostic
 * positions are derived from it: line 18 column 17 is exactly where `Integerr32`
 * starts, and line 32 is the second `::= { acmePoeObjects 1 }`. A stray edit to
 * the text moves the squiggles off the thing they are pointing at.
 */
import ACME_POE_MIB from './acme-poe-mib.txt?raw';

// The copy ON DISK is sound: `SYNTAX Gauge32`. The unsaved draft below is what
// introduced `Integerr32`, and that difference is what makes the editor recover
// the draft — which is also the only path that runs the analysis on open. So one
// picture shows two things: work surviving a closed window, and the semantic
// pass catching what loading alone would not.
const ON_DISK = ACME_POE_MIB.split('\n')
  .map((line, i) => (i === 17 ? '    SYNTAX      Gauge32' : line))
  .join('\n');

dynamic.MibEditorReadDraft = () => ACME_POE_MIB;

dynamic.MibEditorRead = () => ({
  name: 'ACME-POE-MIB',
  // Escaped: unescaped, JavaScript drops each backslash it does not recognise
  // and the path renders as "C:Usersops...".
  path: 'C:\\Users\\ops\\AppData\\Roaming\\SnmpLens\\mibs\\ACME-POE-MIB',
  content: ON_DISK,
  eol: 'lf',
  bundled: false,
  external: false,
  sha256: 'cfbca6d794927294d050e9b093af1dc70fd065f671855aceece1f0e5797c3562',
  diagnostics: [],
});

/**
 * A CIDR sweep that found something.
 *
 * Two things were wrong with the specification's set. The scan is never RUN —
 * the results are the component's own state, like a walk's — and the devices
 * were on 10.20.x.x while the form's default range is 192.168.1.0/24, so the
 * picture would have shown a scan of one subnet returning addresses from
 * another. A reader who knows SNMP is exactly the reader who would notice.
 *
 * The estate is the kind a small site actually has: routers and switches from
 * different vendors, the wireless, and the three things people forget are on
 * the network until they answer an SNMP sweep — the printer, the UPS and the
 * NAS. The sysDescr strings are the real shapes those devices return, because
 * a screenshot of a discovery tool is partly a screenshot of sysDescr.
 */
const DISCOVERED = [
  ['192.168.1.1', 'edge-rtr-01', 'Cisco IOS Software, ISR4331 Software (X86_64_LINUX_IOSD-UNIVERSALK9-M), Version 17.09.04a, RELEASE SOFTWARE (fc3)', 1099992310, 4],
  ['192.168.1.2', 'core-sw-01', 'Cisco IOS Software, C9300 Software (CAT9K_IOSXE), Version 17.12.03, RELEASE SOFTWARE (fc2)', 1043118800, 3],
  ['192.168.1.3', 'core-sw-02', 'Cisco IOS Software, C9300 Software (CAT9K_IOSXE), Version 17.12.03, RELEASE SOFTWARE (fc2)', 1043092140, 3],
  ['192.168.1.10', 'dist-sw-2f', 'Aruba JL255A 2930F-24G-4SFP+ Switch, revision WC.16.11.0013, ROM WC.16.01.0006', 884419770, 6],
  ['192.168.1.11', 'dist-sw-3f', 'Aruba JL255A 2930F-24G-4SFP+ Switch, revision WC.16.11.0013, ROM WC.16.01.0006', 884401200, 7],
  ['192.168.1.20', 'wifi-ctrl', 'MikroTik RouterOS 7.15.3 (stable) CRS328-24P-4S+', 612334880, 5],
  ['192.168.1.21', 'ap-floor2-n', 'Ubiquiti UniFi U6-Pro, firmware 6.6.77.15402', 388120450, 11],
  ['192.168.1.22', 'ap-floor3-n', 'Ubiquiti UniFi U6-Pro, firmware 6.6.77.15402', 388102190, 12],
  ['192.168.1.40', 'prn-acct-01', 'HP LaserJet MFP M528dn, Firmware 2409074_000587, Serial CNBRQ1234X', 219044100, 24],
  ['192.168.1.50', 'ups-server-room', 'APC Smart-UPS SRT 5000 RM, Network Management Card AOS v7.1.2', 447882300, 9],
  ['192.168.1.60', 'nas-backup-01', 'Linux nas-backup-01 4.4.302+ #72806 SMP x86_64 DiskStation DS1821+', 302118940, 8],
  ['192.168.1.80', 'esxi-host-02', 'VMware ESXi 8.0.2 build-23305546 VMware, Inc. x86_64', 154099220, 14],
  ['192.168.1.90', 'mon-collector', 'Linux mon-collector 6.8.0-45-generic #45-Ubuntu SMP x86_64', 88220110, 2],
];

dynamic.SnmpDiscover = () =>
  DISCOVERED.map(([ip, sysName, sysDescr, sysUpTime, responseTime]) => ({
    ip, sysName, sysDescr,
    sysUpTime: String(sysUpTime),
    responseTime,
    reachable: true,
  }));

/**
 * OID → name, for the varbind tables.
 *
 * `Trap.svelte` calls `GetOidDetails` once per varbind and puts the answer in
 * the Name column. Every binding used to be generated with an empty parameter
 * list, so all six calls got the same fixture back and the trap screenshot read
 * `linkDown` six times beside six plainly different OIDs — the one thing a
 * picture of an SNMP tool must not get wrong, because its whole claim is that it
 * resolves OIDs through your MIBs.
 *
 * Only the OIDs the fixtures actually contain are listed. An unknown OID falls
 * back to the static fixture, which is what the trap-OID varbind wants anyway.
 */
const OID_NAMES = {
  '1.3.6.1.2.1.1.3.0': ['sysUpTime', 'The time since the network management portion of the system was last re-initialized.'],
  '1.3.6.1.6.3.1.1.4.1.0': ['snmpTrapOID', 'The authoritative identification of the notification currently being sent.'],
  '1.3.6.1.2.1.2.2.1.1.3': ['ifIndex', 'A unique value, greater than zero, for each interface.'],
  '1.3.6.1.2.1.2.2.1.2.3': ['ifDescr', 'A textual string containing information about the interface.'],
  '1.3.6.1.2.1.2.2.1.7.3': ['ifAdminStatus', 'The desired state of the interface.'],
  '1.3.6.1.2.1.2.2.1.8.3': ['ifOperStatus', 'The current operational state of the interface.'],
  '1.3.6.1.4.1.318.2.3.3.0': ['upsAdvStateSummary', 'A summary of the UPS state, as a display string.'],
  '1.3.6.1.4.1.318.1.1.1.2.2.1.0': ['upsAdvBatteryCapacity', 'The remaining battery capacity, as a percentage of full capacity.'],
  '1.3.6.1.4.1.318.1.1.1.2.2.3.0': ['upsAdvBatteryRunTimeRemaining', 'The UPS battery run time remaining before battery exhaustion.'],
  '1.3.6.1.2.1.1.1.0': ['sysDescr', 'A textual description of the entity.'],
  '1.3.6.1.2.1.1.5.0': ['sysName', 'An administratively-assigned name for this managed node.'],

  // The NOTIFICATION OIDs — the values of the snmpTrapOID varbind, resolved a
  // second time to fill the Message column. Without these all five rows read
  // "linkDown", including the one from a UPS, which is a picture of an SNMP
  // tool failing at the one thing it claims to do.
  '1.3.6.1.6.3.1.1.5.1': ['coldStart', 'The agent is reinitialising and its configuration may have altered.'],
  '1.3.6.1.6.3.1.1.5.3': ['linkDown', 'A communication link is about to enter the down state.'],
  '1.3.6.1.6.3.1.1.5.4': ['linkUp', 'A communication link has come up.'],
  '1.3.6.1.4.1.318.0.5': ['upsOnBattery', 'The UPS is operating on battery: utility power has failed.'],
  '1.3.6.1.4.1.9': ['ciscoEnterprise', 'An enterprise-specific notification from a Cisco agent.'],
};

dynamic.GetOidDetails = (oid) => {
  const key = String(oid || '').replace(/^\./, '');
  const hit = OID_NAMES[key];
  if (!hit) return undefined; // no opinion: answer() falls through to the fixture
  return { name: hit[0], description: hit[1] };
};
