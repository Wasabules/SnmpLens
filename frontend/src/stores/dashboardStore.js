import { writable } from 'svelte/store';

/**
 * Which bound session the dashboard is showing.
 *
 * A store rather than component state, and that is not a preference: the tab
 * shell mounts panels with `{#if activeTab === …}`, which DESTROYS the
 * component on every switch. Component state went with it, which is exactly why
 * mibEditorStore exists — the same shell, the same defect, and nothing in the
 * build or the browser says a word. Switching to Traps and back would drop the
 * dashboard's selection and land on whatever happened to be first.
 *
 * The selection is a session id, not a session: the sessions themselves live in
 * pollingStore and are refreshed by the poll clock, so holding one here would
 * hold a stale copy the moment a sample arrived.
 */
function createDashboardStore() {
  const { subscribe, set } = writable({ sessionId: null });

  return {
    subscribe,
    select(sessionId) {
      set({ sessionId: sessionId || null });
    },
    clear() {
      set({ sessionId: null });
    },
  };
}

export const dashboardStore = createDashboardStore();

/**
 * The session the dashboard should show, given what exists right now.
 *
 * Pure, and it takes both its inputs as ARGUMENTS — reactive.test.mjs exists
 * because Svelte 5 tracks what the expression reads, and a store read inside a
 * callee is not a dependency it can see.
 *
 * The rules: honour the selection when it still exists, otherwise fall back to
 * the first session that HAS a preset — a dashboard is drawn from a layout, and
 * a hand-built session has none — and otherwise nothing.
 */
export function dashboardSession(sessions, sessionId) {
  const list = Array.isArray(sessions) ? sessions : [];
  const chosen = list.find((s) => s.id === sessionId);
  if (chosen) return chosen;
  return list.find((s) => s.preset && Array.isArray(s.preset.widgets) && s.preset.widgets.length > 0) || null;
}

/**
 * The sessions a dashboard can draw: the ones bound from a preset.
 */
export function dashboardCandidates(sessions) {
  return (Array.isArray(sessions) ? sessions : [])
    .filter((s) => s.preset && Array.isArray(s.preset.widgets) && s.preset.widgets.length > 0);
}
