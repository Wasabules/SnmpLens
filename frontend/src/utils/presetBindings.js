// Which monitoring sessions are bound to one equipment.
//
// Its own module, with NO imports, so it can be tested by node directly:
// utils/targets.js reaches anonymize.js, which reaches the settings store,
// which reaches the Wails bridge — a chain that needs the whole bundler to
// load. mibDependencies.js and mibGraph.js are the same shape for the same
// reason.

/**
 * The monitoring sessions bound from a preset to one equipment.
 *
 * By ADDRESS, never by index or by label: `targets` is a stored JSON array that
 * can hold addresses no longer in the settings, the list is reordered whenever
 * somebody edits the text field, and two targets can share a label. The address
 * is the only thing that identifies an equipment across both sides.
 *
 * Pure, and it takes both its inputs as arguments — the target manager calls it
 * from the markup, where reactive.test.mjs is watching.
 *
 * @param {Array} sessions pollingStore sessions
 * @param {string} address the equipment
 * @returns {Array} the sessions that poll it AND carry a preset snapshot
 */
export function boundPresetsFor(sessions, address) {
  const addr = String(address || '').trim();
  if (!addr) return [];
  return (Array.isArray(sessions) ? sessions : []).filter(
    (s) => s && s.preset && (s.targets || []).includes(addr),
  );
}
