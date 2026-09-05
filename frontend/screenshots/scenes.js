/**
 * The scene catalogue: one entry per screenshot the site needs, in BOTH themes.
 *
 * A scene is a URL, not a script. `?scene=monitor-charts-dark` seeds
 * localStorage before the bundle evaluates, picks the tab, and may override any
 * bridge fixture — so a capture is a plain page load with nothing to click and
 * nothing to time.
 *
 * That matters more than it sounds. Driving a UI by clicking is how screenshot
 * automation becomes flaky: a selector moves, a transition is half-finished, and
 * the image is wrong in a way nobody notices until it is on the front page.
 * Here the only thing that can vary between two runs of the same scene is the
 * clock, and the fixtures pin that too.
 *
 * Every entry is declared ONCE and emitted twice, dark and light. The site shows
 * whichever matches the reader's own theme, so a missing counterpart is not a
 * missing picture — it is a picture of the wrong application on half the screens
 * that load the page. Declaring the pair by hand is how one of the two silently
 * drifts from the other.
 */

/** The seven tabs, as App.svelte names them. */
export const TABS = {
  operations: 'operations',
  traps: 'traps',
  history: 'history',
  monitor: 'monitor',
  discovery: 'discovery',
  events: 'events',
  mibeditor: 'mibeditor',
};

/** The two themes every scene is captured in. */
export const THEMES = ['dark', 'light'];

/**
 * Seeds every scene starts from: a configured, used installation.
 *
 * English is forced rather than detected. setupI18n reads `settings.locale`
 * straight out of localStorage before anything is decrypted, so a plaintext blob
 * is read correctly — and without this the shots come out in whatever language
 * the capturing machine happens to be set to, which for this one is French.
 */
function baseSeeds(seeds) {
  return {
    ...seeds,
    snmplens_panel_collapsed: '0',
    snmplens_panel_width: '400',
  };
}

/** Merge a settings patch into the seeded settings blob. */
function withSettings(base, patch) {
  let obj = {};
  try {
    obj = JSON.parse(base.settings || '{}');
  } catch {
    obj = {};
  }
  return JSON.stringify({ ...obj, locale: 'en', ...patch });
}

/** One scene. `tab` picks the workspace; `theme` picks dark or light. */
function scene(base, name, { tab, theme = 'dark', width = 1600, height = 1000, settings = {}, seeds: extra = {}, bindings = {}, latency = {}, events = [], feed = null, act = [], describe }) {
  return {
    name,
    width,
    height,
    theme,
    describe,
    seeds: {
      ...base,
      ...extra,
      settings: withSettings(base, { theme, ...settings }),
      snmplens_active_tab: tab,
    },
    bindings,
    // How long each call should APPEAR to take. A still spends these instantly
    // under virtual time; only a clip ever sees them.
    latency,
    events,
    // A repeating event source, for the screens whose subject is that the
    // numbers keep arriving. See screenshots/bridge/feed.js.
    feed,
    act,
  };
}

/**
 * The catalogue, theme-independent. `base` is the file stem; the emitted scene
 * names — and therefore the file names — are `<base>-dark` and `<base>-light`.
 */
