import type { KeySetRequest, McpServerSetRequest } from "../../ipc/channels.js";
import {
  BUILT_IN_PROVIDER_TYPES,
  CUSTOM_PROVIDER_TYPE,
  type McpServersState,
  type ProviderKeysState,
} from "../../ipc/types.js";
import { asMcpServers, summarizeMcpServers, type McpServerConfig } from "../config/mcp-servers.js";
import { keyMayTravelTo, summarizeProviders, type ProviderConfig } from "../config/providers.js";

/**
 * The member's API keys, as the key window changes them.
 *
 * Keys go in through `set` and nowhere comes back out with one: every answer
 * is `summarizeProviders`' `hasKey`, every log line names an id and a type,
 * and every refusal is a sentence written here that quotes neither. The
 * plaintext is in this process only between the IPC call and
 * `ProviderStore.store`, and after that only on the runner's stdin
 * (`desktop/CLAUDE.md`, "Secrets for the runner go on its stdin").
 */

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const LOOPBACK = new Set(["127.0.0.1", "localhost", "[::1]"]);

function isBuiltIn(type: string): boolean {
  return (BUILT_IN_PROVIDER_TYPES as readonly string[]).includes(type);
}

function isLoopback(raw: string): boolean {
  try {
    return LOOPBACK.has(new URL(raw).hostname);
  } catch {
    return false;
  }
}

/**
 * The list after `request`, and the id it landed on.
 *
 * Ids follow one convention, so a cloud agent can name a provider without
 * knowing this computer: a built-in type's id is the type itself, and a
 * custom OpenAI-compatible endpoint's is a UUID — new ones get `newId()`.
 * A key left out keeps the stored one, so models can change without typing
 * the key again; a provider that has never had one needs one, unless it is a
 * server on this computer (`local`, or a loopback endpoint).
 */
export function applyKeySet(
  list: readonly ProviderConfig[],
  request: KeySetRequest,
  newId: () => string,
): { list: ProviderConfig[]; id: string } {
  const { type } = request;
  let id: string;
  if (isBuiltIn(type)) {
    if (request.id !== undefined && request.id !== type) {
      throw new Error(`A built-in provider's id is its type: ${type}.`);
    }
    id = type;
  } else if (type === CUSTOM_PROVIDER_TYPE) {
    if (request.id !== undefined && !UUID.test(request.id)) {
      throw new Error("A custom endpoint's id is a UUID.");
    }
    id = request.id ?? newId();
  } else {
    throw new Error("That is not a provider type this app offers.");
  }

  const existing = list.find((p) => p.id === id);
  if (existing && existing.type !== type) {
    throw new Error(`${id} is already a ${existing.type} provider. Remove it first.`);
  }

  const baseURL = request.base_url ?? existing?.base_url;
  if (type === CUSTOM_PROVIDER_TYPE && !baseURL) {
    throw new Error("A custom endpoint needs its address.");
  }
  if (baseURL !== undefined && !keyMayTravelTo(baseURL)) {
    throw new Error("The address must be https, or http to this computer: the key is sent to it.");
  }

  const apiKey = request.api_key ?? existing?.api_key;
  const keyless = type === "local" || (baseURL !== undefined && isLoopback(baseURL));
  if (!apiKey && !keyless) {
    throw new Error(`Enter the API key for ${id}.`);
  }

  const models = request.models ?? existing?.models;
  const next: ProviderConfig = {
    id,
    type,
    ...(baseURL !== undefined ? { base_url: baseURL } : {}),
    ...(apiKey ? { api_key: apiKey } : {}),
    ...(models !== undefined && models.length > 0 ? { models: [...models] } : {}),
    ...(existing?.timeout_seconds !== undefined ? { timeout_seconds: existing.timeout_seconds } : {}),
  };
  const out = existing ? list.map((p) => (p.id === id ? next : p)) : [...list, next];
  return { list: out, id };
}

export function applyKeyRemove(list: readonly ProviderConfig[], id: string): ProviderConfig[] {
  if (!list.some((p) => p.id === id)) throw new Error(`There is no provider ${id} on this computer.`);
  return list.filter((p) => p.id !== id);
}

