import { InitializeNotifications, SendNotification, IsNotificationAvailable } from '../../wailsjs/runtime/runtime';

let initialized = false;
let available = false;
let counter = 0;

/**
 * Initialize the native notification system. Call once at app startup.
 */
export async function initNotifications() {
  try {
    available = await IsNotificationAvailable();
    if (available) {
      await InitializeNotifications();
      initialized = true;
      console.log('Native notifications initialized');
    } else {
      console.log('Native notifications not available on this platform');
    }
  } catch (e) {
    console.warn('Failed to initialize native notifications:', e);
  }
}

/**
 * Send a native OS notification (Windows toast, macOS notification center, Linux).
 * @param {string} title
 * @param {string} body
 * @param {string} [subtitle]
 */
export async function sendNativeNotification(title, body, subtitle) {
  if (!initialized || !available) return;

  try {
    await SendNotification({
      id: `snmplens-${++counter}`,
      title,
      body: body || '',
      subtitle: subtitle || '',
    });
  } catch (e) {
    console.warn('Failed to send native notification:', e);
  }
}
