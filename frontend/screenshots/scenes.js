/**
 * The scene catalogue: one entry per screenshot the site needs.
 *
 * A scene is a URL, not a script. `?scene=monitor` seeds localStorage before the
 * bundle evaluates, picks the tab, and may override any bridge fixture — so a
 * capture is a plain page load with nothing to click and nothing to time.
 *
 * That matters more than it sounds. Driving a UI by clicking is how screenshot
 * automation becomes flaky: a selector moves, a transition is half-finished, and
 * the image is wrong in a way nobody notices until it is on the front page.
 * Here the only thing that can vary between two runs of the same scene is the
 * clock, and the fixtures pin that too.
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
function scene(base, name, { tab, theme = 'dark', width = 1600, height = 1000, settings = {}, seeds: extra = {}, bindings = {}, events = [], act = [], describe }) {
  return {
    name,
    width,
    height,
    describe,
    seeds: {
      ...base,
      ...extra,
      settings: withSettings(base, { theme, ...settings }),
      snmplens_active_tab: tab,
    },
    bindings,
    events,
    act,
  };
}

export function buildScenes(seeds) {
  const base = baseSeeds(seeds);
  return [
  scene(base, 'operations-dark', {
    tab: TABS.operations,
    theme: 'dark',
    // A walk's results are the component's own state, so they cannot be seeded
    // — the walk has to be run. This is the one place the harness presses
    // buttons, and it presses them by their label.
    act: ['WALK', 'Execute WALK', 'Table'],
    describe: 'A walk of ifTable rendered as a real table, split by INDEX.',
  }),

  scene(base, 'operations-light', {
    tab: TABS.operations,
    theme: 'light',
    act: ['WALK', 'Execute WALK', 'Table'],
    describe: 'The same, in the light theme.',
  }),

  scene(base, 'mib-browser', {
    tab: TABS.operations,
    theme: 'dark',
    describe: 'The MIB tree, searched, with a node selected and its detail shown.',
  }),

  scene(base, 'monitor-charts', {
    tab: TABS.monitor,
    theme: 'dark',
    // Tall enough for the response-time chart below the main one; at 1100 it
    // was sliced through its legend, which reads as a broken layout.
    height: 1320,
    describe: 'Polling sessions charted over several hours, with a threshold crossed.',
  }),

  scene(base, 'trap-listener', {
    tab: TABS.traps,
    theme: 'dark',
    describe: 'Received traps with their varbinds, the listener running.',
  }),

  scene(base, 'events-journal', {
    tab: TABS.events,
    theme: 'dark',
    describe: 'The journal: traps, thresholds, a reachability loss, a system event.',
  }),

  scene(base, 'history-diff', {
    tab: TABS.history,
    theme: 'dark',
    describe: 'Two walks of the same device, diffed side by side.',
  }),

  scene(base, 'network-discovery', {
    tab: TABS.discovery,
    theme: 'dark',
    describe: 'A CIDR sweep, then ping and traceroute.',
  }),

  scene(base, 'mib-editor', {
    tab: TABS.mibeditor,
    theme: 'dark',
    height: 1100,
    // Without this the editor shows its empty state — "pick a MIB on the left"
    // — which is a picture of the file list, not of the editor.
    act: ['ACME-POE-MIB'],
    describe: 'A MIB open, highlighted, with the analysis pointing at the line.',
  }),

  scene(base, 'target-manager', {
    tab: TABS.operations,
    theme: 'dark',
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
  }),

  scene(base, 'settings-snmp', {
    tab: TABS.operations,
    theme: 'dark',
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
  }),

  scene(base, 'settings-notifications', {
    tab: TABS.operations,
    theme: 'dark',
    height: 1250,
    act: ['key:,', 'Notifications'],
    describe: 'Destinations, routing rules, and the delivery log.',
  }),

  scene(base, 'anonymous-mode', {
    tab: TABS.operations,
    theme: 'dark',
    settings: { anonymousMode: true },
    act: ['WALK', 'Execute WALK'],
    describe: 'The same screen with every address replaced by a stable alias.',
  }),
  ];
}
