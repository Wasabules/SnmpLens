import { writable } from 'svelte/store';

// A one-shot request to switch tabs, from anywhere to the shell.
//
// Used so a MIB that failed to load in Settings can be opened in the editor
// with one click. An event would have to be forwarded through the settings
// modal to reach App, and a store that carries one value is less machinery
// than two hops of plumbing.
export const tabRequest = writable(null);

/** Ask the shell to show a tab. */
export function requestTab(name) {
  tabRequest.set(name);
}
