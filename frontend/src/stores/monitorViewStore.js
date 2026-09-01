import { writable, get } from 'svelte/store';

/**
 * View preferences for the Monitor tab.
 *
 * The panel is mounted behind `{#if activeTab === 'monitor'}`, so leaving the
 * tab destroys it and every `let` in it: view mode, layout, hidden channels,
 * timebase… all reset. Toggling separate/stacked likewise swaps in a brand new
 * chart component. None of that is state the user expects to lose, so it lives
 * here instead of in the components, and survives a restart via localStorage.
 *
 * Only *preferences* belong here — never sampled data (that is the polling
 * store and SQLite) nor transient UI (fullscreen, open modal, in-flight loads).
 */
const KEY = 'snmpMonitorView';
const PERSIST_DELAY_MS = 300;

function load() {
  try {
    const raw = localStorage.getItem(KEY);
    const parsed = raw ? JSON.parse(raw) : {};
    return { form: parsed.form || {}, view: parsed.view || {} };
  } catch (e) {
    console.warn('Failed to read monitor view preferences:', e);
    return { form: {}, view: {} };
  }
}

function createMonitorViewStore() {
  const store = writable(load());
  const { subscribe, update } = store;

  // Coalesce writes: dragging the chart's resize handle would otherwise hit
  // localStorage on every mouse move.
  let timer = null;
  function persistSoon() {
    clearTimeout(timer);
    timer = setTimeout(() => {
      try {
        localStorage.setItem(KEY, JSON.stringify(get(store)));
      } catch (e) {
        console.warn('Failed to save monitor view preferences:', e);
      }
    }, PERSIST_DELAY_MS);
  }

  return {
    subscribe,

    /** Snapshot for initialising component state at mount. */
    snapshot: () => get(store),

    /** Merge the setup-form fields (OIDs, interval, target selection…). */
    patchForm(changes) {
      update((s) => ({ ...s, form: { ...s.form, ...changes } }));
      persistSoon();
    },

    /** Replace the per-session view state in one go. */
    saveView(view) {
      update((s) => ({ ...s, view: { ...view } }));
      persistSoon();
    },

    /** Forget everything about a session that no longer exists. */
    dropSession(sessionId) {
      update((s) => {
        const view = {};
        for (const [group, byId] of Object.entries(s.view || {})) {
          if (byId && typeof byId === 'object' && !Array.isArray(byId)) {
            const copy = { ...byId };
            delete copy[sessionId];
            view[group] = copy;
          } else {
            view[group] = byId;
          }
        }
        return { ...s, view };
      });
      persistSoon();
    },
  };
}

export const monitorViewStore = createMonitorViewStore();
