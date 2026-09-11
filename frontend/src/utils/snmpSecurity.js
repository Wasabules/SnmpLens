/**
 * The SNMPv3 choices, declared once.
 *
 * The default identifiers offered six authentication and six privacy protocols
 * while the per-target override form offered four and three, so a target could
 * not be given SHA-384 or AES-192 at all — and the labels were written out once
 * per form. The VALUES are the strings pkg/snmp maps (getAuthProtocol,
 * getPrivProtocol in client.go), and tests/profiles.test.mjs holds the two
 * lists together; the labels are what an operator reads in the device's own
 * configuration.
 */

export const SEC_LEVELS = ['NoAuthNoPriv', 'AuthNoPriv', 'AuthPriv'];

export const AUTH_PROTOCOLS = [
  { value: 'MD5', label: 'MD5' },
  { value: 'SHA', label: 'SHA' },
  { value: 'SHA224', label: 'SHA-224' },
  { value: 'SHA256', label: 'SHA-256' },
  { value: 'SHA384', label: 'SHA-384' },
  { value: 'SHA512', label: 'SHA-512' },
];

export const PRIV_PROTOCOLS = [
  { value: 'DES', label: 'DES' },
  { value: 'AES', label: 'AES-128' },
  { value: 'AES192C', label: 'AES-192' },
  { value: 'AES256C', label: 'AES-256' },
  { value: 'AES192', label: 'AES-192 (Blumenthal)' },
  { value: 'AES256', label: 'AES-256 (Blumenthal)' },
];

/** Whether a security level authenticates. */
export const usesAuth = (level) => level === 'AuthNoPriv' || level === 'AuthPriv';

/** Whether a security level encrypts. */
export const usesPriv = (level) => level === 'AuthPriv';

/** The label of a protocol value, or the value itself when it is not listed. */
export function protocolLabel(list, value) {
  return list.find((p) => p.value === value)?.label || value || '';
}
