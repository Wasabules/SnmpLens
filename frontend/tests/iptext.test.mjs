// Finding addresses in free text, for anonymous mode.
//
// The old matcher was a dotted-quad regex, so an IPv6 address in a trap
// varbind, a description or an error message went out untouched in a
// screenshot — the one thing anonymous mode exists to prevent. It also
// rewrote OIDs, because 1.3.6.1.2.1.1.1.0 contains a valid dotted-quad.
import { isIpv4, isIpv6, replaceAddresses } from '../src/utils/ipText.js';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

// --- validators ---
for (const s of ['192.168.1.1', '0.0.0.0', '255.255.255.255', '8.8.8.8']) {
  check(`${s} is IPv4`, isIpv4(s));
}
for (const s of ['256.1.1.1', '1.2.3', '1.2.3.4.5', 'a.b.c.d', '']) {
  check(`${s || '(empty)'} is not IPv4`, !isIpv4(s));
}
for (const s of [
  '2001:db8::1', '::1', '::', 'fe80::1%eth0', '2001:0db8:0000:0000:0000:0000:0000:0001',
  '::ffff:192.0.2.1', '2001:db8:0:0:1::1', 'fe80::a00:27ff:fe4e:66a1',
]) {
  check(`${s} is IPv6`, isIpv6(s));
}
for (const s of [
  '1::2::3',            // two elisions
  '2001:db8:::1',       // three colons
  '2001:db8:0:0:0:0:0:0:1', // nine groups
  'gggg::1',            // not hex
  '192.168.1.1',        // the other family
  '12:34',              // a timestamp, not an address
  '',
]) {
  check(`${s || '(empty)'} is not IPv6`, !isIpv6(s), JSON.stringify(s));
}

// --- replacement in free text ---
const mask = (ip) => `<${ip}>`;

check('an IPv4 address is replaced',
  replaceAddresses('link down on 192.168.1.1 now', mask) === 'link down on <192.168.1.1> now');

check('an IPv6 address is replaced',
  replaceAddresses('link down on 2001:db8::1 now', mask) === 'link down on <2001:db8::1> now',
  replaceAddresses('link down on 2001:db8::1 now', mask));

check('a zoned link-local address is replaced',
  replaceAddresses('peer fe80::1%eth0 lost', mask) === 'peer <fe80::1%eth0> lost',
  replaceAddresses('peer fe80::1%eth0 lost', mask));

check('both families in one line are replaced',
  replaceAddresses('192.168.1.1 -> 2001:db8::64', mask) === '<192.168.1.1> -> <2001:db8::64>',
  replaceAddresses('192.168.1.1 -> 2001:db8::64', mask));

check('a bracketed address is replaced without its brackets',
  replaceAddresses('sent to [2001:db8::1]:162', mask) === 'sent to [<2001:db8::1>]:162',
  replaceAddresses('sent to [2001:db8::1]:162', mask));

// --- what must NOT be touched ---
check('an OID is left alone',
  replaceAddresses('1.3.6.1.2.1.1.1.0 = up', mask) === '1.3.6.1.2.1.1.1.0 = up',
  replaceAddresses('1.3.6.1.2.1.1.1.0 = up', mask));

check('a leading-dot OID is left alone',
  replaceAddresses('.1.3.6.1.4.1.9.1.1 nope', mask) === '.1.3.6.1.4.1.9.1.1 nope',
  replaceAddresses('.1.3.6.1.4.1.9.1.1 nope', mask));

check('an address at the end of a sentence is still replaced',
  replaceAddresses('reached 192.168.1.1.', mask) === 'reached <192.168.1.1>.',
  replaceAddresses('reached 192.168.1.1.', mask));

check('a version number is left alone',
  replaceAddresses('version 1.2.3 released', mask) === 'version 1.2.3 released');

check('a timestamp is left alone',
  replaceAddresses('at 12:34:56 today', mask) === 'at 12:34:56 today',
  replaceAddresses('at 12:34:56 today', mask));

check('empty input survives', replaceAddresses('', mask) === '');
check('null input survives', replaceAddresses(null, mask) === null);

// The mapping must be stable: the same address always gets the same label, or
// a reader cannot follow one device through a masked transcript.
{
  const seen = new Map();
  let n = 0;
  const stable = (ip) => {
    if (!seen.has(ip)) seen.set(ip, `Device-${++n}`);
    return seen.get(ip);
  };
  const out = replaceAddresses('2001:db8::1 talked to 2001:db8::2 and 2001:db8::1', stable);
  check('the same address maps to the same label',
    out === 'Device-1 talked to Device-2 and Device-1', out);
}

process.exit(failures ? 1 : 0);
