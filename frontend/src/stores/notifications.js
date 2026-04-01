import { writable } from 'svelte/store';

function createNotificationStore() {
  const { subscribe, update } = writable([]);

  function add(message, type = 'info', timeout = 5000) {
    const id = Date.now();
    const notification = { id, message, type };
    
    update(notifications => [...notifications, notification]);

    if (timeout) {
      setTimeout(() => {
        remove(id);
      }, timeout);
    }
  }

  function remove(id) {
    update(notifications => notifications.filter(n => n.id !== id));
  }

  return {
    subscribe,
    add,
    remove,
  };
}

export const notificationStore = createNotificationStore();
