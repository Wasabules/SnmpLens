/**
 * Credential profiles: named SNMP identifiers a target can be given instead of
 * the default ones.
 *
 * A profile is a COMMUNITY (v1 or v2c) or an SNMPv3 USM USER, never both, and
 * it carries its version. "This switch speaks v3 as ops-ro" is one statement;
 * keeping the version apart from the user it goes with is how a v3 user ends up
 * sent as a v2c community. The default identifiers are not a profile — they are
 * the settings' own community and v3 block, spoken in the version chosen in the
 * header — and they stay what every target without a profile uses.
 *
 * A target's profile lives in `targetOverrides[address].profile`, beside the
 * per-target overrides it replaces for credentials, so renaming or deleting a
 * target carries it along with no code of its own.
 *
 * Pure functions only. The store, the sealing and the bridge are elsewhere, and
 * everything here runs under node in tests/profiles.test.mjs.
 */
import { AUTH_PROTOCOLS, PRIV_PROTOCOLS, SEC_LEVELS, usesAuth, usesPriv } from './snmpSecurity.js';

export const PROFILE_VERSIONS = ['v1', 'v2c', 'v3'];

/** The reference a session records when it was built from the default identifiers. */
export const DEFAULT_REF = 'default';

export const MAX_NAME = 64;
/** usmUserName is an SnmpAdminString (SIZE(1..32)) — octets, not characters (RFC 3414). */
export const MAX_USER = 32;
/** Net-SNMP refuses a USM passphrase shorter than this, and most agents follow it. */
export const MIN_PASSPHRASE = 8;

const ID_PATTERN = /^p-[a-z0-9]{4,32}$/;
const AUTH_VALUES = AUTH_PROTOCOLS.map((p) => p.value);
const PRIV_VALUES = PRIV_PROTOCOLS.map((p) => p.value);
const V3_FIELDS = ['user', 'secLevel', 'authProto', 'authPass', 'privProto', 'privPass', 'contextName'];

/**
 * What a new v3 profile starts as: authenticated and encrypted, with the most
 * widely supported pair that is not broken. SHA-256 is better and newer agents
 * have it; MD5 and DES are what the defaults above used to start at.
 */
const NEW_V3 = {
  user: '',
  secLevel: 'AuthPriv',
  authProto: 'SHA',
  authPass: '',
  privProto: 'AES',
  privPass: '',
  contextName: '',
};

/** The v3 block of a request that is not v3: nothing in it. */
const NO_V3 = {
  user: '',
  secLevel: 'NoAuthNoPriv',
  authProto: '',
  authPass: '',
  privProto: '',
  privPass: '',
  contextName: '',
};

const str = (v) => (typeof v === 'string' ? v : '');

function randomSuffix() {
  const bytes = new Uint8Array(6);
  globalThis.crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(36).padStart(2, '0')).join('').slice(0, 10);
}

/**
 * A new profile id. An id, not the name: a target keeps its profile through a
 * rename, and a session records which profile it was built from without
 * putting anything an operator typed into monitoring.db.
 */
export function newProfileId(profiles = []) {
  const taken = new Set(profiles.map((p) => p && p.id));
  let id;
  do {
    id = 'p-' + randomSuffix();
  } while (taken.has(id));
  return id;
}

/** A profile ready for the editor. */
export function blankProfile(version, profiles = []) {
  const p = { id: newProfileId(profiles), name: '', version: PROFILE_VERSIONS.includes(version) ? version : 'v2c' };
  if (p.version === 'v3') p.v3 = { ...NEW_V3 };
  else p.community = '';
  return p;
}

/**
 * A profile as it may be stored, or null when it cannot be one.
 *
 * Applied on load and on every save, because what is in localStorage is
 * whatever was last written there. Two things are dropped on purpose rather
 * than kept "just in case": the credential of the OTHER kind — a profile moved
 * from v2c to v3 does not keep its community — and a passphrase above the
 * security level. A profile holds no secret it does not use, so nothing is
 * sealed and stored that no request will ever send.
 */
export function normaliseProfile(raw) {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null;
  if (typeof raw.id !== 'string' || !ID_PATTERN.test(raw.id)) return null;
  if (!PROFILE_VERSIONS.includes(raw.version)) return null;

  const out = { id: raw.id, name: str(raw.name).slice(0, MAX_NAME), version: raw.version };
  if (raw.version !== 'v3') {
    out.community = str(raw.community);
    return out;
  }

  const v = raw.v3 && typeof raw.v3 === 'object' ? raw.v3 : {};
  const secLevel = SEC_LEVELS.includes(v.secLevel) ? v.secLevel : NEW_V3.secLevel;
  out.v3 = {
    user: str(v.user),
    secLevel,
    authProto: AUTH_VALUES.includes(v.authProto) ? v.authProto : NEW_V3.authProto,
    authPass: usesAuth(secLevel) ? str(v.authPass) : '',
    privProto: PRIV_VALUES.includes(v.privProto) ? v.privProto : NEW_V3.privProto,
    privPass: usesPriv(secLevel) ? str(v.privPass) : '',
    contextName: str(v.contextName),
  };
  return out;
}

