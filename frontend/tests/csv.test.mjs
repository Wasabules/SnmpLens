// A CSV export cannot become a formula, and cannot escape on its own.
//
// Excel, LibreOffice and Sheets read a cell beginning `= + - @` as a FORMULA.
// The values in these exports are not ours: `sysName` and `sysDescr` come
// straight from the polled device, event summaries carry trap text. So someone
// who controls a device on a scanned network chooses what a cell contains,
// waits for an operator to export a sweep and open it, and gets code execution
// on the workstation that has access to the whole management network. CWE-1236.
//
// THE INTERESTING PART IS WHY IT SURVIVED. The defence was not missing — it was
// in `HistoryPanel.csvCell`, with a comment explaining exactly this attack. It
// simply never reached `utils/csv.js`, and three of the five exports had grown
// their own escaper by copying the RFC 4180 half without the formula half. The
// events export — the one carrying raw trap text — was among them.
//
// So the unit tests below are the smaller half of this file. The structural
// checks are what stop it recurring: an export must use the shared function,
// and nobody may write a private one.
import { readdirSync, readFileSync } from 'node:fs';
import { join, basename } from 'node:path';
import { escapeCSV } from '../src/utils/csv.js';

const root = new URL('../src/', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

function sources(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...sources(path));
    else if (entry.name.endsWith('.svelte') || entry.name.endsWith('.js')) out.push(path);
  }
  return out;
}

/* --- what the escaper must do --------------------------------------------- */

for (const lead of ['=', '+', '-', '@', '\t', '\r']) {
  const payload = lead + `cmd|'/c calc'!A1`;
  const got = escapeCSV(payload);
  check(`a value starting ${JSON.stringify(lead)} is neutralised`,
    got.startsWith("'") || got.startsWith(`"'`), JSON.stringify(got));
}

// A negative reading is not a formula, and prefixing it would stop the column
// being numeric — which is the reason the naive fix is wrong.
for (const n of ['-42', '-3.5', '+7', '1e-3', '0']) {
  check(`the number ${n} is left alone`, escapeCSV(n) === n, JSON.stringify(escapeCSV(n)));
}

// Not numbers, however much they start like one.
for (const s of ['-2+3+cmd|x', '+1;=1', '- 5', '0x10', '', '  1']) {
  const got = escapeCSV(s);
  const neutralised = got.startsWith("'") || got.startsWith(`"'`) || !/^[=+\-@\t\r]/.test(s);
  check(`${JSON.stringify(s)} is not mistaken for a number`, neutralised, JSON.stringify(got));
}

// RFC 4180, plus `;` — Excel in a French, German or Spanish locale reads that
// as the separator, and three of the five locales this app ships are those.
for (const s of ['a,b', 'a;b', 'a"b', 'a\nb', 'a\rb']) {
  const got = escapeCSV(s);
  check(`${JSON.stringify(s)} is quoted`, got.startsWith('"') && got.endsWith('"'), JSON.stringify(got));
}
check('an embedded quote is doubled', escapeCSV('a"b') === '"a""b"', escapeCSV('a"b'));
check('a plain value is untouched', escapeCSV('sysDescr') === 'sysDescr');
check('null and undefined become empty', escapeCSV(null) === '' && escapeCSV(undefined) === '');

/* --- and what the codebase must do with it -------------------------------- */

const files = sources(root);

// An export that builds a .csv has to route its values through the shared
// function. Checked by the FILE having both, which is coarse — but the failure
// it catches is a file that has one and not the other, which is exactly what
// happened three times.
const unescaped = [];
for (const file of files) {
  const src = readFileSync(file, 'utf8');
  if (!/downloadFile\([^)]*/.test(src)) continue;
  if (!/\.csv'/.test(src) && !/\.csv"/.test(src)) continue;
  if (!src.includes('escapeCSV')) unescaped.push(basename(file));
}
check('every file that downloads a .csv imports the shared escaper',
  unescaped.length === 0, unescaped.join(', '));

// Nobody writes their own. `.replace(/"/g, '""')` is the RFC 4180 doubling and
// is unmistakable; finding it outside utils/csv.js means a second escaper has
// appeared, which is how the formula defence came to exist in one export and
// nowhere else.
const privateEscapers = [];
for (const file of files) {
  if (basename(file) === 'csv.js') continue;
  const src = readFileSync(file, 'utf8');
  if (/replace\(\s*\/"\/g\s*,\s*'""'\s*\)/.test(src) || /replace\(\s*\/"\/g\s*,\s*'""'\)/.test(src)) {
    privateEscapers.push(basename(file));
  }
}
check('no component defines its own CSV escaper',
  privateEscapers.length === 0, privateEscapers.join(', '));

/* --- prove the checks can fail -------------------------------------------- */
{
  // The payload that made this a finding rather than a theory.
  const evil = `=cmd|'/c calc'!A1`;
  check('the detector: the raw payload would be a formula', /^[=+\-@\t\r]/.test(evil));
  check('the detector: and the escaper stops it being one', !/^[=+\-@\t\r]/.test(escapeCSV(evil)),
    escapeCSV(evil));

  // The structural check, on a file that would have failed it.
  const wouldFail = `downloadFile(rows.join('\\n'), 'x.csv', 'text/csv');`;
  check('the detector: a .csv download with no escaper is visible',
    /downloadFile\([^)]*/.test(wouldFail) && /\.csv'/.test(wouldFail) && !wouldFail.includes('escapeCSV'));

  const privateOne = `return /[",]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;`;
  check('the detector: a private escaper is visible',
    /replace\(\s*\/"\/g\s*,\s*'""'\s*\)/.test(privateOne));
}

process.exit(failures ? 1 : 0);
