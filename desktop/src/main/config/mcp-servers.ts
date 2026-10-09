import { chmodSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { safeStorage } from "electron";
import type { McpServerSummary } from "../../ipc/types.js";
import { isReservedMcpName } from "../../ipc/mcp-names.js";
import { keyStoreHelp } from "./keystore.js";

/**
 * The member's own MCP servers on this computer, for account mode.
 *
 * Stored like `providers.bin`: encrypted with `safeStorage` at 0600 in
 * userData. Their `env` and `headers` carry tokens, so the plaintext only ever
 * crosses the runner's stdin pipe, which hands it to the executor's — never
 * argv, never an environment block of this app's, never the bridge. Written
 * only by the API-key window (`main/keys/`), which shows names and
 * `hasSecret`, never a value.
 */

const FILE = "mcp-servers.bin";

/** The executor contract's shape (`desktop/runner/executor.go`, `mcpServerConfig`). */
export interface McpServerConfig {
  name: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  headers?: Record<string, string>;
}

export class McpServerStoreUnavailableError extends Error {
  constructor() {
    const { failure, remedy } = keyStoreHelp();
    super(`${failure}, so this computer's MCP servers cannot be stored. ${remedy}.`);
    this.name = "McpServerStoreUnavailableError";
  }
}

export const MAX_MCP_SERVERS = 32;
const NAME = /^[A-Za-z0-9][A-Za-z0-9-]{0,31}$/;
const ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]{0,127}$/;
const HEADER_NAME = /^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$/;
// eslint-disable-next-line no-control-regex -- matching control characters is the entire point.
const CONTROL_CHARS = /[\u0000-\u001f\u007f]/;

function isStringMap(raw: unknown, key: RegExp, valueOk: (v: string) => boolean): raw is Record<string, string> {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return false;
  const entries = Object.entries(raw);
  return entries.length <= 64 && entries.every(([k, v]) => key.test(k) && typeof v === "string" && v.length <= 4096 && valueOk(v));
}

/** An http(s) address without credentials in it. */
export function isMcpUrl(raw: string): boolean {
  try {
    const url = new URL(raw);
    return (url.protocol === "https:" || url.protocol === "http:") && url.host !== "" && url.username === "" && url.password === "";
  } catch {
    return false;
  }
}

/**
 * The same rules the runner holds the list to (`checkMCPServers`), applied
 * before it is written and again when it is read, so a list the runner would
 * refuse at startup never reaches it. Null for anything that fails.
 */
export function asMcpServers(raw: unknown): McpServerConfig[] | null {
  if (!Array.isArray(raw) || raw.length > MAX_MCP_SERVERS) return null;
  const seen = new Set<string>();
  const out: McpServerConfig[] = [];
  for (const entry of raw) {
    if (typeof entry !== "object" || entry === null) return null;
    const o = entry as Record<string, unknown>;
    if (typeof o.name !== "string" || !NAME.test(o.name) || isReservedMcpName(o.name) || seen.has(o.name)) return null;
    seen.add(o.name);
    const hasCommand = typeof o.command === "string" && o.command.trim() !== "";
    const hasUrl = typeof o.url === "string" && o.url.trim() !== "";
    if (hasCommand === hasUrl) return null;
    const server: McpServerConfig = { name: o.name };
    if (hasUrl) {
      if (o.args !== undefined || o.env !== undefined) return null;
      const url = (o.url as string).trim();
      if (!isMcpUrl(url)) return null;
      server.url = url;
      if (o.headers !== undefined) {
        if (!isStringMap(o.headers, HEADER_NAME, (v) => !CONTROL_CHARS.test(v))) return null;
        if (Object.keys(o.headers).length > 0) server.headers = { ...o.headers };
      }
    } else {
      if (o.headers !== undefined) return null;
      const command = (o.command as string).trim();
      if (CONTROL_CHARS.test(command) || command.length > 1024) return null;
      server.command = command;
      if (o.args !== undefined) {
        if (!Array.isArray(o.args) || o.args.length > 64) return null;
        if (!o.args.every((a) => typeof a === "string" && a.length <= 4096 && !a.includes("\u0000"))) return null;
        if (o.args.length > 0) server.args = [...(o.args as string[])];
      }
      if (o.env !== undefined) {
        if (!isStringMap(o.env, ENV_NAME, (v) => !v.includes("\u0000"))) return null;
        if (Object.keys(o.env).length > 0) server.env = { ...o.env };
      }
    }
    out.push(server);
  }
  return out;
}

/** An address with its query and fragment dropped: a token is sometimes put there. */
function displayUrl(raw: string): string {
  try {
    const url = new URL(raw);
    return `${url.origin}${url.pathname}`;
  } catch {
    return "";
  }
}

/**
 * The list as the key screen may show it: names, where it runs and which
 * variables hold a value — never a value.
 */
export function summarizeMcpServers(list: readonly McpServerConfig[]): McpServerSummary[] {
  return list.map((s) => {
    const secretNames = Object.keys(s.url !== undefined ? (s.headers ?? {}) : (s.env ?? {}));
    return {
      name: s.name,
      transport: s.url !== undefined ? "http" : "stdio",
      ...(s.command !== undefined ? { command: s.command } : {}),
      ...(s.args !== undefined ? { args: [...s.args] } : {}),
      ...(s.url !== undefined ? { url: displayUrl(s.url) } : {}),
      secretNames,
      hasSecret: secretNames.length > 0,
    };
  });
}

function allowLinuxFallback(): void {
  if (process.platform !== "linux" || typeof safeStorage.getSelectedStorageBackend !== "function") return;
  if (safeStorage.getSelectedStorageBackend() === "basic_text") safeStorage.setUsePlainTextEncryption(true);
}

export class McpServerStore {
  readonly #dir: string;

  constructor(dir: string) {
    this.#dir = dir;
  }

  #path(): string {
    return path.join(this.#dir, FILE);
  }

  /** The stored list; empty when there is none or it will not decrypt. */
  read(): McpServerConfig[] {
    let ciphertext: Buffer;
    try {
      ciphertext = readFileSync(this.#path());
    } catch {
      return [];
    }
    allowLinuxFallback();
    if (!safeStorage.isEncryptionAvailable()) return [];
    try {
      return asMcpServers(JSON.parse(safeStorage.decryptString(ciphertext))) ?? [];
    } catch {
      return [];
    }
  }

  store(servers: McpServerConfig[]): void {
    const clean = asMcpServers(servers);
    if (!clean) throw new Error("the MCP server list failed validation and was not stored");
    allowLinuxFallback();
    if (!safeStorage.isEncryptionAvailable()) throw new McpServerStoreUnavailableError();
    mkdirSync(this.#dir, { recursive: true });
    const file = this.#path();
    writeFileSync(file, safeStorage.encryptString(JSON.stringify(clean)), { mode: 0o600 });
    chmodSync(file, 0o600);
  }

  forget(): void {
    rmSync(this.#path(), { force: true });
  }
}
