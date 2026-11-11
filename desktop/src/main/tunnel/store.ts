import { randomBytes } from "node:crypto";
import { chmodSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { app, safeStorage } from "electron";
import { REMOTE_MODES, REMOTE_PROVIDERS, type RemoteAccessConfig, type RemoteSecretsView } from "../../ipc/types.js";
import { keyStoreHelp } from "../config/keystore.js";
import { normalizeHostname } from "../../ipc/validate.js";
import type { CloudflareTunnelRecord } from "./cloudflare.js";

/**
 * Remote access settings on disk.
 *
 * The non-secret half is plain JSON beside settings.json (`remote-access.json`):
 * a person may read or paste it. The two secrets (the ngrok authtoken and the optional
 * ngrok API key) are bearer credentials for the person's own tunnel account,
 * so they live in `remote-access.bin`, `safeStorage`-encrypted at 0600 like
 * `pairing.bin` — never in the JSON, never on argv, never back across the bridge.
 * (Cloudflare needs none of its own: its login is the certificate cloudflared keeps.)
 */

const CONFIG_FILE = "remote-access.json";
const SECRETS_FILE = "remote-access.bin";

/**
 * A port the server's remote listener asks for first. Stable on purpose, so the
 * tunnel keeps pointing at the same place between launches.
 */
export const DEFAULT_REMOTE_PORT = 47820;

export interface RemoteSecrets {
  ngrokAuthtoken?: string;
  /** Optional: only lets the app list the account's ngrok domains. */
  ngrokApiKey?: string;
}

/** What the app learned about the ngrok account when the authtoken was proven. */
export interface NgrokAccountRecord {
  verified: boolean;
  domains: string[];
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
  /** A short random id of this installation; names our Cloudflare tunnel and the suggested subdomain. */
  seed(): string;
  cloudflareTunnel(): CloudflareTunnelRecord | undefined;
  setCloudflareTunnel(record: CloudflareTunnelRecord | undefined): void;
  ngrokAccount(): NgrokAccountRecord;
  setNgrokAccount(record: NgrokAccountRecord | undefined): void;
}

export function secretsView(secrets: RemoteSecrets): RemoteSecretsView {
  return { ngrokAuthtoken: !!secrets.ngrokAuthtoken, ngrokApiKey: !!secrets.ngrokApiKey };
}

function allowLinuxFallback(): void {
  if (process.platform !== "linux" || typeof safeStorage.getSelectedStorageBackend !== "function") return;
  if (safeStorage.getSelectedStorageBackend() === "basic_text") safeStorage.setUsePlainTextEncryption(true);
}

export class RemoteSecretsUnavailableError extends Error {
  constructor() {
    const { failure, remedy } = keyStoreHelp();
    super(`${failure}, so the ngrok credentials cannot be stored. ${remedy}.`);
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

interface Persisted {
  config: RemoteAccessConfig;
  lastPort: number;
  seed: string;
  cloudflareTunnel?: CloudflareTunnelRecord;
  ngrok: NgrokAccountRecord;
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/** A hand-edited record is untrusted too: anything that is not exactly the shape is dropped. */
function asTunnelRecord(raw: unknown): CloudflareTunnelRecord | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const o = raw as Record<string, unknown>;
  if (typeof o.tunnelId !== "string" || !UUID.test(o.tunnelId)) return undefined;
  if (typeof o.tunnelName !== "string" || !/^[a-z0-9-]{1,80}$/.test(o.tunnelName)) return undefined;
  if (typeof o.credentialsFile !== "string" || o.credentialsFile === "" || o.credentialsFile.length > 1024) return undefined;
  if (typeof o.zoneId !== "string" || o.zoneId === "" || o.zoneId.length > 128) return undefined;
  const routed = typeof o.routedHostname === "string" ? normalizeHostname(o.routedHostname) : null;
  return {
    tunnelId: o.tunnelId,
    tunnelName: o.tunnelName,
    credentialsFile: o.credentialsFile,
    zoneId: o.zoneId,
    ...(routed ? { routedHostname: routed } : {}),
  };
}

function asNgrokRecord(raw: unknown): NgrokAccountRecord {
  if (typeof raw !== "object" || raw === null) return { verified: false, domains: [] };
  const o = raw as Record<string, unknown>;
  const domains = Array.isArray(o.domains)
    ? o.domains.filter((d): d is string => typeof d === "string").map((d) => normalizeHostname(d)).filter((d): d is string => d !== null).slice(0, 200)
    : [];
  return { verified: o.verified === true, domains };
}

export class RemoteAccessStore implements RemoteAccessStoreLike {
  readonly #dir: string;
  #cache: Persisted | null = null;
  #seedSaved = false;

  constructor(dir = app.getPath("userData")) {
    this.#dir = dir;
  }

  #state(): Persisted {
    if (this.#cache) return this.#cache;
    let raw: unknown = {};
    try {
      raw = JSON.parse(readFileSync(path.join(this.#dir, CONFIG_FILE), "utf8"));
    } catch {
      raw = {};
    }
    const o = (typeof raw === "object" && raw !== null ? raw : {}) as Record<string, unknown>;
    const port = o.lastPort;
    const tunnel = asTunnelRecord(o.cloudflareTunnel);
    this.#seedSaved = typeof o.seed === "string" && /^[a-f0-9]{6}$/.test(o.seed);
    this.#cache = {
      config: asRemoteConfig(raw),
      lastPort: typeof port === "number" && Number.isInteger(port) && port > 1024 && port < 65536 ? port : DEFAULT_REMOTE_PORT,
      seed: typeof o.seed === "string" && /^[a-f0-9]{6}$/.test(o.seed) ? o.seed : randomBytes(3).toString("hex"),
      ...(tunnel ? { cloudflareTunnel: tunnel } : {}),
      ngrok: asNgrokRecord(o.ngrok),
    };
    return this.#cache;
  }

  #write(next: Persisted): void {
    mkdirSync(this.#dir, { recursive: true });
    const { config, lastPort, seed, cloudflareTunnel, ngrok } = next;
    const body = { ...config, lastPort, seed, ...(cloudflareTunnel ? { cloudflareTunnel } : {}), ngrok };
    writeFileSync(path.join(this.#dir, CONFIG_FILE), `${JSON.stringify(body, null, 2)}\n`, "utf8");
    this.#seedSaved = true;
    this.#cache = next;
  }

  seed(): string {
    const state = this.#state();
    // The first read made the seed up; keep it so the tunnel name survives a restart.
    if (!this.#seedSaved) this.#write(state);
    return state.seed;
  }

  cloudflareTunnel(): CloudflareTunnelRecord | undefined {
    const record = this.#state().cloudflareTunnel;
    return record ? { ...record } : undefined;
  }

  setCloudflareTunnel(record: CloudflareTunnelRecord | undefined): void {
    const next: Persisted = { ...this.#state() };
    if (record) next.cloudflareTunnel = record;
    else delete next.cloudflareTunnel;
    this.#write(next);
  }

  ngrokAccount(): NgrokAccountRecord {
    const { verified, domains } = this.#state().ngrok;
    return { verified, domains: [...domains] };
  }

  setNgrokAccount(record: NgrokAccountRecord | undefined): void {
    this.#write({ ...this.#state(), ngrok: asNgrokRecord(record) });
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
      if (typeof raw.ngrokAuthtoken === "string" && raw.ngrokAuthtoken !== "") out.ngrokAuthtoken = raw.ngrokAuthtoken;
      if (typeof raw.ngrokApiKey === "string" && raw.ngrokApiKey !== "") out.ngrokApiKey = raw.ngrokApiKey;
      return out;
    } catch {
      return {};
    }
  }

  setSecrets(set: RemoteSecrets, clear: readonly (keyof RemoteSecrets)[] = []): void {
    const merged: RemoteSecrets = { ...this.secrets(), ...set };
    for (const name of clear) delete merged[name];
    const file = path.join(this.#dir, SECRETS_FILE);
    if (!merged.ngrokAuthtoken && !merged.ngrokApiKey) {
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
