import { contextBridge, ipcRenderer } from "electron";
import { KEYS_BRIDGE_KEY, KEYS_CHANNELS, type KeySetRequest, type KeysBridge } from "../ipc/channels.js";
import type { ProviderKeysState } from "../ipc/types.js";

/**
 * The API-key window's preload: three named calls and nothing else. The main
 * process answers them for this window's own page only, and none of them
 * hands a key back — `set` takes one in, every answer says `hasKey`.
 */

async function call<T>(channel: string, payload?: unknown): Promise<T> {
  try {
    return (await ipcRenderer.invoke(channel, payload)) as T;
  } catch (err) {
    const raw = err instanceof Error ? err.message : String(err);
    const match = /Error invoking remote method '[^']+': (?:Error: )?(.*)/s.exec(raw);
    throw new Error((match?.[1] ?? raw).trim());
  }
}

const bridge: KeysBridge = {
  list: () => call<ProviderKeysState>(KEYS_CHANNELS.list),
  set: (request: KeySetRequest) => call<ProviderKeysState>(KEYS_CHANNELS.set, request),
  remove: (id: string) => call<ProviderKeysState>(KEYS_CHANNELS.remove, { id }),
};

contextBridge.exposeInMainWorld(KEYS_BRIDGE_KEY, Object.freeze(bridge));
