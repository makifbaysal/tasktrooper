import { mkdtempSync, readFileSync, statSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";

/**
 * A stand-in for the OS key store: reversible, and never the plaintext — so a
 * test can tell an encrypted file from one that is not.
 */
const keychain = vi.hoisted(() => ({
  available: true,
  encryptString: (plain: string): Buffer => Buffer.from(Buffer.from(plain, "utf8").map((b) => b ^ 0x5a)),
  decryptString: (cipher: Buffer): string => Buffer.from(cipher.map((b) => b ^ 0x5a)).toString("utf8"),
}));

vi.mock("electron", () => ({
  safeStorage: {
    isEncryptionAvailable: () => keychain.available,
    encryptString: keychain.encryptString,
    decryptString: keychain.decryptString,
  },
  app: { getPath: () => "/userData" },
}));

const { asProviders, ProviderStore, summarizeProviders, usableProviderIds } = await import("./providers.js");

/**
 * The member's API keys go from this file to the runner's stdin and on to the
 * executor's. The runner refuses a list that breaks its rules at startup,
 * which would be a runner that never attaches over a typo; so the same rules
 * are held here, before anything is written and again when it is read.
 */
describe("asProviders", () => {
  const openai = { id: "openai", type: "openai", api_key: "sk-1", models: ["gpt-4.1"] };

  it("accepts the executor contract's shape, keys and timeouts included", () => {
    const list = [
      openai,
      { id: "8f0e7c0a-1b2c-4d5e-9f00-0123456789ab", type: "openai_compatible", base_url: "https://openrouter.ai/api/v1", api_key: "k", timeout_seconds: 120 },
      { id: "local", type: "local", base_url: "http://127.0.0.1:1234/v1" },
    ];
    expect(asProviders(list)).toEqual(list);
    expect(asProviders([])).toEqual([]);
  });

  it("refuses a key that would travel over plain http to another machine", () => {
    expect(asProviders([{ ...openai, base_url: "http://llm.example.com/v1" }])).toBeNull();
    expect(asProviders([{ ...openai, base_url: "https://user:pw@llm.example.com" }])).toBeNull();
  });

  it("refuses what the runner would refuse: a flag for an id, a duplicate, a header in a key", () => {
    for (const list of [
      [{ ...openai, id: "-x" }],
      [openai, openai],
      [{ ...openai, api_key: "sk\nX-Evil: 1" }],
      [{ ...openai, models: [""] }],
      [{ ...openai, timeout_seconds: -1 }],
      [{ id: "openai" }],
      "not a list",
    ]) {
      expect(asProviders(list), JSON.stringify(list)).toBeNull();
    }
  });
});

/**
 * `providers.bin` is the one place the member's keys live at rest. What goes
 * in must come back out unchanged, encrypted on disk, owner-only — and a
 * store the key store will not encrypt for must refuse rather than write
 * plaintext.
 */
describe("ProviderStore", () => {
  const list = [
    { id: "anthropic", type: "anthropic", api_key: "sk-ant-ROUNDTRIP-1", models: ["claude-sonnet-4-5"] },
    { id: "8f0e7c0a-1b2c-4d5e-9f00-0123456789ab", type: "openai_compatible", base_url: "https://openrouter.ai/api/v1", api_key: "or-ROUNDTRIP-2" },
    { id: "local", type: "local", base_url: "http://127.0.0.1:1234/v1" },
  ];

  it("round-trips the list through providers.bin, encrypted and owner-only", () => {
    const dir = mkdtempSync(path.join(os.tmpdir(), "tt-providers-"));
    const store = new ProviderStore(dir);
    expect(store.read()).toEqual([]);
    store.store(list);
    expect(new ProviderStore(dir).read()).toEqual(list);

    const file = path.join(dir, "providers.bin");
    const raw = readFileSync(file);
    for (const secret of ["sk-ant-ROUNDTRIP-1", "or-ROUNDTRIP-2"]) expect(raw.includes(secret)).toBe(false);
    if (process.platform !== "win32") expect(statSync(file).mode & 0o777).toBe(0o600);

    store.forget();
    expect(store.read()).toEqual([]);
  });

  it("refuses to write when the key store cannot encrypt, rather than writing plaintext", () => {
    const dir = mkdtempSync(path.join(os.tmpdir(), "tt-providers-"));
    keychain.available = false;
    try {
      expect(() => new ProviderStore(dir).store(list)).toThrow(/cannot be stored/);
    } finally {
      keychain.available = true;
    }
  });

  it("summarizes without a key — only whether there is one", () => {
    const summaries = summarizeProviders(list);
    expect(summaries.map((p) => p.hasKey)).toEqual([true, true, false]);
    expect(JSON.stringify(summaries)).not.toMatch(/ROUNDTRIP|api_key/);
  });

  it("counts a provider with a key, or a local server, as one an agent can run on", () => {
    expect(usableProviderIds([...list, { id: "openai", type: "openai" }])).toEqual([
      "anthropic",
      "8f0e7c0a-1b2c-4d5e-9f00-0123456789ab",
      "local",
    ]);
  });
});
