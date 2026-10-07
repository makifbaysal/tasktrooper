import { randomBytes } from "node:crypto";
import { chmodSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { app, safeStorage } from "electron";
import { keyStoreHelp, undecryptableHelp } from "./keystore.js";

/**
 * The two secrets this machine holds, and where they live.
 *
 * Both are generated here on first run and then never change: the API key the
 * backend is started with (and the UI sends as a bearer), and the key the
 * backend encrypts stored provider credentials with. `mcp_secrets_key` in
 * particular MUST be stable across runs — a new one on every launch would make
 * every already-encrypted row unreadable.
 *
 * Not a `.env` file. The old shell-sourced one evaluated every value as shell,
 * so a secret containing a `$` or a backtick was expanded before anything read
 * it. Here the transport is the macOS Keychain: `safeStorage` encrypts with a
 * key the OS holds in the login keychain under this app's own entry, the
 * ciphertext lands in userData at 0600, and the plaintext goes from memory into
 * a child's environment with no shell in between.
 *
 * Why not `security add-generic-password`: it takes the secret as argv, which
 * is world-readable in `ps` for the lifetime of the call.
 */

const FILE = "local.bin";

/** 32 bytes each. Long enough that guessing is not a threat model. */
const TOKEN_BYTES = 32;

type StoredSecrets =
  | { kind: "ok"; secrets: LocalSecrets }
  | { kind: "missing" }
  | { kind: "unavailable" }
  /** The file is there and the key this session has does not open it. */
  | { kind: "undecryptable" }
  /** It opened, and what is inside is not a usable pair. */
  | { kind: "corrupt" };

export interface LocalSecrets {
  /** Bearer for the local backend: its `SERVER_API_KEY`, and the UI's token. */
  api_token: string;
  /** The backend's `MCP_SECRETS_KEY`. Stable for the life of the install. */
  mcp_secrets_key: string;
  created_at: string;
}

export class SecretStoreUnavailableError extends Error {
  constructor() {
    const { failure, remedy, fallback } = keyStoreHelp();
    super(`${failure}, so this app will not store the credentials its local server needs. ${remedy}. ${fallback}`);
    this.name = "SecretStoreUnavailableError";
  }
}

/** A `local.bin` that exists and will not decrypt in this Linux session. See `undecryptableHelp`. */
export class SecretStoreLockedError extends Error {
  constructor(file: string) {
    super(undecryptableHelp(file));
    this.name = "SecretStoreLockedError";
  }
}

/**
 * A Linux session with no keyring (a bare window manager, a container) has no OS
 * secret store. Electron can still "encrypt" there with a key built into
 * Electron itself — obscuring, not protecting — which is weaker than a keyring
 * and better than refusing to start. No-op elsewhere.
 */
function allowLinuxFallback(): void {
  if (process.platform !== "linux" || typeof safeStorage.getSelectedStorageBackend !== "function") return;
  if (safeStorage.getSelectedStorageBackend() === "basic_text") safeStorage.setUsePlainTextEncryption(true);
}

export class SecretStore {
  readonly #dir: string;

  constructor(dir = app.getPath("userData")) {
    this.#dir = dir;
  }

  #path(): string {
    return path.join(this.#dir, FILE);
  }

  available(): boolean {
    allowLinuxFallback();
    return safeStorage.isEncryptionAvailable();
  }

  /**
   * The secrets, generating them on the first call.
   *
   * On macOS and Windows a file that exists but does not decrypt is
   * regenerated rather than treated as an error: the key lives in the login
   * keychain or behind DPAPI for this user, so a failure means the keychain
   * entry was removed or the profile was restored onto a different machine —
   * permanent either way. That costs the provider credentials stored under the
   * old key, which is unavoidable — they cannot be decrypted either — and it is
   * better than an app that will not start.
   *
   * Linux is the exception, because there the failure is usually NOT
   * permanent: the backend is picked per session, and a keyring that is locked
   * or not running today is there again tomorrow. Regenerating would make a
   * transient failure permanent, so it refuses instead (`SecretStoreLockedError`).
   *
   * A file that decrypts and is not a usable pair is corrupt on every platform,
   * and is replaced.
   */
  ensure(): LocalSecrets {
    const stored = this.#load();
    if (stored.kind === "ok") return stored.secrets;
    if (stored.kind === "undecryptable" && process.platform === "linux") throw new SecretStoreLockedError(this.#path());
    allowLinuxFallback();
    if (!safeStorage.isEncryptionAvailable()) throw new SecretStoreUnavailableError();

    const next: LocalSecrets = {
      api_token: randomBytes(TOKEN_BYTES).toString("base64url"),
      // base64, not base64url: this one is read by the Go backend, whose
      // existing key handling is the standard encoding.
      mcp_secrets_key: randomBytes(TOKEN_BYTES).toString("base64"),
      created_at: new Date().toISOString(),
    };
    this.#write(next);
    return next;
  }

  read(): LocalSecrets | null {
    const stored = this.#load();
    return stored.kind === "ok" ? stored.secrets : null;
  }

  #load(): StoredSecrets {
    let ciphertext: Buffer;
    try {
      ciphertext = readFileSync(this.#path());
    } catch {
      return { kind: "missing" };
    }
    allowLinuxFallback();
    if (!safeStorage.isEncryptionAvailable()) return { kind: "unavailable" };
    let plaintext: string;
    try {
      plaintext = safeStorage.decryptString(ciphertext);
    } catch {
      return { kind: "undecryptable" };
    }
    try {
      const secrets = asSecrets(JSON.parse(plaintext));
      return secrets ? { kind: "ok", secrets } : { kind: "corrupt" };
    } catch {
      return { kind: "corrupt" };
    }
  }

  #write(secrets: LocalSecrets): void {
    mkdirSync(this.#dir, { recursive: true });
    const file = this.#path();
    writeFileSync(file, safeStorage.encryptString(JSON.stringify(secrets)), { mode: 0o600 });
    // writeFileSync's mode is only applied on create; an existing file keeps
    // whatever it had, so rewriting over a file someone chmod'd wide open would
    // silently stay wide open.
    chmodSync(file, 0o600);
  }

  forget(): void {
    try {
      rmSync(this.#path(), { force: true });
    } catch {
      // Nothing to do: the next read returns null either way.
    }
  }
}

/**
 * Both strings present and non-empty, or this is not a usable file. A partial
 * one would start the backend with an empty key and fail at the first
 * encrypted read, several layers from here.
 */
export function asSecrets(raw: unknown): LocalSecrets | null {
  if (typeof raw !== "object" || raw === null) return null;
  const o = raw as Record<string, unknown>;
  const token = typeof o.api_token === "string" ? o.api_token : "";
  const key = typeof o.mcp_secrets_key === "string" ? o.mcp_secrets_key : "";
  if (token === "" || key === "") return null;
  return {
    api_token: token,
    mcp_secrets_key: key,
    created_at: typeof o.created_at === "string" ? o.created_at : "",
  };
}
