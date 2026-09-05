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
function scene(base, name, { tab, theme = 'dark', width = 1600, height = 1000, settings = {}, seeds: extra = {}, bindings = {}, events = [], describe }) {
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
  };
}

export function buildScenes(seeds) {
  const base = baseSeeds(seeds);
  return [
  scene(base, 'operations-dark', {
    tab: TABS.operations,
    theme: 'dark',
    describe: 'A walk of ifTable rendered as a real table, split by INDEX.',
  }),

  scene(base, 'operations-light', {
    tab: TABS.operations,
    theme: 'light',
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
    describe: 'A MIB open with syntax highlighting and a diagnostic on the line.',
  }),

  scene(base, 'anonymous-mode', {
    tab: TABS.operations,
    theme: 'dark',
    settings: { anonymousMode: true },
    describe: 'The same screen with every address replaced by a stable alias.',
  }),
  ];
}