/** The stored list, cleaned: malformed entries and repeated ids dropped. */
export function normaliseProfiles(list) {
  if (!Array.isArray(list)) return [];
  const seen = new Set();
  const out = [];
  for (const raw of list) {
    const p = normaliseProfile(raw);
    if (!p || seen.has(p.id)) continue;
    seen.add(p.id);
    out.push(p);
  }
  return out;
}

export function findProfile(settings, id) {
  if (!id) return null;
  return (settings?.credentialProfiles || []).find((p) => p && p.id === id) || null;
}

/**
 * What a profile makes a request say: its version, and ONLY the credential that
 * version uses. A v3 target is not sent the default community alongside its
 * user, and a v2c target is not sent the default v3 passphrases — neither is
 * read on the other side, and neither belongs in a request to that device.
 */
export function profileIdentity(profile) {
  if (profile.version === 'v3') {
    return { snmpVersion: 'v3', community: '', v3: { ...NO_V3, ...profile.v3 } };
  }
  return { snmpVersion: profile.version, community: profile.community || '', v3: { ...NO_V3 } };
}

/**
 * Settings with one profile's identity applied, for a request that names a
 * profile rather than a target — the connection test, a discovery scan. An id
 * that names nothing leaves the default identifiers in place.
 */
export function withProfile(settings, id) {
  const profile = findProfile(settings, id);
  if (!profile) return { ...settings, credentialRef: DEFAULT_REF };
  return { ...settings, ...profileIdentity(profile), credentialRef: profile.id };
}

/** Whether an override carries credentials of its own rather than a profile. */
export function hasCustomCredentials(override) {
  return !!override && (override.community !== undefined || override.snmpVersion !== undefined || override.v3 !== undefined);
}

/** The addresses a profile is assigned to. */
export function profileUsage(settings, id) {
  return Object.entries(settings?.targetOverrides || {})
    .filter(([, ov]) => ov && ov.profile === id)
    .map(([address]) => address);
}

/**
 * Give a target a profile, or take its profile away (`id` null), returning new
 * overrides.
 *
 * Assigning drops the target's own community, version and v3 block: two
 * answers to "what does this target authenticate with" is one too many. Its
 * port, timeout and retries are not credentials and stay.
 */
export function assignProfile(overrides, address, id) {
  const next = { ...(overrides || {}) };
  const current = { ...(next[address] || {}) };
  if (id) {
    delete current.community;
    delete current.snmpVersion;
    delete current.v3;
    current.profile = id;
  } else {
    delete current.profile;
  }
  if (Object.keys(current).length > 0) next[address] = current;
  else delete next[address];
  return next;
}

/** Remove a profile and every reference to it: its targets go back to the default identifiers. */
export function removeProfile(settings, id) {
  let overrides = settings.targetOverrides || {};
  for (const address of profileUsage(settings, id)) overrides = assignProfile(overrides, address, null);
  return {
    ...settings,
    credentialProfiles: (settings.credentialProfiles || []).filter((p) => p.id !== id),
    targetOverrides: overrides,
  };
}

/** `base`, or `base 2`, `base 3`… whichever no other profile is called. */
export function uniqueName(base, profiles, exceptId = null) {
  const taken = new Set(
    (profiles || []).filter((p) => p.id !== exceptId).map((p) => (p.name || '').trim().toLowerCase()),
  );
  const root = (base || '').trim().slice(0, MAX_NAME);
  if (!taken.has(root.toLowerCase())) return root;
  for (let n = 2; ; n++) {
    const suffix = ` ${n}`;
    const candidate = root.slice(0, MAX_NAME - suffix.length) + suffix;
    if (!taken.has(candidate.toLowerCase())) return candidate;
  }
}

const octets = (s) => new TextEncoder().encode(s).length;

/**
 * What is wrong with a profile, and what is merely worth knowing.
 *
 * Errors block saving; warnings do not, because each describes something a
 * real network may legitimately still need — an old agent that knows only MD5
 * and DES is a reason to monitor it, not a reason to refuse. Every message is an
 * i18n key, with the field it belongs to.
 */
