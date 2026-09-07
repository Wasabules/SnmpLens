// A spreadsheet reads a cell starting with any of these as a FORMULA, not as
// text. Tab and carriage return are here because Excel skips leading whitespace
// before deciding.
const FORMULA_LEAD = /^[=+\-@\t\r]/;

// A strict number, so a negative reading stays a number. Deliberately not
// Number(s): that accepts leading whitespace, "0x10", "Infinity" and "", and
// leading whitespace is one of the vectors above.
const STRICT_NUMBER = /^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/;

/**
 * Escape a value for safe CSV output.
 *
 * Two separate jobs, and the second one was missing.
 *
 * RFC 4180 QUOTING keeps the file parseable: a comma, a quote or a newline
 * inside a field has to be quoted or the columns shift.
 *
 * FORMULA NEUTRALISATION keeps the file from executing. Excel, LibreOffice and
 * Sheets treat a cell beginning `= + - @` as a formula, and these exports carry
 * text this application did not write: `sysName` and `sysDescr` come straight
 * from the polled device (DiscoveryPanel.svelte), event summaries come from
 * traps. So someone who controls a device on a scanned network chooses what a
 * cell contains, waits for an operator to export a sweep and open it, and gets
 * code execution on the workstation with access to the whole management
 * network. CWE-1236.
 *
 * The apostrophe is the convention every spreadsheet honours: it forces the
 * cell to text and is not itself displayed.
 *
 * A value that is a plain number is left alone — prefixing `-42` would stop the
 * column being numeric, and a number cannot be a formula.
 *
 * @param {*} val
 * @returns {string}
 */
export function escapeCSV(val) {
  let s = String(val ?? '');
  if (FORMULA_LEAD.test(s) && !STRICT_NUMBER.test(s)) {
    s = "'" + s;
  }
  // `;` as well as `,`: Excel in a French, German or Spanish locale — three of
  // the five this application ships — reads `;` as the separator, so a value
  // containing one shifts the columns there and nowhere else. `\r` for the same
  // reason as `\n`. The private escaper in HistoryPanel had this wider set and
  // the shared one did not, which is the other half of why they diverged.
  return /[",;\r\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
}

/**
 * Trigger a file download in the browser.
 * @param {string} content
 * @param {string} filename
 * @param {string} mimeType
 */
export function downloadFile(content, filename, mimeType) {
  const blob = new Blob([content], { type: mimeType });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