/**
 * The list after `request`. A server added or changed takes the request's
 * transport; in `env` and `headers` an empty value keeps the stored one under
 * that name (when the server had it) and a name the request leaves out is
 * dropped, so no secret has to be typed again to change a command.
 */
export function applyMcpServerSet(list: readonly McpServerConfig[], request: McpServerSetRequest): McpServerConfig[] {
  const existing = list.find((s) => s.name === request.name);
  const merge = (sent: Record<string, string> | undefined, stored: Record<string, string> | undefined): Record<string, string> => {
    const out: Record<string, string> = {};
    for (const [key, value] of Object.entries(sent ?? {})) {
      const kept = value === "" ? stored?.[key] : value;
      if (kept !== undefined && kept !== "") out[key] = kept;
    }
    return out;
  };
  const next: McpServerConfig = { name: request.name };
  if (request.url !== undefined) {
    next.url = request.url;
    const headers = merge(request.headers, existing?.url !== undefined ? existing.headers : undefined);
    if (Object.keys(headers).length > 0) next.headers = headers;
  } else {
    next.command = request.command ?? "";
    if (request.args !== undefined && request.args.length > 0) next.args = [...request.args];
    const env = merge(request.env, existing?.command !== undefined ? existing.env : undefined);
    if (Object.keys(env).length > 0) next.env = env;
  }
  const out = existing ? list.map((s) => (s.name === request.name ? next : s)) : [...list, next];
  return out;
}

export function applyMcpServerRemove(list: readonly McpServerConfig[], name: string): McpServerConfig[] {
  if (!list.some((s) => s.name === name)) throw new Error(`There is no MCP server ${name} on this computer.`);
  return list.filter((s) => s.name !== name);
}

export interface KeyServiceDeps {
  store: { read(): ProviderConfig[]; store(providers: ProviderConfig[]): void };
  mcpStore: { read(): McpServerConfig[]; store(servers: McpServerConfig[]): void };
  /**
   * Told after every stored change. Restarts the runner when it is running —
   * it reads the list at spawn and hands it to the executor — without
   * unpairing it; answers whether it did.
   */
  changed(): boolean;
  /** One line per change. Ids and types only. */
  log(line: string): void;
  newId(): string;
}

export class KeyService {
  readonly #deps: KeyServiceDeps;

  constructor(deps: KeyServiceDeps) {
    this.#deps = deps;
  }

  list(): ProviderKeysState {
    return { providers: summarizeProviders(this.#deps.store.read()), runnerRestarting: false };
  }

  set(request: KeySetRequest): ProviderKeysState {
    const { list, id } = applyKeySet(this.#deps.store.read(), request, this.#deps.newId);
    return this.#commit(list, `stored the ${request.type} provider ${id}`);
  }

  remove(id: string): ProviderKeysState {
    return this.#commit(applyKeyRemove(this.#deps.store.read(), id), `removed the provider ${id}`);
  }

  listMcp(): McpServersState {
    return { servers: summarizeMcpServers(this.#deps.mcpStore.read()), runnerRestarting: false };
  }

  setMcp(request: McpServerSetRequest): McpServersState {
    const list = applyMcpServerSet(this.#deps.mcpStore.read(), request);
    if (!asMcpServers(list)) {
      throw new Error("That is not an MCP server the runner can use: check its name, its command or address, and its variables.");
    }
    return this.#commitMcp(list, `stored the MCP server ${request.name}`);
  }

  removeMcp(name: string): McpServersState {
    return this.#commitMcp(applyMcpServerRemove(this.#deps.mcpStore.read(), name), `removed the MCP server ${name}`);
  }

  #commitMcp(list: McpServerConfig[], line: string): McpServersState {
    this.#deps.mcpStore.store(list);
    const runnerRestarting = this.#deps.changed();
    this.#deps.log(`[keys] ${line}${runnerRestarting ? "; restarting the runner" : ""}`);
    return { servers: summarizeMcpServers(list), runnerRestarting };
  }

  #commit(list: ProviderConfig[], line: string): ProviderKeysState {
    this.#deps.store.store(list);
    const runnerRestarting = this.#deps.changed();
    this.#deps.log(`[keys] ${line}${runnerRestarting ? "; restarting the runner" : ""}`);
    return { providers: summarizeProviders(list), runnerRestarting };
  }
}
