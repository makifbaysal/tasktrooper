import { chmodSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { app, safeStorage } from "electron";
import type { RunnerPairingBundle, RunnerPairingSummary } from "../../ipc/types.js";
import { keyStoreHelp } from "./keystore.js";

/**
 * Where this computer's pairing lives, account mode only.
 *
 * The bundle carries `runner_token`, a bearer credential for this computer's own
 * tunnel session — nothing else on this machine may use it, and nothing else
 * needs to. Same transport as `config/secrets.ts`'s `local.bin`: `safeStorage`
 * encrypts with a key the OS holds in the login keychain, the ciphertext lands
 * in userData at 0600, and the plaintext only ever crosses a pipe (the
 * runner's stdin) with no shell in between. Never `security add-generic-password`
 * — that takes the secret as argv, which is world-readable in `ps`.
 */

const FILE = "pairing.bin";

/**
 * A Linux session with no keyring has no OS secret store; Electron's fallback
 * key is weaker than one and better than refusing to start. No-op elsewhere —
 * see `config/secrets.ts`'s identical helper, which this mirrors rather than
 * imports: two one-line functions are cheaper to keep in step than a shared
 * module neither file otherwise needs.
 */
function allowLinuxFallback(): void {
  if (process.platform !== "linux" || typeof safeStorage.getSelectedStorageBackend !== "function") return;
  if (safeStorage.getSelectedStorageBackend() === "basic_text") safeStorage.setUsePlainTextEncryption(true);
}

export class PairingStoreUnavailableError extends Error {
  constructor() {
    const { failure, remedy } = keyStoreHelp();
    super(`${failure}, so this computer's pairing cannot be stored. ${remedy}.`);
    this.name = "PairingStoreUnavailableError";
  }
}

/**
 * Every field required, non-empty, and free of the characters that would make
 * it a different value crossing a pipe or a log line — the same discipline
 * `ipc/validate.ts` holds IPC payloads to, applied here because a bundle can
 * also arrive from a hand-edited `pairing.bin` restored onto a new machine.
 */
// eslint-disable-next-line no-control-regex -- matching control characters is the entire point.
const CONTROL_CHARS = /[\u0000-\u001f\u007f]/;

function cleanNonEmpty(value: unknown): string | null {
  if (typeof value !== "string") return null;
  if (value === "" || CONTROL_CHARS.test(value)) return null;
  return value;
}

export function asPairingBundle(raw: unknown): RunnerPairingBundle | null {
  if (typeof raw !== "object" || raw === null) return null;
  const o = raw as Record<string, unknown>;
  const runner_token = cleanNonEmpty(o.runner_token);
  const tenant_id = cleanNonEmpty(o.tenant_id);
  const member_uid = cleanNonEmpty(o.member_uid);
  const paired_at = cleanNonEmpty(o.paired_at);
  const label = cleanNonEmpty(o.label);
  if (!runner_token || !tenant_id || !member_uid || !paired_at || !label) return null;

  const tm_base_url = cleanNonEmpty(o.tm_base_url);
  if (!tm_base_url) return null;
  let parsed: URL;
  try {
    parsed = new URL(tm_base_url);
  } catch {
    return null;
  }
  // https only, except loopback: a control plane run on this same machine for
  // a local trial never puts the runner_token on a network wire either way,
  // so plain http there is not the exposure the https requirement guards
  // against everywhere else.
  const isLoopbackHttp = parsed.protocol === "http:" && isLoopbackHost(parsed.hostname);
  if (parsed.protocol !== "https:" && !isLoopbackHttp) return null;

  return { runner_token, tm_base_url, tenant_id, member_uid, paired_at, label };
}

// Node's URL, unlike Go's, keeps the brackets in `hostname` for an IPv6
// literal ("[::1]", not "::1") — strip them so this compares the same three
// names isLoopbackHost in desktop/runner/mcp.go compares.
function isLoopbackHost(host: string): boolean {
  const bare = host.startsWith("[") && host.endsWith("]") ? host.slice(1, -1) : host;
  return bare === "127.0.0.1" || bare === "localhost" || bare === "::1";
}

export function pairingSummary(bundle: RunnerPairingBundle): RunnerPairingSummary {
  const { runner_token: _runner_token, ...summary } = bundle;
  return summary;
}

export class PairingStore {
  readonly #dir: string;

  constructor(dir = app.getPath("userData")) {
    this.#dir = dir;
  }

  #path(): string {
    return path.join(this.#dir, FILE);
  }

  /**
   * The stored bundle, or `null` when this computer is not paired — including a
   * file that exists but will not decrypt (a keychain entry that moved, or a
   * copy of userData restored onto a different machine). That is not a crash:
   * it is the same state as never having paired, and the UI's pairing card
   * already has the flow for it.
   */
  read(): RunnerPairingBundle | null {
    let ciphertext: Buffer;
    try {
      ciphertext = readFileSync(this.#path());
    } catch {
      return null;
    }
    allowLinuxFallback();
    if (!safeStorage.isEncryptionAvailable()) return null;
    try {
      return asPairingBundle(JSON.parse(safeStorage.decryptString(ciphertext)));
    } catch {
      return null;
    }
  }

  store(bundle: RunnerPairingBundle): void {
    const clean = asPairingBundle(bundle);
    if (!clean) throw new Error("pairing bundle failed validation and was not stored");
    allowLinuxFallback();
    if (!safeStorage.isEncryptionAvailable()) throw new PairingStoreUnavailableError();

    mkdirSync(this.#dir, { recursive: true });
    const file = this.#path();
    writeFileSync(file, safeStorage.encryptString(JSON.stringify(clean)), { mode: 0o600 });
    // writeFileSync's mode only applies on create; rewriting an existing file
    // someone chmod'd wide open would otherwise silently stay wide open.
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
