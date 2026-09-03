/**
 * Finding IP addresses inside free text.
 *
 * Split out of anonymize.js so it can be run under `go test`'s equivalent: the
 * store it lives beside imports Svelte and localStorage, and none of that is
 * needed to decide whether a run of characters is an address.
 *
 * Candidates are matched loosely and then VALIDATED, rather than matched with
 * a shape. An IPv6 address has several legal spellings — compressed, zoned,
 * with a trailing IPv4 form — and a regex strict enough to accept them all is
 * unreadable, while a loose one quietly accepts prose.
 */

/** A run that could be an address. Validated below before it is believed. */
const CANDIDATE =
  /(?:[0-9A-Fa-f]{0,4}:){2,}[0-9A-Fa-f]{0,4}(?:\.\d{1,3}){0,3}(?:%[A-Za-z0-9._~-]+)?|\d{1,3}(?:\.\d{1,3}){3}/g;

/** Whether `s` is a dotted-quad with four octets in range. */
export function isIpv4(s) {
  const parts = s.split('.');
  return (
    parts.length === 4 &&
    parts.every((p) => /^\d{1,3}$/.test(p) && Number(p) <= 255)
  );
}

/** Whether `s` is an IPv6 address, compression, zone and trailing IPv4 included. */
export function isIpv6(s) {
  const addr = s.split('%')[0];
  if (!addr.includes(':')) return false;

  const halves = addr.split('::');
  if (halves.length > 2) return false;

  const split = (part) => (part === '' ? [] : part.split(':'));
  const groups = [...split(halves[0]), ...(halves.length === 2 ? split(halves[1]) : [])];

  // A trailing dotted-quad (::ffff:192.0.2.1) stands for two groups.
  let count = groups.length;
  const last = groups[groups.length - 1];
  const endsWithV4 = !!last && last.includes('.');
  if (endsWithV4) {
    if (!isIpv4(last)) return false;
    count += 1;
  }

  const hexGroups = endsWithV4 ? groups.slice(0, -1) : groups;
  if (!hexGroups.every((g) => /^[0-9A-Fa-f]{1,4}$/.test(g))) return false;

  // Compressed, the elision must stand for at least one group.
  return halves.length === 2 ? count <= 7 : count === 8;
}

/**
 * Replace every IP address in `text` with `fn(address)`.
 *
 * An OID is left alone. `1.3.6.1.2.1.1.1.0` contains `1.3.6.1`, which is a
 * perfectly valid dotted-quad, so a match that continues as digits on either
 * side is part of a longer run and not an address — otherwise anonymous mode
 * rewrote the OIDs in every description it touched.
 */
export function replaceAddresses(text, fn) {
  if (!text) return text;
  return String(text).replace(CANDIDATE, (match, offset, whole) => {
    if (isIpv6(match)) return fn(match);
    if (!isIpv4(match)) return match;
    if (/\d\.$/.test(whole.slice(0, offset))) return match;
    if (/^\.\d/.test(whole.slice(offset + match.length))) return match;
    return fn(match);
  });
}
