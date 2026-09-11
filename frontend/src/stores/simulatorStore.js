import { writable, derived } from 'svelte/store';
import {
  ListSimulatorModels,
  ListSimulatorModelIcons,
  ListSimulatedDevices,
  SimulatorSaveDevice,
  SimulatorDeleteDevice,
  SimulatorStartDevice,
  SimulatorStopDevice,
  SimulatorDeviceCredentials,
  SimulatorSuggestAddress,
  SimulatorSendTrap,
  ImportSimulatorModelsDialog,
  SimulatorDeleteModel,
  TrapListenerEngineID,
} from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

/**
 * The simulated devices and the models they are made from, as Go holds them.
 *
 * Go keeps the only copy: this store lists and asks, and a change comes back
 * as 'simulator:changed' — which is also how the status in the header learns
 * of a device started from the modal, or stopped by the application closing a
 * session — or as 'simulator:models' when a model is imported or deleted.
 * Nothing here is persisted; the lists are re-read.
 */
function createSimulatorStore() {
  const { subscribe, update } = writable({ models: [], icons: {}, devices: [], loaded: false });
  let subscribed = false;

  /** The devices, re-read: what the modal polls while it is open. */
  async function refresh() {
    const devices = await ListSimulatedDevices();
    update((s) => ({ ...s, devices: devices || [], loaded: true }));
  }

  /**
   * The models, and the custom ones' icons by model ID. Re-read when they
   * change and not with the devices: an icon crosses the bridge as a data URI,
   * and the devices are polled every two seconds.
   */
  async function refreshModels() {
    const [models, icons] = await Promise.all([ListSimulatorModels(), ListSimulatorModelIcons()]);
    update((s) => ({
      ...s,
      models: models || [],
      icons: Object.fromEntries((icons || []).map((i) => [i.id, i.icon])),
    }));
  }

  /** Subscribes once, however many times it is called, and lists. */
  function init() {
    if (!subscribed) {
      subscribed = true;
      EventsOn('simulator:changed', () => {
        refresh().catch(() => {});
      });
      // A model imported again restarts the devices made from it.
      EventsOn('simulator:models', () => {
        Promise.all([refreshModels(), refresh()]).catch(() => {});
      });
    }
    return Promise.all([refreshModels(), refresh()]);
  }

  return {
    subscribe,
    init,
    refresh,
    refreshModels,
    save: (device) => SimulatorSaveDevice(device),
    remove: (id) => SimulatorDeleteDevice(id),
    start: (id) => SimulatorStartDevice(id),
    stop: (id) => SimulatorStopDevice(id),
    credentials: async (id) => (await SimulatorDeviceCredentials(id)) || { community: '', users: {}, destinations: {} },
    suggestAddress: () => SimulatorSuggestAddress(),
    /** Sends a notification now; resolves to what became of it at each destination. */
    sendTrap: async (id, name) => (await SimulatorSendTrap(id, name)) || [],
    /** Asks for model files, JSON or ZIP, and imports them; resolves to what became of each. */
    importModels: async () => (await ImportSimulatorModelsDialog()) || [],
    removeModel: (id) => SimulatorDeleteModel(id),
    /** The engine ID SnmpLens's own trap listener stands for, in hex. */
    listenerEngineId: async () => (await TrapListenerEngineID()) || '',
  };
}

export const simulatorStore = createSimulatorStore();

/** How many simulated devices are answering: the figure in the header. */
export const runningSimulated = derived(simulatorStore, ($s) => $s.devices.filter((d) => d.running).length);
