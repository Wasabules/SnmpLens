import { writable } from 'svelte/store';

/**
 * Shared x-range between charts that belong to the same group.
 *
 * Two measures with different scales must never share one plot with two y-axes
 * (that is the classic misleading chart). Instead they are stacked as small
 * multiples — which only works if they stay on the SAME time window, so zooming
 * or panning one has to drive the others. Each entry is keyed by group and
 * carries the id of the chart that produced it, so the origin can ignore its
 * own echo.
 */
export const chartSync = writable({});

export function broadcastRange(group, sourceId, min, max) {
  if (!group) return;
  chartSync.update((state) => ({ ...state, [group]: { sourceId, min, max, live: false } }));
}

/** Tell the group to resume live tracking with this timebase. */
export function broadcastLive(group, sourceId, windowMs) {
  if (!group) return;
  chartSync.update((state) => ({ ...state, [group]: { sourceId, live: true, windowMs } }));
}
