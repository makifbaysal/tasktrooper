import { chmodSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { app, safeStorage } from "electron";
import { REMOTE_MODES, REMOTE_PROVIDERS, type RemoteAccessConfig, type RemoteSecretsView } from "../../ipc/types.js";
import { keyStoreHelp } from "../config/keystore.js";
import { normalizeHostname } from "../../ipc/validate.js";

/**
 * Remote access settings on disk.
 *
 * The non-secret half is plain JSON beside settings.json (`remote-access.json`):
 * a person may read or paste it. The two secrets (a Cloudflare tunnel token, an
 * ngrok authtoken) are bearer credentials for the person's own tunnel account,
 * so they live in `remote-access.bin`, `safeStorage`-encrypted at 0600 like
 * `pairing.bin` — never in the JSON, never on argv, never back across the bridge.
 */

const CONFIG_FILE = "remote-access.json";
const SECRETS_FILE = "remote-access.bin";

/**
 * A port the server's remote listener asks for first. Stable on purpose: a named
 * Cloudflare tunnel's public hostname is configured in the Cloudflare dashboard
 * to point at one `http://127.0.0.1:<port>`, and a random port per launch would
 * break that every time.
 */
export const DEFAULT_REMOTE_PORT = 47820;

export interface RemoteSecrets {
  cloudflareToken?: string;
  ngrokAuthtoken?: string;
}

export function defaultRemoteConfig(): RemoteAccessConfig {
  return { provider: "cloudflare", mode: "quick", hostname: "", autoStart: false, serverName: "" };
}

export interface RemoteAccessStoreLike {
  config(): RemoteAccessConfig;
  setConfig(patch: Partial<RemoteAccessConfig>): RemoteAccessConfig;
  lastPort(): number;
  setLastPort(port: number): void;
  secrets(): RemoteSecrets;
  setSecrets(set: RemoteSecrets, clear?: readonly (keyof RemoteSecrets)[]): void;
}

export function secretsView(secrets: RemoteSecrets): RemoteSecretsView {
  return { cloudflareToken: !!secrets.cloudflareToken, ngrokAuthtoken: !!secrets.ngrokAuthtoken };
}

function allowLinuxFallback(): void {
  if (process.platform !== "linux" || typeof safeStorage.getSelectedStorageBackend !== "function") return;
  if (safeStorage.getSelectedStorageBackend() === "basic_text") safeStorage.setUsePlainTextEncryption(true);
}

export class RemoteSecretsUnavailableError extends Error {
  constructor() {
    const { failure, remedy } = keyStoreHelp();
    super(`${failure}, so the tunnel token cannot be stored. ${remedy}.`);
    this.name = "RemoteSecretsUnavailableError";
  }
}

/** A hand-edited remote-access.json is untrusted the way an IPC payload is. */
export function asRemoteConfig(raw: unknown): RemoteAccessConfig {
  const out = defaultRemoteConfig();
  if (typeof raw !== "object" || raw === null) return out;
  const o = raw as Record<string, unknown>;
  if (typeof o.provider === "string" && (REMOTE_PROVIDERS as readonly string[]).includes(o.provider)) {
    out.provider = o.provider as RemoteAccessConfig["provider"];
  }
  if (typeof o.mode === "string" && (REMOTE_MODES as readonly string[]).includes(o.mode)) {
    out.mode = o.mode as RemoteAccessConfig["mode"];
  }
  if (typeof o.hostname === "string") out.hostname = normalizeHostname(o.hostname) ?? "";
  if (typeof o.autoStart === "boolean") out.autoStart = o.autoStart;
  // eslint-disable-next-line no-control-regex -- control characters are exactly what is removed.
  if (typeof o.serverName === "string") out.serverName = o.serverName.replace(/[\u0000-\u001f\u007f]/g, "").trim().slice(0, 80);
  return out;
}

export class RemoteAccessStore implements RemoteAccessStoreLike {
  readonly #dir: string;
  #cache: { config: RemoteAccessConfig; lastPort: number } | null = null;

  constructor(dir = app.getPath("userData")) {
    this.#dir = dir;
  }

  #state(): { config: RemoteAccessConfig; lastPort: number } {
    if (this.#cache) return this.#cache;
    let raw: unknown = {};
    try {
      raw = JSON.parse(readFileSync(path.join(this.#dir, CONFIG_FILE), "utf8"));
    } catch {
      raw = {};
    }
    const port = (raw as { lastPort?: unknown } | null)?.lastPort;
    this.#cache = {
      config: asRemoteConfig(raw),
      lastPort: typeof port === "number" && Number.isInteger(port) && port > 1024 && port < 65536 ? port : DEFAULT_REMOTE_PORT,
    };
    return this.#cache;
  }

  #write(next: { config: RemoteAccessConfig; lastPort: number }): void {
    mkdirSync(this.#dir, { recursive: true });
    writeFileSync(path.join(this.#dir, CONFIG_FILE), `${JSON.stringify({ ...next.config, lastPort: next.lastPort }, null, 2)}\n`, "utf8");
    this.#cache = next;
  }

  config(): RemoteAccessConfig {
    return { ...this.#state().config };
  }

  setConfig(patch: Partial<RemoteAccessConfig>): RemoteAccessConfig {
    const state = this.#state();
    const config = asRemoteConfig({ ...state.config, ...patch });
    this.#write({ ...state, config });
    return { ...config };
  }

  lastPort(): number {
    return this.#state().lastPort;
  }

  setLastPort(port: number): void {
    const state = this.#state();
    if (state.lastPort !== port) this.#write({ ...state, lastPort: port });
  }

  secrets(): RemoteSecrets {
    let ciphertext: Buffer;
    try {
      ciphertext = readFileSync(path.join(this.#dir, SECRETS_FILE));
    } catch {
      return {};
    }
    allowLinuxFallback();
    if (!safeStorage.isEncryptionAvailable()) return {};
    try {
      const raw = JSON.parse(safeStorage.decryptString(ciphertext)) as Record<string, unknown>;
      const out: RemoteSecrets = {};
      if (typeof raw.cloudflareToken === "string" && raw.cloudflareToken !== "") out.cloudflareToken = raw.cloudflareToken;
      if (typeof raw.ngrokAuthtoken === "string" && raw.ngrokAuthtoken !== "") out.ngrokAuthtoken = raw.ngrokAuthtoken;
      return out;
    } catch {
      return {};
    }
  }

  setSecrets(set: RemoteSecrets, clear: readonly (keyof RemoteSecrets)[] = []): void {
    const merged: RemoteSecrets = { ...this.secrets(), ...set };
    for (const name of clear) delete merged[name];
    const file = path.join(this.#dir, SECRETS_FILE);
    if (!merged.cloudflareToken && !merged.ngrokAuthtoken) {
      rmSync(file, { force: true });
      return;
    }
    allowLinuxFallback();
    if (!safeStorage.isEncryptionAvailable()) throw new RemoteSecretsUnavailableError();
    mkdirSync(this.#dir, { recursive: true });
    writeFileSync(file, safeStorage.encryptString(JSON.stringify(merged)), { mode: 0o600 });
    chmodSync(file, 0o600);
  }
}
