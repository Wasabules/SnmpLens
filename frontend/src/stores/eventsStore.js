import { writable, get } from 'svelte/store';
import {
  EventsQuery,
  EventsCounts,
  EventsAck,
  EventsAckAll,
  EventsDelete,
  EventsClear,
} from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { _ } from 'svelte-i18n';
import { notificationStore } from './notifications';
import { settingsStore } from './settingsStore';
import { sendNativeNotification } from '../utils/nativeNotify';

/**
 * The event journal: what the system tells the operator.
 *
 * Deliberately NOT the SNMP query history — that records what the operator
 * asked a device to do. This records what happened to them: traps arriving,
 * thresholds breached, targets going dark. Only this stream is worth
 * forwarding to syslog or email, and only this stream has to keep working
 * with no window open.
 *
 * Unlike historyStore, nothing is mirrored in memory wholesale: the journal is
 * server-paginated with a keyset cursor, because it is written to while it is
 * being read and can hold tens of thousands of rows.
 */
const EMPTY_COUNTS = { unacked: 0, unackedBySeverity: {}, unackedByCategory: {} };

// Severities worth interrupting the operator for. Everything else is recorded
// and visible in the tab, but does not raise a toast.
const NOTIFY_FROM = new Set(['major', 'critical']);
const NOTIFY_COOLDOWN_MS = 30000;
let lastNotifiedAt = 0;

// Alert sound, kept here because this is now the single place an incident is
// announced — Go detects, the journal records, and this notifies.
function playAlertSound() {
  try {
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.frequency.value = 880;
    osc.type = 'sine';
    gain.gain.value = 0.3;
    osc.start();
    osc.stop(ctx.currentTime + 0.2);
  } catch (e) {
    /* audio is a nicety, never a failure */
  }
}

function announce(ev) {
  if (!ev || ev.acked || !NOTIFY_FROM.has(ev.severity)) return;

  const t = get(_);
  const message = t(ev.titleKey, { values: ev.params || {}, default: '' }) || ev.summary;
  notificationStore.add(message, 'error');

  // The OS notification and the sound are rate-limited so a flapping target
  // cannot spam the desktop; the journal still records every occurrence.
  const now = Date.now();
  if (now - lastNotifiedAt < NOTIFY_COOLDOWN_MS) return;
  lastNotifiedAt = now;

  const settings = get(settingsStore);
  if (settings.monitor?.systemNotifications) {
    sendNativeNotification(t('events.title'), message);
    if (settings.monitor?.alertSound) playAlertSound();
  }
}

function createEventsStore() {
  const { subscribe, update, set } = writable({
    items: [],
    total: 0,
    nextCursor: 0,
    loading: false,
    error: null,
    filter: {},
  });

  const counts = writable(EMPTY_COUNTS);

  async function refreshCounts() {
    try {
      const c = await EventsCounts();
      counts.set(c || EMPTY_COUNTS);
    } catch (e) {
      console.warn('Failed to load event counts:', e);
    }
  }

  /** Load the newest page for a filter, replacing what is displayed. */
  async function load(filter = {}) {
    update((s) => ({ ...s, loading: true, error: null, filter }));
    try {
      const page = await EventsQuery({ ...filter, beforeSeq: 0 });
      set({
        items: page.items || [],
        total: page.total || 0,
        nextCursor: page.nextCursor || 0,
        loading: false,
        error: null,
        filter,
      });
    } catch (e) {
      console.error('Failed to query events:', e);
      update((s) => ({ ...s, loading: false, error: String(e) }));
    }
    refreshCounts();
  }

  /** Append the next page. */
  async function loadMore() {
    const state = get({ subscribe });
    if (!state.nextCursor || state.loading) return;
    update((s) => ({ ...s, loading: true }));
    try {
      const page = await EventsQuery({ ...state.filter, beforeSeq: state.nextCursor });
      update((s) => ({
        ...s,
        items: [...s.items, ...(page.items || [])],
        total: page.total || s.total,
        nextCursor: page.nextCursor || 0,
        loading: false,
      }));
    } catch (e) {
      console.error('Failed to load more events:', e);
      update((s) => ({ ...s, loading: false, error: String(e) }));
    }
  }

  async function ack(ids) {
    if (!ids || !ids.length) return;
    // Optimistic: the badge should drop the moment the operator clicks.
    update((s) => ({
      ...s,
      items: s.items.map((e) => (ids.includes(e.id) ? { ...e, acked: true } : e)),
    }));
    try {
      await EventsAck(ids);
    } catch (e) {
      console.error('Failed to acknowledge events:', e);
    }
    refreshCounts();
  }

  async function ackAll(filter) {
    try {
      await EventsAckAll(filter || {});
    } catch (e) {
      console.error('Failed to acknowledge all events:', e);
    }
    await load(get({ subscribe }).filter);
  }

  async function remove(ids) {
    if (!ids || !ids.length) return;
    update((s) => ({ ...s, items: s.items.filter((e) => !ids.includes(e.id)) }));
    try {
      await EventsDelete(ids);
    } catch (e) {
      console.error('Failed to delete events:', e);
    }
    refreshCounts();
  }

  async function clear() {
    try {
      await EventsClear();
    } catch (e) {
      console.error('Failed to clear the journal:', e);
    }
    await load(get({ subscribe }).filter);
  }

  /**
   * Live tail. The Go side persists an event and THEN emits this — the row
   * exists whether or not a window was listening, so a missed emit costs a
   * refresh, never a record.
   */
  function listen() {
    EventsOn('event:new', (ev) => {
      announce(ev);
      counts.update((c) => ({
        ...c,
        unacked: (c.unacked || 0) + (ev && ev.acked ? 0 : 1),
      }));
      update((s) => {
        // Only prepend when looking at the newest page of a matching filter;
        // otherwise the list would silently disagree with its own filter.
        if (s.nextCursor && s.items.length === 0) return s;
        if (!matchesFilter(ev, s.filter)) return s;
        return { ...s, items: [ev, ...s.items], total: s.total + 1 };
      });
    });
  }

  return { subscribe, counts, load, loadMore, ack, ackAll, remove, clear, refreshCounts, listen };
}

// Mirror of the SQL filter, for the live-tail path only. Kept intentionally
// small: the authoritative filtering happens in SQL.
function matchesFilter(ev, filter) {
  if (!ev || !filter) return true;
  if (filter.categories && filter.categories.length && !filter.categories.includes(ev.category)) return false;
  if (filter.unackedOnly && ev.acked) return false;
  if (filter.sessionId && ev.sessionId !== filter.sessionId) return false;
  return true;
}

export const eventsStore = createEventsStore();
export const eventCounts = eventsStore.counts;
