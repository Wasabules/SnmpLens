// A vocabulary served from Go is a contract, and nothing checked this one.
//
// pkg/preset owns two lists the interface renders and does not define: the
// widget KINDS, and the validation MESSAGES. Both cross the bridge as i18n key
// suffixes rather than prose — preset.widget.<kind>, preset.err.<message> —
// precisely so the vocabulary is not the one part of the application that never
// translates. The cost of that choice is a second place the key has to exist.
//
// svelte-i18n falls back SILENTLY to the key itself, so a message Go emits and
// no locale defines renders as the literal string "preset.err.oidTooDeep" in a
// dialog, under a field path, next to real sentences. It looks like a bug in
// the preset file rather than a missing translation, and nothing anywhere fails.
//
// This is snmpparams.test.mjs for a Go-served vocabulary: read the Go source,
// and require en.json to answer — with the same placeholders the Args map
// supplies, because a message that names {found} when Go sends `count` renders
// the brace text verbatim.
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../../', import.meta.url).pathname.replace(/^[/]([A-Za-z]:)/, '$1');
const presetDir = join(root, 'pkg', 'preset');
const en = JSON.parse(readFileSync(join(root, 'frontend', 'src', 'i18n', 'en.json'), 'utf8'));

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const goSource = readdirSync(presetDir)
  .filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'))
  .map((f) => readFileSync(join(presetDir, f), 'utf8'))
  .join('\n');

/* --- the messages --------------------------------------------------------- */

// errf(field, "message", args) — the second argument is the key suffix, and the
// args map keys are the placeholders the message may use.
const messages = new Map(); // message -> Set(placeholder)
for (const m of goSource.matchAll(/errf\((?:[^,]|\([^)]*\))+,\s*"([a-zA-Z]+)"\s*,\s*(nil|map\[string\]string\{)/g)) {
  const [, name, tail] = m;
  if (!messages.has(name)) messages.set(name, new Set());
  if (tail === 'nil') continue;
  // The arg map is on the same line or the next few; take what follows up to
  // the closing brace of the literal.
  const from = m.index + m[0].length;
  const chunk = goSource.slice(from, from + 400);
  const end = chunk.indexOf('}');
  for (const a of chunk.slice(0, end < 0 ? 400 : end).matchAll(/"([a-zA-Z]+)":/g)) {
    messages.get(name).add(a[1]);
  }
}

check('the Go source yields validation messages', messages.size >= 10, `${messages.size} found`);

const errKeys = en.preset?.err || {};
for (const [name, args] of [...messages].sort()) {
  const text = errKeys[name];
  if (typeof text !== 'string') {
    check(`preset.err.${name} exists in en.json`, false, 'missing');
    continue;
  }
  const used = new Set([...text.matchAll(/\{([a-zA-Z]+)\}/g)].map((m) => m[1]));
  // Every placeholder the message uses must be one Go actually sends —
  // otherwise it renders as literal braces.
  const unknown = [...used].filter((p) => !args.has(p));
  check(`preset.err.${name} names only placeholders Go sends`, unknown.length === 0,
    unknown.length ? `unknown: ${unknown.join(', ')}` : `${[...args].join(', ') || 'no args'}`);
}

// And nothing in en.json that Go cannot emit: a message nobody sends is a
// message nobody maintains, and the next reader believes it is reachable.
for (const name of Object.keys(errKeys)) {
  check(`preset.err.${name} is emitted by Go`, messages.has(name),
    messages.has(name) ? '' : 'orphaned: no errf in pkg/preset sends it');
}

/* --- the widget kinds ----------------------------------------------------- */

const kinds = [...goSource.matchAll(/Kind[A-Z][a-zA-Z]*\s*=\s*"([a-z]+)"/g)].map((m) => m[1]);
check('the Go source yields widget kinds', kinds.length >= 4, kinds.join(', '));

const widgetKeys = en.preset?.widget || {};
for (const kind of kinds) {
  check(`preset.widget.${kind} exists in en.json`, typeof widgetKeys[kind] === 'string');
}
for (const kind of Object.keys(widgetKeys)) {
  check(`preset.widget.${kind} is a kind Go serves`, kinds.includes(kind),
    kinds.includes(kind) ? '' : 'orphaned: pkg/preset declares no such kind');
}

// The panel must render EVERY kind. A kind added in Go that the dispatch does
// not name renders as nothing at all, with no error anywhere — the same silence
// this whole file exists to break.
const panel = readFileSync(join(root, 'frontend', 'src', 'settings', 'PresetSettings.svelte'), 'utf8');
const servesEveryKind = /preset\.widget\.\$\{/.test(panel) || kinds.every((k) => panel.includes(`'${k}'`));
check('the library panel labels kinds through the served key rather than a copied list',
  servesEveryKind, servesEveryKind ? '' : 'the panel names kinds one by one and will silently miss the next one');

/* --- prove the checks can fail ------------------------------------------- */

check('the detector: a message Go does not emit is caught', !messages.has('notAMessage'));
check('the detector: an unknown placeholder is caught',
  [...'{nope}'.matchAll(/\{([a-zA-Z]+)\}/g)].map((m) => m[1])[0] === 'nope');

process.exit(failures ? 1 : 0);
