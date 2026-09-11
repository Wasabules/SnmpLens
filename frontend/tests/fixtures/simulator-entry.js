// The simulator's renderer-side rules, from one bundle, with what a device added
// as a target has to satisfy. targets.js reaches the settings store through
// anonymize.js, and the store reaches the Wails bridge, so it cannot be imported
// under node directly.
export * from '../../src/utils/simulator.js';
export { getEffectiveSettings, portOf } from '../../src/utils/targets.js';
