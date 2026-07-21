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

/**
 * Copy rich content: an HTML representation plus a plain-text fallback. Pasting
 * into Word / Google Docs / Outlook yields the formatted HTML (e.g. a real
 * table); plain-text targets (editors, terminals) get the fallback.
 * @param {string} html
 * @param {string} plain
 * @param {string} [label='Table']
 */
export async function copyRich(html, plain, label = 'Table') {
  const t = get(_);
  try {
    if (navigator.clipboard && navigator.clipboard.write && typeof ClipboardItem !== 'undefined') {
      const item = new ClipboardItem({
        'text/html': new Blob([html], { type: 'text/html' }),
        'text/plain': new Blob([plain], { type: 'text/plain' }),
      });
      await navigator.clipboard.write([item]);
    } else {
      await navigator.clipboard.writeText(plain);
    }
    notificationStore.add(t('clipboard.copied', { values: { label } }), 'success');
  } catch (err) {
    // Rich write can fail on stricter webviews — fall back to plain text.
    try {
      await navigator.clipboard.writeText(plain);
      notificationStore.add(t('clipboard.copied', { values: { label } }), 'success');
    } catch (e) {
      notificationStore.add(t('clipboard.copyError'), 'error');
    }
  }
}
