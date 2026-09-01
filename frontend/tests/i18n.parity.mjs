// Checks that the five locale files carry exactly the same set of keys.
//
// svelte-i18n falls back silently: a key missing from fr.json renders the
// English string, and a key left behind after a rename renders the raw key id.
// Neither shows up in a build, so a locale can drift for months and only be
// noticed by whoever reads that language. English is the reference because it
// is the one that always gets the new keys first.
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const dir = new URL('../src/i18n/', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');

function flatten(value, prefix = '') {
  const keys = new Set();
  for (const [k, v] of Object.entries(value)) {
    const key = prefix + k;
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      for (const nested of flatten(v, key + '.')) keys.add(nested);
    } else {
      keys.add(key);
    }
  }
  return keys;
}

// Placeholders are part of the contract: {oid} in one locale and {device} in
// another means one of them renders a literal brace to a user.
//
// This extracts the ARGUMENT NAME, so it treats a plain {count} and an ICU
// {count, plural, one {# result} other {# results}} as the same placeholder.
// They genuinely are: Chinese has no plural forms, so `{count} 个结果` is the
// correct translation of the English plural form, not a missing one.
function placeholders(str) {
  return new Set([...String(str).matchAll(/\{\s*(\w+)\s*[,}]/g)].map((m) => m[1]));
}

function valueAt(obj, dotted) {
  return dotted.split('.').reduce((acc, part) => (acc == null ? acc : acc[part]), obj);
}

const locales = {};
for (const file of readdirSync(dir)) {
  if (!file.endsWith('.json')) continue;
  locales[file.slice(0, -5)] = JSON.parse(readFileSync(join(dir, file), 'utf8'));
}

if (!locales.en) {
  console.log('FAIL  en.json is missing; it is the reference locale');
  process.exit(1);
}

const reference = flatten(locales.en);
let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

check('at least the five shipped locales are present',
  ['en', 'fr', 'de', 'es', 'zh'].every((l) => l in locales),
  Object.keys(locales).join(', '));

for (const [locale, data] of Object.entries(locales)) {
  if (locale === 'en') continue;
  const keys = flatten(data);
  const missing = [...reference].filter((k) => !keys.has(k));
  const extra = [...keys].filter((k) => !reference.has(k));

  check(`${locale}: no key missing`, missing.length === 0,
    missing.length ? missing.slice(0, 5).join(', ') + (missing.length > 5 ? ` (+${missing.length - 5})` : '') : '');
  check(`${locale}: no key left over`, extra.length === 0,
    extra.length ? extra.slice(0, 5).join(', ') + (extra.length > 5 ? ` (+${extra.length - 5})` : '') : '');

  const mismatched = [];
  for (const key of reference) {
    if (!keys.has(key)) continue;
    const a = valueAt(locales.en, key);
    const b = valueAt(data, key);
    if (typeof a !== 'string' || typeof b !== 'string') continue;
    const pa = placeholders(a);
    const pb = placeholders(b);
    if (pa.size !== pb.size || [...pa].some((p) => !pb.has(p))) {
      mismatched.push(key);
    }
  }
  check(`${locale}: placeholders match English`, mismatched.length === 0,
    mismatched.slice(0, 5).join(', '));
}

console.log(`\n${reference.size} keys checked across ${Object.keys(locales).length} locales`);
process.exit(failures ? 1 : 0);
