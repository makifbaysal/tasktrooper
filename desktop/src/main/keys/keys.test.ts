import { afterEach, describe, expect, it, vi } from "vitest";
import type { McpServerConfig } from "../config/mcp-servers.js";
import type { ProviderConfig } from "../config/providers.js";

vi.mock("electron", () => ({ safeStorage: {}, app: { getPath: () => "/userData" } }));

const { applyKeyRemove, applyKeySet, KeyService } = await import("./keys.js");

const UUID = "8f0e7c0a-1b2c-4d5e-9f00-0123456789ab";
const newId = (): string => UUID;

describe("applyKeySet: the id convention", () => {
  it("gives a built-in provider its type as its id", () => {
    const { list, id } = applyKeySet([], { type: "anthropic", api_key: "k" }, newId);
    expect(id).toBe("anthropic");
    expect(list).toEqual([{ id: "anthropic", type: "anthropic", api_key: "k" }]);
    expect(() => applyKeySet([], { id: "claude", type: "anthropic", api_key: "k" }, newId)).toThrow(/its type/);
  });

  it("gives a new custom endpoint a UUID, and refuses any other id for one", () => {
    const { id } = applyKeySet([], { type: "openai_compatible", base_url: "https://openrouter.ai/api/v1", api_key: "k" }, newId);
    expect(id).toBe(UUID);
    expect(() =>
      applyKeySet([], { id: "openrouter", type: "openai_compatible", base_url: "https://x.example", api_key: "k" }, newId),
    ).toThrow(/UUID/);
  });

  it("needs an address for a custom endpoint, and one the key may travel to", () => {
    expect(() => applyKeySet([], { type: "openai_compatible", api_key: "k" }, newId)).toThrow(/address/);
    expect(() =>
      applyKeySet([], { type: "openai_compatible", base_url: "http://llm.example.com/v1", api_key: "k" }, newId),
    ).toThrow(/https/);
  });

  it("needs a key for a new hosted provider, but not for a server on this computer", () => {
    expect(() => applyKeySet([], { type: "openai" }, newId)).toThrow(/Enter the API key/);
    expect(applyKeySet([], { type: "local" }, newId).list).toEqual([{ id: "local", type: "local" }]);
    expect(applyKeySet([], { type: "openai_compatible", base_url: "http://127.0.0.1:8080/v1" }, newId).id).toBe(UUID);
  });

  it("keeps the stored key, address and models when a change leaves them out", () => {
    const stored: ProviderConfig[] = [{ id: "openai", type: "openai", api_key: "sk-old", models: ["gpt-4.1"], timeout_seconds: 90 }];
    expect(applyKeySet(stored, { type: "openai", models: ["gpt-5"] }, newId).list).toEqual([
      { id: "openai", type: "openai", api_key: "sk-old", models: ["gpt-5"], timeout_seconds: 90 },
    ]);
    expect(applyKeySet(stored, { type: "openai", api_key: "sk-new" }, newId).list[0]).toMatchObject({
      api_key: "sk-new",
      models: ["gpt-4.1"],
    });
  });

  it("replaces in place and refuses to turn one provider into another type", () => {
    const stored: ProviderConfig[] = [
      { id: "anthropic", type: "anthropic", api_key: "a" },
      { id: UUID, type: "openai_compatible", base_url: "https://x.example/v1", api_key: "b" },
    ];
    const { list } = applyKeySet(stored, { id: UUID, type: "openai_compatible", api_key: "c" }, newId);
    expect(list.map((p) => p.id)).toEqual(["anthropic", UUID]);
    expect(list[1]).toMatchObject({ base_url: "https://x.example/v1", api_key: "c" });
  });

  it("removes by id, and says so when there is none", () => {
    const stored: ProviderConfig[] = [{ id: "groq", type: "groq", api_key: "g" }];
    expect(applyKeyRemove(stored, "groq")).toEqual([]);
    expect(() => applyKeyRemove(stored, "openai")).toThrow(/no provider openai/);
  });
});

/**
 * The window's three answers, and what this process says about a change,
 * never carry a key: the reply is `hasKey`, the log names an id, and a
 * refusal is a sentence that quotes neither.
 */
