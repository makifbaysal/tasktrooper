import { contextBridge, ipcRenderer } from "electron";
import {
  KEYS_BRIDGE_KEY,
  KEYS_CHANNELS,
  KEYS_EVENTS,
  type KeySetRequest,
  type KeysBridge,
  type KeysPrefill,
  type McpServerSetRequest,
} from "../ipc/channels.js";
import type { McpServersState, ProviderKeysState } from "../ipc/types.js";

/**
 * The API-key window's preload: a few named calls and nothing else. The main
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
  prefill: () => call<KeysPrefill | null>(KEYS_CHANNELS.prefill),
  mcpList: () => call<McpServersState>(KEYS_CHANNELS.mcpList),
  mcpSet: (request: McpServerSetRequest) => call<McpServersState>(KEYS_CHANNELS.mcpSet, request),
  mcpRemove: (name: string) => call<McpServersState>(KEYS_CHANNELS.mcpRemove, { name }),
  onPrefill: (cb) => {
    const listener = (_event: unknown, prefill: KeysPrefill | null): void => cb(prefill);
    ipcRenderer.on(KEYS_EVENTS.prefill, listener);
    return () => {
      ipcRenderer.removeListener(KEYS_EVENTS.prefill, listener);
    };
  },
};

contextBridge.exposeInMainWorld(KEYS_BRIDGE_KEY, Object.freeze(bridge));
