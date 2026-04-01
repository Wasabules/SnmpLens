/**
 * Build a base SnmpRequest object from the settings store value.
 * @param {object} settings - The $settingsStore value
 * @param {string[]} targets
 * @param {string} oid
 * @returns {object} SnmpRequest-compatible object
 */
export function buildSnmpRequest(settings, targets, oid) {
  return {
    targets,
    oid,
    community: settings.community,
    version: settings.snmpVersion,
    port: settings.port,
    timeout: settings.timeout,
    retries: settings.retries,
    v3: buildV3Params(settings),
  };
}

/**
 * Build a SetRequest object.
 * @param {object} settings
 * @param {string[]} targets
 * @param {string} oid
 * @param {string} value
 * @param {string} valueType
 * @returns {object}
 */
export function buildSetRequest(settings, targets, oid, value, valueType) {
  return {
    ...buildSnmpRequest(settings, targets, oid),
    value,
    valueType,
  };
}

/**
 * Build a GetBulkRequest object.
 * @param {object} settings
 * @param {string[]} targets
 * @param {string} oid
 * @param {number} nonRepeaters
 * @param {number} maxRepetitions
 * @returns {object}
 */
export function buildGetBulkRequest(settings, targets, oid, nonRepeaters, maxRepetitions) {
  return {
    ...buildSnmpRequest(settings, targets, oid),
    nonRepeaters,
    maxRepetitions,
  };
}

/**
 * Build a TestRequest object.
 * @param {object} settings
 * @param {string} target
 * @returns {object}
 */
export function buildTestRequest(settings, target) {
  return {
    target,
    community: settings.community,
    version: settings.snmpVersion,
    port: settings.port,
    timeout: settings.timeout,
    v3: buildV3Params(settings),
  };
}

/**
 * Build a DiscoverRequest object.
 * @param {object} settings
 * @param {string} cidr
 * @param {number} timeout
 * @returns {object}
 */
export function buildDiscoverRequest(settings, cidr, timeout) {
  return {
    cidr,
    community: settings.community,
    version: settings.snmpVersion,
    port: settings.port,
    timeout,
    v3: buildV3Params(settings),
  };
}

/**
 * Build a TrapListenerRequest object.
 * @param {object} settings
 * @returns {object}
 */
export function buildTrapListenerRequest(settings) {
  return {
    port: settings.trapPort,
    v3: buildV3Params(settings),
  };
}

/**
 * Extract V3Params from settings.
 * @param {object} settings
 * @returns {object}
 */
function buildV3Params(settings) {
  const v3 = settings.v3 || {};
  return {
    User: v3.user || '',
    AuthPass: v3.authPass || '',
    AuthProto: v3.authProto || '',
    PrivPass: v3.privPass || '',
    PrivProto: v3.privProto || '',
    SecLevel: v3.secLevel || '',
    ContextName: v3.contextName || '',
  };
}
