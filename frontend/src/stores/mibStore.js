import { writable, get } from 'svelte/store';
import { LoadAllMibs, LoadEnabledMibs, LoadMibsWithDiagnostics, GetPersistentMibDirectory } from '../../wailsjs/go/main/App';
import { notificationStore } from './notifications';
import { mibPathsStore } from './mibPathsStore';

export const mibDiagnostics = writable([]);

function createMibStore() {
  const { subscribe, set, update } = writable({
    tree: [],
    isLoading: true,
    error: null,
  });

  // Add parent references to all nodes in the tree
  function addParentReferences(nodes, parent = null) {
    if (!nodes) return;
    nodes.forEach(node => {
      node.parent = parent;
      if (node.children && node.children.length > 0) {
        addParentReferences(node.children, node);
      }
    });
  }

  // Get list of enabled MIB files from mibPathsStore
  async function getEnabledMibFiles() {
    const pathsState = get(mibPathsStore);
    const defaultPath = await GetPersistentMibDirectory();
    const enabledFiles = [];

    // Get enabled MIBs from default path
    if (pathsState.detectedMibs[defaultPath]) {
      pathsState.detectedMibs[defaultPath].forEach(mib => {
        if (pathsState.enabledMibs[defaultPath]?.[mib] !== false) {
          enabledFiles.push(mib);
        }
      });
    }

    // Get enabled MIBs from custom paths
    pathsState.customPaths.forEach(customPath => {
      if (pathsState.detectedMibs[customPath]) {
        pathsState.detectedMibs[customPath].forEach(mib => {
          if (pathsState.enabledMibs[customPath]?.[mib] !== false) {
            enabledFiles.push(mib);
          }
        });
      }
    });

    return enabledFiles;
  }

  async function loadInternal(silent = false) {
    update(store => ({ ...store, isLoading: true, error: null }));
    try {
      const enabledFiles = await getEnabledMibFiles();

      console.log(`Loading ${enabledFiles.length} enabled MIBs with diagnostics`);
      const response = await LoadMibsWithDiagnostics(enabledFiles);

      const tree = response.tree || [];
      const diagnostics = response.diagnostics || [];

      // Store diagnostics
      mibDiagnostics.set(diagnostics);

      // Add parent references for breadcrumb navigation
      addParentReferences(tree);
      set({ tree, isLoading: false, error: null });

      if (!silent) {
        const successCount = diagnostics.filter(d => d.success).length;
        const failCount = diagnostics.filter(d => !d.success).length;

        if (tree.length > 0) {
          let msg = `${successCount} MIB(s) loaded successfully.`;
          if (failCount > 0) {
            msg += ` ${failCount} failed.`;
          }
          notificationStore.add(msg, failCount > 0 ? 'info' : 'success');
        } else {
          notificationStore.add('No MIBs found or loaded from the directory.', 'info');
        }
      }
    } catch (err) {
      set({ tree: [], isLoading: false, error: err });
      mibDiagnostics.set([]);
      if (!silent) {
        notificationStore.add(`Error loading MIBs: ${err}`, 'error');
      }
    }
  }

  return {
    subscribe,
    load: () => loadInternal(false),
    loadSilent: () => loadInternal(true),
  };
}

export const mibStore = createMibStore();
