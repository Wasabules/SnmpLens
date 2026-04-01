<script>
  import { notificationStore } from './stores/notifications';
</script>

<div class="notification-container">
  {#each $notificationStore as notification (notification.id)}
    <div
      class="notification {notification.type}"
      role="button"
      tabindex="0"
      on:click={() => notificationStore.remove(notification.id)}
      on:keydown={(e) => (e.key === 'Enter' || e.key === ' ') && notificationStore.remove(notification.id)}
    >
      {notification.message}
    </div>
  {/each}
</div>

<style>
  .notification-container {
    position: fixed;
    bottom: 10px;
    right: 10px;
    z-index: 1000;
    display: flex;
    flex-direction: column-reverse;
    gap: 10px;
  }

  .notification {
    padding: 10px 20px;
    border-radius: 5px;
    color: white;
    font-size: 0.9em;
    opacity: 0.95;
    cursor: pointer;
    transition: all 0.3s ease-in-out;
    box-shadow: 0 2px 10px var(--shadow-color);
  }

  .notification.info {
    background-color: var(--accent-color);
  }

  .notification.success {
    background-color: var(--success-color);
  }

  .notification.error {
    background-color: var(--error-color);
  }

  .notification.warning {
    background-color: var(--warning-color);
  }
</style>
