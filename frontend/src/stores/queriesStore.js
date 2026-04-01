import { writable } from 'svelte/store';

const STORAGE_KEY = 'snmplens_saved_queries';
const MAX_QUERIES = 100;

function createQueriesStore() {
  const { subscribe, set, update } = writable([]);

  function load() {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored) set(JSON.parse(stored));
    } catch (err) {
      console.error('Failed to load queries:', err);
      set([]);
    }
  }

  function save(queries) {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(queries));
    } catch (err) {
      console.error('Failed to save queries:', err);
    }
  }

  function add(query) {
    update(queries => {
      const newQuery = {
        id: Date.now() + Math.random(),
        createdAt: new Date().toISOString(),
        lastUsedAt: new Date().toISOString(),
        ...query,
      };
      const updated = [newQuery, ...queries].slice(0, MAX_QUERIES);
      save(updated);
      return updated;
    });
  }

  function remove(id) {
    update(queries => {
      const updated = queries.filter(q => q.id !== id);
      save(updated);
      return updated;
    });
  }

  function markUsed(id) {
    update(queries => {
      const updated = queries.map(q =>
        q.id === id ? { ...q, lastUsedAt: new Date().toISOString() } : q
      );
      save(updated);
      return updated;
    });
  }

  load();

  return { subscribe, add, remove, markUsed, load };
}

export const queriesStore = createQueriesStore();