export function validateProfile(profile, profiles = []) {
  const errors = [];
  const warnings = [];
  const name = (profile.name || '').trim();

  if (!name) errors.push({ field: 'name', key: 'profiles.err.nameRequired' });
  else if (name.length > MAX_NAME) errors.push({ field: 'name', key: 'profiles.err.nameTooLong', values: { max: MAX_NAME } });
  else if (profiles.some((p) => p.id !== profile.id && (p.name || '').trim().toLowerCase() === name.toLowerCase())) {
    errors.push({ field: 'name', key: 'profiles.err.nameTaken' });
  }

  if (profile.version !== 'v3') {
    if (!profile.community) errors.push({ field: 'community', key: 'profiles.err.communityRequired' });
    return { errors, warnings };
  }

  const v = profile.v3 || {};
  if (!v.user) errors.push({ field: 'user', key: 'profiles.err.userRequired' });
  else if (octets(v.user) > MAX_USER) errors.push({ field: 'user', key: 'profiles.err.userTooLong', values: { max: MAX_USER } });

  if (usesAuth(v.secLevel)) {
    if (!v.authPass) errors.push({ field: 'authPass', key: 'profiles.err.authPassRequired' });
    else if (v.authPass.length < MIN_PASSPHRASE) warnings.push({ field: 'authPass', key: 'profiles.warn.shortPass', values: { min: MIN_PASSPHRASE } });
    if (v.authProto === 'MD5') warnings.push({ field: 'authProto', key: 'profiles.warn.md5' });
  }
  if (usesPriv(v.secLevel)) {
    if (!v.privPass) errors.push({ field: 'privPass', key: 'profiles.err.privPassRequired' });
    else if (v.privPass.length < MIN_PASSPHRASE) warnings.push({ field: 'privPass', key: 'profiles.warn.shortPass', values: { min: MIN_PASSPHRASE } });
    if (v.privProto === 'DES') warnings.push({ field: 'privProto', key: 'profiles.warn.des' });
  }
  return { errors, warnings };
}

/**
 * Every SNMPv3 user the trap listener should accept: the default identifiers'
 * v3 block when it names a user, then every v3 profile.
 *
 * Reduced to what RECEIVING uses — no context name, nothing above the security
 * level — which is pkg/snmp's trapUser rule, so that the list compared here
 * changes exactly when the listener's would. Completeness is left to Go, which
 * refuses an unusable user with a reason instead of dropping it in silence.
 */
export function trapUsers(settings) {
  const out = [];
  const seen = new Set();
  const add = (v3) => {
    if (!v3 || !v3.user) return;
    const level = v3.secLevel || 'NoAuthNoPriv';
    const u = {
      user: v3.user,
      secLevel: level,
      authProto: usesAuth(level) ? v3.authProto || '' : '',
      authPass: usesAuth(level) ? v3.authPass || '' : '',
      privProto: usesPriv(level) ? v3.privProto || '' : '',
      privPass: usesPriv(level) ? v3.privPass || '' : '',
      contextName: '',
    };
    const key = JSON.stringify(u);
    if (seen.has(key)) return;
    seen.add(key);
    out.push(u);
  };
  add(settings?.v3);
  for (const p of settings?.credentialProfiles || []) {
    if (p && p.version === 'v3') add(p.v3);
  }
  return out;
}

/** A profile's identity as one comparable string — field by field, never key order. */
function identityKey(identity) {
  return [identity.snmpVersion ?? '', identity.community ?? '', ...V3_FIELDS.map((f) => identity.v3?.[f] ?? '')].join(' ');
}

/**
 * Which identifier sets changed between two settings: `DEFAULT_REF` when the
 * default community or v3 block did, and the id of every profile whose version
 * or credentials did.
 *
 * Compared field by field, because a session is RESTARTED when its connection
 * is replaced and a restart throws away the samples its deltas and rates are
 * derived from: a false positive is not free. A deleted profile is not a change
 * — its sessions keep what they have, the way deleting a preset stops nothing.
 */
export function changedCredentialRefs(before, after) {
  const refs = new Set();
  const defaults = (s) => identityKey({ community: s?.community ?? '', v3: s?.v3 || {} });
  if (defaults(before) !== defaults(after)) refs.add(DEFAULT_REF);

  const was = new Map((before?.credentialProfiles || []).map((p) => [p.id, p]));
  for (const p of after?.credentialProfiles || []) {
    const old = was.get(p.id);
    if (old && identityKey(profileIdentity(old)) !== identityKey(profileIdentity(p))) refs.add(p.id);
  }
  return refs;
}
