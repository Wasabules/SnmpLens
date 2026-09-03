// One entry point exporting both halves of credential custody, so a test can
// exercise the module that SEALS and the module that WRITES together.
//
// They were tested separately, and the invariant that matters — "a failure to
// seal never puts a credential in localStorage" — lives in the seam between
// them, where neither test looked.
export * from '../../src/utils/crypto.js';
export { settingsStore } from '../../src/stores/settingsStore.js';