const CATALOGUE = [
  {
    base: 'operations',
    tab: TABS.operations,
    // A walk's results are the component's own state, so they cannot be seeded
    // — the walk has to be run. This is the one place the harness presses
    // buttons, and it presses them by their label.
    // 'Table View', not 'Table'. Two buttons in the results toolbar start with
    // "Table ", the exporter comes first in the document, and the loose match
    // takes the earliest — so this pressed "Table CSV" and the only visible
    // effect was a toast saying the table had been exported. It looked like it
    // worked because Table View is already the default.
    act: ['WALK', 'Execute WALK', 'Table View'],
    // The fixture already claims 412 ms in its own responseTimeMs, and answering
    // instantly contradicted it: 155 varbinds landed in the same frame as the
    // click, so the button never showed that it was working.
    latency: { SnmpWalk: 900 },
    describe: 'A walk of ifTable rendered as a real table, split by INDEX.',
  },
  {
    base: 'mib-browser',
    tab: TABS.operations,
    // The picture had nothing selected, so the detail panel its caption promised
    // was not in it. The node is addressed by OID rather than by name because
    // "ifOperStatus" is ALSO the label of a favourite in the panel above, which
    // is what a text search finds first.
    //
    // Deliberately not filtering first, though that was the obvious move:
    // measured, typing into the search box replaces the tree with COMPACTED
    // ancestor paths — three rows reading "mgmt .mib-2 .interfaces .ifTable
    // .ifEntry" — and the leaf, which is the thing being selected, is no longer
    // in the document at all.
    act: ['sel:[data-oid="1.3.6.1.2.1.2.2.1.8"] .node-label'],
    describe: 'The MIB tree, searched, with a node selected and its detail shown.',
  },
  {
    base: 'monitor-charts',
    tab: TABS.monitor,
    // Six hours of history are seeded, and then the samples keep coming: for the
    // still this changes nothing that virtual time does not spend instantly, and
    // it is what makes the clip of this screen worth recording at all.
    feed: { from: 'monitorSamples', everyMs: 420 },
    // Tall enough for the response-time chart below the main one; at 1100 it
    // was sliced through its legend, which reads as a broken layout.
    height: 1320,
    describe: 'Polling sessions charted over several hours, with a threshold crossed.',
  },
  {
    base: 'trap-listener',
    tab: TABS.traps,
    // "with their varbinds, the listener running" was true of neither: every
    // row was collapsed behind its chevron and the header said the listener was
    // stopped, next to a Start Listening button nobody had pressed.
    act: ['Start Listening', 'sel:.trap-summary|0'],
    describe: 'Received traps with their varbinds, the listener running.',
  },
  {
    base: 'events-journal',
    tab: TABS.events,
    describe: 'The journal: traps, thresholds, a reachability loss, a system event.',
  },
  {
    base: 'history-diff',
    tab: TABS.history,
    // Nothing was ever diffed here — the scene had no steps at all, so the
    // picture was the plain history list with Diff Mode un-pressed, under a
    // caption about a comparison. Diff mode wants two entries chosen, A then B,
    // before it will offer Compare.
    // The OLDER walk is chosen first: diff mode labels them A then B in the
    // order they are picked, and A being the later of the two reads backwards.
    act: ['Diff Mode', 'sel:.entry-header|1', 'sel:.entry-header|0', 'Compare'],
    // The modal is taller than the list behind it.
    height: 1150,
    describe: 'Two walks of the same device, diffed side by side.',
  },
  {
    base: 'network-discovery',
    tab: TABS.discovery,
    height: 1150,
    // The sweep has to be RUN: its results are the component's own state, so
    // without this the picture is of an empty form saying "no scan results".
    act: ['Scan'],
    describe: 'A CIDR sweep that found a real estate, with each device named.',
  },
  {
    base: 'mib-editor',
    tab: TABS.mibeditor,
    height: 1100,
    // Without this the editor shows its empty state — "pick a MIB on the left"
    // — which is a picture of the file list, not of the editor.
    act: ['ACME-POE-MIB'],
    describe: 'A MIB open, highlighted, with the analysis pointing at the line.',
  },
  {
    base: 'target-manager',
    tab: TABS.operations,
    // Framed to the dialog. At 1100 the lower half was empty backdrop, which
    // makes a documentation image about a dialog mostly about nothing.
    height: 940,
    // A real estate rather than two devices: three groups, and per-device
    // overrides on the two that were migrated to v3 while the rest were not,
    // which is the situation the feature exists for.
    settings: {
      targets: [
        '10.20.0.1 # core-sw-01',
        '10.20.0.2 # core-sw-02',
        '10.20.4.11 # dist-sw-2f',
        '10.20.4.12 # dist-sw-3f',
        '10.20.4.23 # dist-sw-4f',
        '10.20.8.1 # edge-rtr-01',
        '10.20.8.2 # edge-rtr-02',
        '192.168.30.5 # ups-server-room',
      ].join('\n'),
      targetGroups: [
        { id: 'default', name: 'Default' },
        { id: 'core', name: 'Core switches' },
        { id: 'dist', name: 'Distribution' },
        { id: 'edge', name: 'Edge routers' },
      ],
      targetGroupAssignments: {
        '10.20.0.1': 'core', '10.20.0.2': 'core',
        '10.20.4.11': 'dist', '10.20.4.12': 'dist', '10.20.4.23': 'dist',
        '10.20.8.1': 'edge', '10.20.8.2': 'edge',
      },
      targetOverrides: {
        '10.20.8.1': { snmpVersion: 'v3', v3: { user: 'noc-ro', authProto: 'SHA-256', privProto: 'AES-256' } },
        '10.20.8.2': { snmpVersion: 'v3', v3: { user: 'noc-ro', authProto: 'SHA-256', privProto: 'AES-256' } },
        '192.168.30.5': { snmpVersion: 'v1', port: 1161 },
      },
    },
    act: ['Target'],
    describe: 'Managing targets: groups, per-device overrides, reachability.',
  },
  {
    base: 'settings-snmp',
    tab: TABS.operations,
    height: 1100,
    // A named v3 user, with the passphrases BLANK — which is not an oversight.
    // The interface never receives a stored credential back from the backend,
    // so an empty field beside a filled username is what a configured
    // installation actually looks like.
    settings: {
      v3: { user: 'noc-ro', secLevel: 'authPriv', authProto: 'SHA-256', privProto: 'AES-256' },
    },
    act: ['key:,', 'SNMP'],
    describe: 'SNMP defaults and the v3 credentials, with the store named.',
  },
  {
    base: 'settings-notifications',
    tab: TABS.operations,
    height: 1250,
    act: ['key:,', 'Notifications'],
    describe: 'Destinations, routing rules, and the delivery log.',
  },
  {
    base: 'anonymous-mode',
    tab: TABS.operations,
    // NOT `settings: { anonymousMode: true }`, which is what this used to say —
    // and it silently produced an UNMASKED screenshot. settingsStore.js forces
    // the flag to false on load, deliberately, so that closing the application
    // can never leave someone's install in a masked state. Seeding it is
    // therefore impossible by design, and the only way in is the application's
    // own Ctrl+Shift+A — pressed after the walk, so there is something on
    // screen for it to mask.
    latency: { SnmpWalk: 900 },
    act: ['WALK', 'Execute WALK', 'key:shift+A'],
    describe: 'The same screen with every address replaced by a stable alias.',
  },
];

export function buildScenes(seeds) {
  const base = baseSeeds(seeds);
  return CATALOGUE.flatMap((entry) =>
    THEMES.map((theme) => scene(base, `${entry.base}-${theme}`, { ...entry, theme })),
  );
}
