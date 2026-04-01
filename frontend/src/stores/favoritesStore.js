import { writable } from 'svelte/store';

const STORAGE_KEY = 'snmplens_favorites';

/**
 * @typedef {Object} Favorite
 * @property {string} oid - The OID of the favorite node
 * @property {string} name - The name of the favorite node
 * @property {string} type - The type of the favorite node (Scalar, Table, etc.)
 * @property {string} path - The full path to the node (e.g., "iso › org › dod › internet")
 * @property {number} addedAt - Timestamp when the favorite was added
 */

function createFavoritesStore() {
  const { subscribe, set, update } = writable([]);

  // Load favorites from localStorage
  function load() {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored) {
        const favorites = JSON.parse(stored);
        set(favorites);
      }
    } catch (err) {
      console.error('Failed to load favorites:', err);
      set([]);
    }
  }

  // Save favorites to localStorage
  function save(favorites) {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(favorites));
    } catch (err) {
      console.error('Failed to save favorites:', err);
    }
  }

  // Add a node to favorites
  function add(node, path) {
    update(favorites => {
      // Check if already exists
      if (favorites.some(f => f.oid === node.oid)) {
        return favorites; // Already in favorites
      }

      const newFavorite = {
        oid: node.oid,
        name: node.name,
        type: node.mibType,
        path: path || node.name,
        addedAt: Date.now(),
      };

      const updated = [...favorites, newFavorite];
      save(updated);
      return updated;
    });
  }

  // Remove a node from favorites
  function remove(oid) {
    update(favorites => {
      const updated = favorites.filter(f => f.oid !== oid);
      save(updated);
      return updated;
    });
  }

  // Check if a node is in favorites
  function isFavorite(oid, favorites) {
    return favorites.some(f => f.oid === oid);
  }

  // Clear all favorites
  function clear() {
    set([]);
    localStorage.removeItem(STORAGE_KEY);
  }

  // Export favorites to JSON
  function exportToJson() {
    let currentFavorites = [];
    subscribe(f => currentFavorites = f)();
    return JSON.stringify(currentFavorites, null, 2);
  }

  // Import favorites from JSON
  function importFromJson(jsonString) {
    try {
      const imported = JSON.parse(jsonString);
      if (Array.isArray(imported)) {
        set(imported);
        save(imported);
        return true;
      }
      return false;
    } catch (err) {
      console.error('Failed to import favorites:', err);
      return false;
    }
  }

  // Initialize by loading from localStorage
  load();

  return {
    subscribe,
    add,
    remove,
    isFavorite,
    clear,
    exportToJson,
    importFromJson,
    load,
  };
}

export const favoritesStore = createFavoritesStore();
