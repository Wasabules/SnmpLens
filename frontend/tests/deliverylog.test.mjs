// The delivery log.
//
// A dead letter is, in the outbox's own words, "the only way an operator learns
// that a notification never arrived" and is "kept until the operator deals with
// them" — and until this panel existed there was no screen listing one and no
// button retrying one. NotifyListDeliveries and NotifyRetryDelivery were bound
// across the bridge with no caller anywhere in the renderer, so every failure
// the outbox is built to survive ended in silence.
//
// The field names are the contract with pkg/storage.Delivery's JSON tags, and
// nothing else checks them: a renamed tag renders blank cells rather than
// failing, in the panel whose entire job is to say what went wrong.
import { readFileSync } from 'node:fs';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

const panel = readFileSync(
  new URL('../src/settings/NotifySettings.svelte', import.meta.url), 'utf8');
const goSource = readFileSync(
  new URL('../../pkg/storage/notify_store.go', import.meta.url), 'utf8');

// --- the panel exists and is wired to the bridge ---
for (const binding of ['NotifyListDeliveries', 'NotifyRetryDelivery']) {
  const imported = new RegExp(`^\\s*${binding},`, 'm').test(panel);
  const called = new RegExp(`${binding}\\s*\\(`).test(panel);
  check(`${binding} is imported`, imported);
  check(`${binding} is actually called`, called,
    called ? '' : 'bound across the bridge with no caller — the dead letter is invisible');
}

// --- every field the panel reads exists in the Go struct ---
const tags = new Set(
  [...goSource.matchAll(/json:"([A-Za-z0-9_]+)/g)].map((m) => m[1]));
check('the Delivery struct was found', tags.size > 0, `${tags.size} tags`);

const read = new Set(
  [...panel.matchAll(/\bd\.([a-zA-Z][a-zA-Z0-9]*)\b/g)].map((m) => m[1]));
check('the panel reads some delivery fields', read.size > 0, [...read].join(', '));

for (const field of read) {
  check(`d.${field} exists in pkg/storage.Delivery`, tags.has(field),
    tags.has(field) ? '' : `no json:"${field}" tag — this cell renders blank`);
}

// The ones the operator actually needs must all be shown.
for (const field of ['state', 'sinkId', 'attempts', 'lastError', 'createdAt']) {
  check(`the log shows ${field}`, read.has(field),
    read.has(field) ? '' : 'an operator cannot tell what happened without it');
}

// --- retrying is offered where it is possible, and only there ---
{
  const offered = /state === 'dead'[\s\S]{0,240}retryDelivery/.test(panel);
  check('a given-up delivery offers a retry', offered,
    offered ? '' : 'nothing retries a dead letter');
}

// --- every string is translatable ---
{
  const en = JSON.parse(readFileSync(
    new URL('../src/i18n/en.json', import.meta.url), 'utf8'));
  // Literal keys used in the markup, plus the ones the state list names —
  // which is the whole point of that list being explicit.
  const used = [...panel.matchAll(/\$_\('notify\.(delivery[A-Za-z]*|deliveries[A-Za-z]*|noDeliveries|deadLetterCount)'/g)]
    .map((m) => m[1]);
  const fromStateList = [...panel.matchAll(/'(deliveryState[A-Za-z]+)'/g)].map((m) => m[1]);
  used.push(...fromStateList);
  check('every delivery state has its own key rather than a built one',
    fromStateList.length >= 4, fromStateList.join(', '));
  check('the panel uses delivery keys', used.length > 0, used.join(', '));
  const missing = used.filter((k) => !(k in en.notify));
  check('every delivery key exists in en.json', missing.length === 0, missing.join(', '));
}

// The detector must be able to fail.
check('the detector: an unknown field would be caught', !tags.has('notAFieldName'));

process.exit(failures ? 1 : 0);
