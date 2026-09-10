// The polling store and the settings store it reads, from ONE bundle.
//
// A test that gives targets credential profiles has to put them in the same
// settings store the polling store reads, and a second bundle would carry a
// second, empty copy of it.
export * from '../../src/stores/pollingStore.js';
export { settingsStore } from '../../src/stores/settingsStore.js';
// The store names split sessions through svelte-i18n, and a test has to set up
// the instance the bundle carries rather than a second one of its own.
export { addMessages, init as initI18n } from 'svelte-i18n';