describe("KeyService", () => {
  const SECRET = "sk-ant-NEVER-ECHOED-0123456789";

  afterEach(() => vi.restoreAllMocks());

  function service(stored: ProviderConfig[] = [], changed = false) {
    let list = stored;
    const lines: string[] = [];
    const svc = new KeyService({
      store: { read: () => list, store: (next) => (list = next) },
      mcpStore: { read: () => [], store: () => undefined },
      changed: () => changed,
      log: (line) => lines.push(line),
      newId,
    });
    return { svc, lines, stored: () => list };
  }

  it("stores the key and answers with hasKey only, in the reply and in the log", () => {
    const consoleCalls: unknown[] = [];
    for (const method of ["log", "info", "warn", "error", "debug"] as const) {
      vi.spyOn(console, method).mockImplementation((...args: unknown[]) => consoleCalls.push(args));
    }
    const { svc, lines, stored } = service([], true);

    const reply = svc.set({ type: "anthropic", api_key: SECRET, models: ["claude-sonnet-4-5"] });
    expect(stored()[0]?.api_key).toBe(SECRET);
    expect(reply).toEqual({
      providers: [{ id: "anthropic", type: "anthropic", models: ["claude-sonnet-4-5"], hasKey: true }],
      runnerRestarting: true,
    });

    const listed = svc.list();
    const removed = svc.remove("anthropic");
    for (const out of [reply, listed, removed, lines, consoleCalls]) {
      expect(JSON.stringify(out)).not.toContain(SECRET);
    }
    expect(lines).toEqual([
      "[keys] stored the anthropic provider anthropic; restarting the runner",
      "[keys] removed the provider anthropic; restarting the runner",
    ]);
  });

  it("refuses without quoting the key, and stores nothing", () => {
    const { svc, stored } = service([{ id: "anthropic", type: "anthropic", api_key: "a" }]);
    let refusal = "";
    try {
      svc.set({ type: "openai_compatible", base_url: "http://llm.example.com", api_key: SECRET });
    } catch (err) {
      refusal = err instanceof Error ? err.message : String(err);
    }
    expect(refusal).toMatch(/https/);
    expect(refusal).not.toContain(SECRET);
    expect(stored()).toEqual([{ id: "anthropic", type: "anthropic", api_key: "a" }]);
  });

  it("says whether the change restarted the runner", () => {
    expect(service([], false).svc.set({ type: "groq", api_key: "g" }).runnerRestarting).toBe(false);
  });
});

describe("MCP servers", () => {
  const TOKEN = "env-NEVER-ECHOED-0123456789";
  const HEADER = "Bearer hdr-NEVER-ECHOED-9876543210";

  function service(stored: McpServerConfig[] = [], changed = false) {
    let list = stored;
    const lines: string[] = [];
    const svc = new KeyService({
      store: { read: () => [], store: () => undefined },
      mcpStore: { read: () => list, store: (next) => (list = next) },
      changed: () => changed,
      log: (line) => lines.push(line),
      newId,
    });
    return { svc, lines, stored: () => list };
  }

  it("stores a stdio server and an http one, and answers with names and hasSecret only", () => {
    const { svc, lines, stored } = service([], true);
    svc.setMcp({ name: "notes", command: "/bin/notes", args: ["--stdio"], env: { NOTES_TOKEN: TOKEN } });
    const reply = svc.setMcp({ name: "wiki", url: "https://wiki.example/mcp", headers: { Authorization: HEADER } });

    expect(stored()).toEqual([
      { name: "notes", command: "/bin/notes", args: ["--stdio"], env: { NOTES_TOKEN: TOKEN } },
      { name: "wiki", url: "https://wiki.example/mcp", headers: { Authorization: HEADER } },
    ]);
    expect(reply.runnerRestarting).toBe(true);
    expect(reply.servers.map((s) => [s.name, s.hasSecret])).toEqual([
      ["notes", true],
      ["wiki", true],
    ]);
    for (const out of [reply, svc.listMcp(), svc.removeMcp("notes"), lines]) {
      expect(JSON.stringify(out)).not.toMatch(/NEVER-ECHOED/);
    }
    expect(lines[0]).toBe("[keys] stored the MCP server notes; restarting the runner");
  });

  it("keeps a stored value that the change leaves empty, and drops a name it leaves out", () => {
    const stored: McpServerConfig[] = [{ name: "notes", command: "/bin/notes", env: { A: "keep-a", B: "drop-b", C: "old-c" } }];
    const { svc, stored: after } = service(stored);
    svc.setMcp({ name: "notes", command: "/bin/notes2", env: { A: "", C: "new-c", D: "" } });
    expect(after()).toEqual([{ name: "notes", command: "/bin/notes2", env: { A: "keep-a", C: "new-c" } }]);
  });

  it("stores nothing when the runner would refuse the list, and says so without the secret", () => {
    const { svc, stored } = service();
    let refusal = "";
    try {
      svc.setMcp({ name: "a", url: "ftp://a.example", headers: { Authorization: HEADER } });
    } catch (err) {
      refusal = err instanceof Error ? err.message : String(err);
    }
    expect(refusal).toMatch(/not an MCP server the runner can use/);
    expect(refusal).not.toContain("NEVER-ECHOED");
    expect(stored()).toEqual([]);
  });

  it("says there is no such server on a remove", () => {
    expect(() => service().svc.removeMcp("ghost")).toThrow(/no MCP server ghost/);
  });
});
