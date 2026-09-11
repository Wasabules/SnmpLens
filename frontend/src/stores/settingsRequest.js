import { writable } from 'svelte/store';

// A one-shot request to open Settings on a section — and, optionally, at a
// place inside it — from anywhere in the application.
//
// The sibling of tabRequest, for the same reason: the Traps tab's "Manage
// profiles" is two components away from the dialog, and a store carrying one
// value is less machinery than an event forwarded through the shell.
export const settingsRequest = writable(null);

/** Ask the shell to open Settings on `section`, scrolled to the element `anchor`. */
export function requestSettings(section, anchor = '') {
  settingsRequest.set({ section, anchor });
}
