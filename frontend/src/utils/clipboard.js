import { get } from 'svelte/store';
import { _ } from 'svelte-i18n';
import { notificationStore } from '../stores/notifications';

/**
 * Copy text to clipboard and show a notification.
 * @param {string} text
 * @param {string} [label='Value']
 */
export async function copyToClipboard(text, label = 'Value') {
  const t = get(_);
  try {
    await navigator.clipboard.writeText(text);
    notificationStore.add(t('clipboard.copied', { values: { label } }), 'success');
  } catch (err) {
    notificationStore.add(t('clipboard.copyError'), 'error');
  }
}
