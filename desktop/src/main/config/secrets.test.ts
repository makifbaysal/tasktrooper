import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * A stand-in for `safeStorage` whose key can change between calls, which is
 * exactly what a Linux session does when the keyring that encrypted a file is
 * locked or not running the next time.
 */
const keyring = vi.hoisted(() => ({ key: "keyring-1", available: true }));

vi.mock("electron", () => ({
  app: { getPath: () => "/nonexistent" },
  safeStorage: {
    isEncryptionAvailable: () => keyring.available,
    getSelectedStorageBackend: () => "gnome_libsecret",
    setUsePlainTextEncryption: () => undefined,
    encryptString: (plain: string) => Buffer.from(`${keyring.key}|${plain}`),
    decryptString: (cipher: Buffer) => {
      const text = cipher.toString();
      if (!text.startsWith(`${keyring.key}|`)) throw new Error("Error while decrypting the ciphertext provided to safeStorage.decryptString.");
      return text.slice(keyring.key.length + 1);
    },
  },
}));

const { SecretStore, SecretStoreLockedError, SecretStoreUnavailableError, asSecrets } = await import("./secrets.js");

/**
 * The parser is the whole of the file's contract with its own past: a
 * `local.bin` written by an older build, or a half-written one, must read as
 * "no secrets yet" so the next `ensure()` generates a working pair — never as a
 * pair with an empty key, which starts the backend and fails at the first
 * encrypted row.
 */
describe("asSecrets", () => {
  it("accepts a complete pair", () => {
    expect(asSecrets({ api_token: "t", mcp_secrets_key: "k", created_at: "2026-09-14T00:00:00Z" })).toEqual({
      api_token: "t",
      mcp_secrets_key: "k",
      created_at: "2026-09-14T00:00:00Z",
    });
  });

  it("tolerates a missing created_at, which is metadata and not a secret", () => {
    expect(asSecrets({ api_token: "t", mcp_secrets_key: "k" })?.created_at).toBe("");
  });

  it("refuses anything that would start the backend with an empty key", () => {
    for (const raw of [
      null,
      undefined,
      "",
      42,
      {},
      { api_token: "t" },
      { mcp_secrets_key: "k" },
      { api_token: "", mcp_secrets_key: "k" },
      { api_token: "t", mcp_secrets_key: "" },
      { api_token: 1, mcp_secrets_key: "k" },
      // The pairing bundle this file used to hold. Nothing in it is usable now.
      { tenant_id: "t", runner_token: "r", tm_base_url: "https://example.invalid" },
    ]) {
      expect(asSecrets(raw), JSON.stringify(raw) ?? "undefined").toBeNull();
    }
  });
});

describe("SecretStore.ensure", () => {
  const realPlatform = Object.getOwnPropertyDescriptor(process, "platform");
  const onPlatform = (platform: NodeJS.Platform): void => {
    Object.defineProperty(process, "platform", { value: platform, configurable: true });
  };
  let dir: string;

  beforeEach(() => {
    dir = mkdtempSync(path.join(tmpdir(), "tt-secrets-"));
    keyring.key = "keyring-1";
    keyring.available = true;
  });
  afterEach(() => {
    if (realPlatform) Object.defineProperty(process, "platform", realPlatform);
    rmSync(dir, { recursive: true, force: true });
  });

  it("generates once and then returns the same pair", () => {
    const store = new SecretStore(dir);
    const first = store.ensure();
    expect(first.mcp_secrets_key).not.toBe("");
    expect(new SecretStore(dir).ensure()).toEqual(first);
  });

  /**
   * The invariant: `mcp_secrets_key` must never be regenerated on an install
   * that has stored provider credentials. On Linux a keyring that is locked
   * today opens tomorrow, so a file that will not decrypt is a refusal — and
   * the file is left exactly as it was.
   */
  it("refuses, and leaves local.bin untouched, when Linux cannot decrypt it", () => {
    onPlatform("linux");
    const original = new SecretStore(dir).ensure();
    const file = path.join(dir, "local.bin");
    const bytes = readFileSync(file);

    keyring.key = "basic_text-this-session";
    expect(() => new SecretStore(dir).ensure()).toThrow(SecretStoreLockedError);
    expect(() => new SecretStore(dir).ensure()).toThrow(/Unlock or start it/);
    expect(readFileSync(file).equals(bytes)).toBe(true);

    // The keyring is back: the same pair, not a new one.
    keyring.key = "keyring-1";
    expect(new SecretStore(dir).ensure()).toEqual(original);
  });

  it("keeps regenerating on macOS and Windows, where an undecryptable file is a lost key for good", () => {
    for (const platform of ["darwin", "win32"] as const) {
      onPlatform(platform);
      keyring.key = "old";
      const original = new SecretStore(dir).ensure();
      keyring.key = "new";
      const next = new SecretStore(dir).ensure();
      expect(next.mcp_secrets_key).not.toBe(original.mcp_secrets_key);
    }
  });

  it("replaces a file that decrypts to something unusable, on Linux too", () => {
    onPlatform("linux");
    writeFileSync(path.join(dir, "local.bin"), Buffer.from(`${keyring.key}|not json`));
    const store = new SecretStore(dir);
    expect(store.ensure().api_token).not.toBe("");
  });

  it("refuses rather than writes when there is no key store at all", () => {
    onPlatform("darwin");
    keyring.available = false;
    expect(() => new SecretStore(dir).ensure()).toThrow(SecretStoreUnavailableError);
  });
});
