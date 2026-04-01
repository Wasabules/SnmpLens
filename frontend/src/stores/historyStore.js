import { writable } from 'svelte/store';

// Maximum number of history entries to keep
const MAX_HISTORY_ENTRIES = 500;

// Load history from localStorage or use empty array
const initialHistory = JSON.parse(localStorage.getItem('snmpHistory')) || [];

function createHistoryStore() {
  const { subscribe, set, update } = writable(initialHistory);

  return {
    subscribe,
    
    // Add a new history entry
    add: (entry) => {
      update(history => {
        const newEntry = {
          id: Date.now() + Math.random(), // Unique ID
          timestamp: new Date().toISOString(),
          ...entry
        };
        
        // Add to beginning of array (most recent first)
        const newHistory = [newEntry, ...history];
        
        // Limit history size
        const trimmedHistory = newHistory.slice(0, MAX_HISTORY_ENTRIES);
        
        // Save to localStorage
        localStorage.setItem('snmpHistory', JSON.stringify(trimmedHistory));
        
        return trimmedHistory;
      });
    },
    
    // Clear all history
    clear: () => {
      localStorage.removeItem('snmpHistory');
      set([]);
    },
    
    // Remove a specific entry by id
    remove: (id) => {
      update(history => {
        const filtered = history.filter(entry => entry.id !== id);
        localStorage.setItem('snmpHistory', JSON.stringify(filtered));
        return filtered;
      });
    },
    
    // Export history as JSON
    export: () => {
      const history = JSON.parse(localStorage.getItem('snmpHistory')) || [];
      return JSON.stringify(history, null, 2);
    },
    
    // Import history from JSON
    import: (jsonString) => {
      try {
        const imported = JSON.parse(jsonString);
        if (Array.isArray(imported)) {
          localStorage.setItem('snmpHistory', JSON.stringify(imported));
          set(imported);
          return true;
        }
        return false;
      } catch (error) {
        console.error('Failed to import history:', error);
        return false;
      }
    }
  };
}

export const historyStore = createHistoryStore();
