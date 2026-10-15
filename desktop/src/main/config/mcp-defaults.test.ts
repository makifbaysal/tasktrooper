import { existsSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({ safeStorage: {}, app: { getPath: () => "/userData" } }));

const { asMcpServers } = await import("./mcp-servers.js");
const { MCP_DEFAULTS, MCP_DEFAULTS_VERSION, SeededMcpServerStore, withMcpDefaults } = await import("./mcp-defaults.js");
type McpServerConfig = import("./mcp-servers.js").McpServerConfig;

function memoryStore(initial: McpServerConfig[] | null) {
  let list = initial;
  const writes: McpServerConfig[][] = [];
  return {
    writes,
    read: () => list ?? [],
    readKnown: () => list,
    store: (next: McpServerConfig[]) => {
      writes.push(next);
      list = next;
    },
  };
}

describe("the default MCP servers", () => {
  const servers = withMcpDefaults([], 0);

  it("are a list the runner accepts, with no secret and no placeholder in it", () => {
    expect(asMcpServers(servers)).toEqual(servers);
    expect(JSON.stringify(servers)).not.toContain("${");
    expect(servers.some((s) => "since" in s)).toBe(false);
  });

  it("start with only browser switched on, as the cloud had them", () => {
    expect(servers.filter((s) => !s.disabled).map((s) => s.name)).toEqual(["browser"]);
  });

  it("are the templates the backend offers, less the one whose argument is a password", () => {
    const source = ["../server", "../backend/agent-server"]
      .map((dir) => path.join(dir, "internal/domain/mcp_template.go"))
      .find((file) => existsSync(file));
    expect(source).toBeDefined();
    const ids = [...readFileSync(source as string, "utf8").matchAll(/ID: "([^"]+)"/g)].map((m) => m[1]);
    expect(MCP_DEFAULTS.map((d) => d.name)).toEqual(ids.filter((id) => id !== "postgres"));
  });
});

describe("withMcpDefaults", () => {
  it("adds only the defaults the list does not name, and never changes a stored server", () => {
    const mine: McpServerConfig = { name: "browser", command: "/bin/my-browser" };
    const next = withMcpDefaults([mine], 0);
    expect(next[0]).toBe(mine);
    expect(next.filter((s) => s.name === "browser")).toHaveLength(1);
    expect(next).toHaveLength(MCP_DEFAULTS.length);
  });

  it("adds nothing to a computer already seeded at this version", () => {
    expect(withMcpDefaults([], MCP_DEFAULTS_VERSION)).toEqual([]);
  });
});

describe("SeededMcpServerStore", () => {
  const dir = () => mkdtempSync(path.join(os.tmpdir(), "tt-mcp-defaults-"));

  it("adds the defaults on the first read, once, and remembers the version", () => {
    const where = dir();
    const inner = memoryStore([]);
    const store = new SeededMcpServerStore(inner, where);
    expect(store.read()).toHaveLength(MCP_DEFAULTS.length);
    store.read();
    expect(inner.writes).toHaveLength(1);
    expect(JSON.parse(readFileSync(path.join(where, "mcp-defaults.json"), "utf8"))).toEqual({ version: MCP_DEFAULTS_VERSION });
  });

  it("does not bring back a default the member removed", () => {
    const where = dir();
    writeFileSync(path.join(where, "mcp-defaults.json"), JSON.stringify({ version: MCP_DEFAULTS_VERSION }));
    const inner = memoryStore([{ name: "notes", command: "/bin/notes" }]);
    expect(new SeededMcpServerStore(inner, where).read()).toEqual([{ name: "notes", command: "/bin/notes" }]);
    expect(inner.writes).toHaveLength(0);
  });

  it("leaves a list it cannot read alone, and tries again next time", () => {
    const where = dir();
    const inner = memoryStore(null);
    expect(new SeededMcpServerStore(inner, where).read()).toEqual([]);
    expect(inner.writes).toHaveLength(0);
    expect(existsSync(path.join(where, "mcp-defaults.json"))).toBe(false);
  });

  it("logs and carries on when the key store refuses the write", () => {
    const where = dir();
    const lines: string[] = [];
    const inner = {
      ...memoryStore([]),
      store: () => {
        throw new Error("no key store");
      },
    };
    expect(new SeededMcpServerStore(inner, where, (line) => lines.push(line)).read()).toEqual([]);
    expect(lines[0]).toMatch(/could not add the default MCP servers: no key store/);
    expect(existsSync(path.join(where, "mcp-defaults.json"))).toBe(false);
  });
});
