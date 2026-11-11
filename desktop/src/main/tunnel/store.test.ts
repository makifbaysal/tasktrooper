import { mkdtempSync, readFileSync, readdirSync, statSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  app: { getPath: () => "/unused" },
  safeStorage: {
    isEncryptionAvailable: () => true,
    encryptString: (text: string) => Buffer.from(`enc:${Buffer.from(text).toString("base64")}`),
    decryptString: (buf: Buffer) => Buffer.from(buf.toString().replace(/^enc:/, ""), "base64").toString(),
  },
}));

const { RemoteAccessStore, asRemoteConfig, DEFAULT_REMOTE_PORT, secretsView } = await import("./store.js");

const TOKEN = "eyJhIjoiMDEyMzQ1Njc4OWFiY2RlZiIsInQiOiJ0b2tlbi10b2tlbi10b2tlbiJ9";

function dir(): string {
  return mkdtempSync(path.join(os.tmpdir(), "tt-remote-"));
}

describe("RemoteAccessStore", () => {
  it("keeps the plain settings readable and the secrets encrypted in a file of their own", () => {
    const d = dir();
    const store = new RemoteAccessStore(d);
    store.setConfig({ provider: "ngrok", mode: "named", hostname: "Phone.Example.com", autoStart: true });
    store.setSecrets({ cloudflareToken: TOKEN, ngrokAuthtoken: "2abcDEF1234ghi_5JkLmNoPqRsTuVwXyZ12" });

    const json = readFileSync(path.join(d, "remote-access.json"), "utf8");
    expect(json).not.toContain(TOKEN);
    expect(json).not.toContain("2abcDEF");
    expect(JSON.parse(json)).toMatchObject({ provider: "ngrok", mode: "named", hostname: "phone.example.com", autoStart: true });

    const bin = readFileSync(path.join(d, "remote-access.bin"));
    expect(bin.toString()).not.toContain(TOKEN);
    expect(statSync(path.join(d, "remote-access.bin")).mode & 0o777).toBe(0o600);
    expect(readdirSync(d).sort()).toEqual(["remote-access.bin", "remote-access.json"]);

    const reopened = new RemoteAccessStore(d);
    expect(reopened.config()).toMatchObject({ provider: "ngrok", hostname: "phone.example.com" });
    expect(reopened.secrets().cloudflareToken).toBe(TOKEN);
    expect(secretsView(reopened.secrets())).toEqual({ cloudflareToken: true, ngrokAuthtoken: true });
  });

  it("never writes a secret into settings.json", () => {
    const d = dir();
    new RemoteAccessStore(d).setSecrets({ cloudflareToken: TOKEN });
    expect(readdirSync(d)).not.toContain("settings.json");
  });

  it("forgets a cleared secret and removes the file when none is left", () => {
    const d = dir();
    const store = new RemoteAccessStore(d);
    store.setSecrets({ cloudflareToken: TOKEN });
    store.setSecrets({}, ["cloudflareToken"]);
    expect(store.secrets()).toEqual({});
    expect(readdirSync(d)).not.toContain("remote-access.bin");
  });

  it("remembers the last remote port and defaults to the stable one", () => {
    const d = dir();
    const store = new RemoteAccessStore(d);
    expect(store.lastPort()).toBe(DEFAULT_REMOTE_PORT);
    store.setLastPort(48001);
    expect(new RemoteAccessStore(d).lastPort()).toBe(48001);
  });

  it("treats a hand-edited config as untrusted", () => {
    expect(asRemoteConfig({ provider: "evil", mode: "x", hostname: "not a host; rm -rf", autoStart: "yes", serverName: "a\nb" })).toEqual({
      provider: "cloudflare",
      mode: "quick",
      hostname: "",
      autoStart: false,
      serverName: "ab",
    });
  });
});
