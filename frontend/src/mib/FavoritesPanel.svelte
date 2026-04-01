<script>
  import { favoritesStore } from '../stores/favoritesStore';
  import { notificationStore } from '../stores/notifications';
  import { createEventDispatcher } from 'svelte';
  import { _ } from 'svelte-i18n';
  import { get } from 'svelte/store';

  const dispatch = createEventDispatcher();

  // Get icon from type string (for favorites)
  function getNodeIconFromType(type) {
    const icons = {
      'Scalar': '📊',
      'Column': '📑',
      'Table': '📋',
      'Notification': '🔔',
      'Node': '📁',
      'Group': '📦',
    };
    return icons[type] || '📄';
  }
</script>

{#if $favoritesStore.length > 0}
  <div class="favorites-panel">
    <div class="favorites-header">
      <span class="favorites-title">⭐ {$_('favorites.title', { values: { count: $favoritesStore.length } })}</span>
      <button
        class="btn-icon"
        on:click={() => {
          if (confirm(get(_)('favorites.clearConfirm'))) {
            favoritesStore.clear();
            notificationStore.add(get(_)('favorites.cleared'), 'info');
          }
        }}
        title={$_('favorites.clearAll')}
      >
        🗑️
      </button>
    </div>
    <div class="favorites-list">
      {#each $favoritesStore as favorite (favorite.oid)}
        <div
          class="favorite-item"
          on:click={() => dispatch('navigate', favorite)}
          on:keydown={(e) => e.key === 'Enter' && dispatch('navigate', favorite)}
          role="button"
          tabindex="0"
          title="{favorite.path}\nOID: {favorite.oid}"
        >
          <span class="favorite-icon">{getNodeIconFromType(favorite.type)}</span>
          <span class="favorite-name">{favorite.name}</span>
          <button
            class="btn-remove"
            on:click|stopPropagation={() => favoritesStore.remove(favorite.oid)}
            title={$_('favorites.removeFrom')}
          >
            ✕
          </button>
        </div>
      {/each}
    </div>
  </div>
{/if}

<style>
  .favorites-panel {
    background-color: var(--bg-lighter-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    margin-bottom: 10px;
    overflow: hidden;
  }

  .favorites-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 10px;
    background-color: var(--favorites-subtle);
    border-bottom: 1px solid var(--border-color);
  }

  .favorites-title {
    font-size: 0.9em;
    font-weight: 600;
    color: var(--favorites-color);
  }

  .favorites-list {
    max-height: 200px;
    overflow-y: auto;
    padding: 5px;
  }

  .favorite-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    margin-bottom: 3px;
    background-color: var(--bg-color);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.2s;
    font-size: 0.85em;
  }

  .favorite-item:hover {
    background-color: var(--accent-color);
    border-color: var(--accent-color);
  }

  .favorite-icon {
    font-size: 1.1em;
    line-height: 1;
  }

  .favorite-name {
    flex-grow: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .btn-remove {
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 1.1em;
    padding: 0 4px;
    line-height: 1;
    opacity: 0.6;
    transition: opacity 0.2s;
  }

  .btn-remove:hover {
    opacity: 1;
    color: var(--error-color);
  }

  .favorite-item:hover .btn-remove {
    color: white;
  }

  .btn-icon {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 1em;
    padding: 2px 6px;
    border-radius: 3px;
    transition: background-color 0.2s;
  }

  .btn-icon:hover {
    background-color: var(--hover-overlay-medium);
  }

  /* Scrollbar for favorites list */
  .favorites-list::-webkit-scrollbar {
    width: 6px;
  }

  .favorites-list::-webkit-scrollbar-track {
    background: var(--bg-color);
  }

  .favorites-list::-webkit-scrollbar-thumb {
    background: var(--border-color);
    border-radius: 3px;
  }

  .favorites-list::-webkit-scrollbar-thumb:hover {
    background: var(--bg-disabled-hover);
  }
</style>
