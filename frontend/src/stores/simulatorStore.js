import { writable, derived } from 'svelte/store';
import {
  ListSimulatorModels,
  ListSimulatedDevices,
  SimulatorSaveDevice,
  SimulatorDeleteDevice,
  SimulatorStartDevice,
  SimulatorStopDevice,
  SimulatorDeviceCredentials,
  SimulatorSuggestAddress,
  SimulatorSendTrap,
  TrapListenerEngineID,
} from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

/**
 * The simulated devices, as Go holds them.
 *
 * Go keeps the only copy: this store lists and asks, and a change comes back
 * as 'simulator:changed' — which is also how the status in the header learns
 * of a device started from the modal, or stopped by the application closing a
 * session. Nothing here is persisted; the list is re-read.
 */
function createSimulatorStore() {
  const { subscribe, set } = writable({ models: [], devices: [], loaded: false });
  let subscribed = false;

  async function refresh() {
    const [models, devices] = await Promise.all([ListSimulatorModels(), ListSimulatedDevices()]);
    set({ models: models || [], devices: devices || [], loaded: true });
  }

  /** Subscribes once, however many times it is called, and lists. */
  function init() {
    if (!subscribed) {
      subscribed = true;
      EventsOn('simulator:changed', () => {
        refresh().catch(() => {});
      });
    }
    return refresh();
  }

  return {
    subscribe,
    init,
    refresh,
    save: (device) => SimulatorSaveDevice(device),
    remove: (id) => SimulatorDeleteDevice(id),
    start: (id) => SimulatorStartDevice(id),
    stop: (id) => SimulatorStopDevice(id),
    credentials: async (id) => (await SimulatorDeviceCredentials(id)) || { community: '', users: {}, destinations: {} },
    suggestAddress: () => SimulatorSuggestAddress(),
    /** Sends a notification now; resolves to what became of it at each destination. */
    sendTrap: async (id, name) => (await SimulatorSendTrap(id, name)) || [],
    /** The engine ID SnmpLens's own trap listener stands for, in hex. */
    listenerEngineId: async () => (await TrapListenerEngineID()) || '',
  };
}

export const simulatorStore = createSimulatorStore();

/** How many simulated devices are answering: the figure in the header. */
export const runningSimulated = derived(simulatorStore, ($s) => $s.devices.filter((d) => d.running).length);
