import { chmodSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { safeStorage } from "electron";
import { keyStoreHelp } from "./keystore.js";

/**
 * The member's own LLM providers for account mode, API keys included.
 *
 * In account mode the agent loop runs here, in the executor, with the member's
 * keys; the cloud never sees them. They are stored like `pairing.bin`:
 * encrypted with `safeStorage` (the OS key store) at 0600 in userData, and
 * their plaintext only ever crosses the runner's stdin pipe, which hands them
 * to the executor's. Never argv, never an environment block, never the bridge.
 *
 * Read when the runner starts, so a change takes a runner restart.
 */

const FILE = "providers.bin";

/** The executor contract's provider shape (`desktop/runner/executor.go`). */
export interface ProviderConfig {
  id: string;
  type: string;
  base_url?: string;
  api_key?: string;
  models?: string[];
  /** The executor's per-provider request timeout; absent keeps its default. */
  timeout_seconds?: number;
}

export class ProviderStoreUnavailableError extends Error {
  constructor() {
    const { failure, remedy } = keyStoreHelp();
    super(`${failure}, so this computer's API keys cannot be stored. ${remedy}.`);
    this.name = "ProviderStoreUnavailableError";
  }
}

const IDENT = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
// eslint-disable-next-line no-control-regex -- matching control characters is the entire point.
const CONTROL_CHARS = /[\u0000-\u001f\u007f]/;
const LOOPBACK = new Set(["127.0.0.1", "localhost", "[::1]"]);

/**
 * The same rules the runner holds the list to (`checkProviders`), applied
 * before it is written and again when it is read, so a list the runner would
 * refuse at startup never reaches it. Null for anything that fails.
 */
export function asProviders(raw: unknown): ProviderConfig[] | null {
  if (!Array.isArray(raw) || raw.length > 64) return null;
  const seen = new Set<string>();
  const out: ProviderConfig[] = [];
  for (const entry of raw) {
    if (typeof entry !== "object" || entry === null) return null;
    const o = entry as Record<string, unknown>;
    if (typeof o.id !== "string" || !IDENT.test(o.id) || seen.has(o.id)) return null;
    if (typeof o.type !== "string" || !IDENT.test(o.type)) return null;
    seen.add(o.id);
    const provider: ProviderConfig = { id: o.id, type: o.type };
    if (o.base_url !== undefined) {
      if (typeof o.base_url !== "string" || !keyMayTravelTo(o.base_url)) return null;
      provider.base_url = o.base_url;
    }
    if (o.api_key !== undefined) {
      if (typeof o.api_key !== "string" || CONTROL_CHARS.test(o.api_key)) return null;
      provider.api_key = o.api_key;
    }
    if (o.timeout_seconds !== undefined) {
      const t = o.timeout_seconds;
      if (typeof t !== "number" || !Number.isInteger(t) || t < 0 || t > 3600) return null;
      provider.timeout_seconds = t;
    }
    if (o.models !== undefined) {
      if (!Array.isArray(o.models)) return null;
      const models: string[] = [];
      for (const m of o.models) {
        if (typeof m !== "string" || m === "" || m.length > 256 || CONTROL_CHARS.test(m)) return null;
        models.push(m);
      }
      provider.models = models;
    }
    out.push(provider);
  }
  return out;
}

/** https anywhere, or plain http to this machine: the key is sent to it. */
function keyMayTravelTo(raw: string): boolean {
  try {
    const url = new URL(raw);
    if (url.username !== "" || url.password !== "") return false;
    return url.protocol === "https:" || (url.protocol === "http:" && LOOPBACK.has(url.hostname));
  } catch {
    return false;
  }
}

function allowLinuxFallback(): void {
  if (process.platform !== "linux" || typeof safeStorage.getSelectedStorageBackend !== "function") return;
  if (safeStorage.getSelectedStorageBackend() === "basic_text") safeStorage.setUsePlainTextEncryption(true);
}

export class ProviderStore {
  readonly #dir: string;

  constructor(dir: string) {
    this.#dir = dir;
  }

  #path(): string {
    return path.join(this.#dir, FILE);
  }

  /** The stored list; empty when there is none or it will not decrypt. */
  read(): ProviderConfig[] {
    let ciphertext: Buffer;
    try {
      ciphertext = readFileSync(this.#path());
    } catch {
      return [];
    }
    allowLinuxFallback();
    if (!safeStorage.isEncryptionAvailable()) return [];
    try {
      return asProviders(JSON.parse(safeStorage.decryptString(ciphertext))) ?? [];
    } catch {
      return [];
    }
  }

  store(providers: ProviderConfig[]): void {
    const clean = asProviders(providers);
    if (!clean) throw new Error("the provider list failed validation and was not stored");
    allowLinuxFallback();
    if (!safeStorage.isEncryptionAvailable()) throw new ProviderStoreUnavailableError();
    mkdirSync(this.#dir, { recursive: true });
    const file = this.#path();
    writeFileSync(file, safeStorage.encryptString(JSON.stringify(clean)), { mode: 0o600 });
    chmodSync(file, 0o600);
  }

  forget(): void {
    rmSync(this.#path(), { force: true });
  }
}
