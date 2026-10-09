import { mkdtempSync, readFileSync, statSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";

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

const { asMcpServers, McpServerStore, summarizeMcpServers } = await import("./mcp-servers.js");

const list = [
  { name: "notes", command: "/usr/local/bin/notes-mcp", args: ["--stdio"], env: { NOTES_TOKEN: "env-ROUNDTRIP-1" } },
  { name: "wiki", url: "https://wiki.example/mcp?key=QUERY-SECRET", headers: { Authorization: "Bearer hdr-ROUNDTRIP-2" } },
  { name: "plain", command: "node" },
];

describe("asMcpServers", () => {
  it("accepts the executor contract's shape", () => {
    expect(asMcpServers(list)).toEqual(list);
    expect(asMcpServers([])).toEqual([]);
  });

  it("refuses what the runner would refuse", () => {
    for (const bad of [
      [{ name: "a_b", command: "x" }],
      [{ name: "-a", command: "x" }],
      [{ name: "a", command: "x" }, { name: "a", command: "y" }],
      [{ name: "a" }],
      [{ name: "a", command: "x", url: "https://a.example" }],
      [{ name: "a", url: "ftp://a.example" }],
      [{ name: "a", url: "https://u:p@a.example" }],
      [{ name: "a", url: "https://a.example", env: { A: "b" } }],
      [{ name: "a", command: "x", headers: { A: "b" } }],
      [{ name: "a", command: "x", env: { "1X": "v" } }],
      [{ name: "a", url: "https://a.example", headers: { A: "b\nX: 1" } }],
      "not a list",
    ]) {
      expect(asMcpServers(bad), JSON.stringify(bad)).toBeNull();
    }
  });
});

describe("McpServerStore", () => {
  it("round-trips the list through mcp-servers.bin, encrypted and owner-only", () => {
    const dir = mkdtempSync(path.join(os.tmpdir(), "tt-mcp-"));
    const store = new McpServerStore(dir);
    expect(store.read()).toEqual([]);
    store.store(list);
    expect(new McpServerStore(dir).read()).toEqual(list);

    const file = path.join(dir, "mcp-servers.bin");
    const raw = readFileSync(file);
    for (const secret of ["env-ROUNDTRIP-1", "hdr-ROUNDTRIP-2", "notes-mcp"]) expect(raw.includes(secret)).toBe(false);
    if (process.platform !== "win32") expect(statSync(file).mode & 0o777).toBe(0o600);

    store.forget();
    expect(store.read()).toEqual([]);
  });

  it("refuses to write when the key store cannot encrypt, rather than writing plaintext", () => {
    const dir = mkdtempSync(path.join(os.tmpdir(), "tt-mcp-"));
    keychain.available = false;
    try {
      expect(() => new McpServerStore(dir).store(list)).toThrow(/cannot be stored/);
    } finally {
      keychain.available = true;
    }
  });

  it("summarizes names and whether there is a secret, never a value or a query string", () => {
    const summaries = summarizeMcpServers(list);
    expect(summaries.map((s) => [s.name, s.transport, s.hasSecret, s.secretNames])).toEqual([
      ["notes", "stdio", true, ["NOTES_TOKEN"]],
      ["wiki", "http", true, ["Authorization"]],
      ["plain", "stdio", false, []],
    ]);
    expect(JSON.stringify(summaries)).not.toMatch(/ROUNDTRIP|QUERY-SECRET/);
  });
});
