import { derived } from 'svelte/store';
import { settingsStore } from './settingsStore';
import { getTargetLabels } from '../utils/targets';

/**
 * address -> label, for every configured target that has one.
 *
 * Charts and tiles show the label when there is one (an operator reads
 * "Router-Paris" far faster than "10.12.4.7") and keep the address in the
 * tooltip. Anonymous Mode still wins over both: a label can identify a site
 * just as plainly as an IP, so masked output stays masked.
 */
export const targetLabels = derived(settingsStore, ($settings) => getTargetLabels($settings.targets));
