// Rate control for anything a TRAP can trigger in the renderer.
//
// A trap arrives from the network with nobody authenticating it, and the Go
// side is built to absorb a storm: the socket buffer was raised to 8 MiB and
// 2000 events a second are journalled without loss. Every one of them is then
// emitted to the window, so the renderer is offered the same rate — and it had
// no limit of any kind. One toast per trap, one OS notification per trap, and
// one bridge call per trap into GetOidDetails, which takes pkg/mib's
// package-level exclusive gosmi mutex. So remote UDP traffic set the rate at
// which the renderer held the lock every MIB operation in the product needs,
// and the window whose job is to SHOW the flood is what stopped working.
//
// A gate is leading-edge with a trailing summary: the first event in a window
// goes through immediately, because an operator should learn about a trap the
// moment it arrives, and everything after it is counted and reported once as
// "and N more". That is strictly more information than N identical toasts, and
// it is bounded.
//
// now and schedule are injected so the tests can drive time rather than wait
// for it.

/**
 * @param {object} opts
 * @param {number} opts.windowMs how long one summary window lasts
 * @param {(count: number) => void} opts.onSummary called with what was suppressed
 * @param {() => number} [opts.now]
 * @param {(fn: () => void, ms: number) => any} [opts.schedule]
 * @param {(handle: any) => void} [opts.cancel]
 */
export function createBurstGate({ windowMs, onSummary, now, schedule, cancel }) {
  const clock = now || (() => Date.now());
  const later = schedule || ((fn, ms) => setTimeout(fn, ms));
  const stop = cancel || ((h) => clearTimeout(h));

  let windowEnds = 0;
  let suppressed = 0;
  let timer = null;

  function flush() {
    timer = null;
    const n = suppressed;
    suppressed = 0;
    // The window is over: the next event leads a new one.
    windowEnds = 0;
    if (n > 0) onSummary(n);
  }

  return {
    /**
     * @returns {boolean} true when the caller should act on this event now.
     */
    offer() {
      const t = clock();
      if (t >= windowEnds) {
        windowEnds = t + windowMs;
        // Arm the trailing summary. Without it, the tail of a burst is simply
        // never reported: the last events fall inside a window that nothing
        // closes.
        if (timer === null) timer = later(flush, windowMs);
        return true;
      }
      suppressed++;
      return false;
    },

    /** For teardown; a pending summary is dropped, not fired. */
    reset() {
      if (timer !== null) stop(timer);
      timer = null;
      suppressed = 0;
      windowEnds = 0;
    },

    /** Test and diagnostic access. */
    pending() {
      return suppressed;
    },
  };
}

/**
 * Keep the newest `max` entries of a list that grows from the front.
 *
 * Written once rather than four times, because the four places that needed it
 * were four different lists fed by the same unauthenticated source.
 *
 * @template T
 * @param {T[]} list newest first
 * @param {number} max
 * @returns {T[]} the same array when nothing was dropped, so a store update
 *   that changes nothing does not force a re-render
 */
export function capNewestFirst(list, max) {
  if (list.length <= max) return list;
  return list.slice(0, max);
}
