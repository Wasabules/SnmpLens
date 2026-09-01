import { writable } from 'svelte/store';

// Monotonic, collision-free. Date.now() gave two toasts raised in the same
// synchronous tick the SAME id: they collided as {#each} keys, and removing
// one dismissed both.
let idSeq = 0;

function createNotificationStore() {
  const { subscribe, update } = writable([]);

  function add(message, type = 'info', timeout = 5000) {
    const id = `${Date.now()}-${idSeq++}`;
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
