import { existsSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({ safeStorage: {}, app: { getPath: () => "/userData" } }));

const { asMcpServers } = await import("./mcp-servers.js");
const { ARTCRAFT_APPS, MCP_DEFAULTS, MCP_DEFAULTS_VERSION, SeededMcpServerStore, artCraftCli, artCraftDefaults, reconcileArtCraft, withMcpDefaults } =
  await import(
  "./mcp-defaults.js"
);
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

  it("start with only browser switched on, as the cloud had them, besides the ArtCraft apps installed here", () => {
    const artCraft = new Set<string>(ARTCRAFT_APPS);
    expect(servers.filter((s) => !s.disabled && !artCraft.has(s.name)).map((s) => s.name)).toEqual(["browser"]);
  });

  it("switch an ArtCraft app on only when its CLI is installed, writing only the design files folder", () => {
    const home = "/home/me";
    const apps = artCraftDefaults(home, (file) => file.endsWith("figcraft-cli"));
    expect(apps.filter((a) => !a.disabled).map((a) => a.name)).toEqual(["figcraft"]);
    const figcraft = apps.find((a) => a.name === "figcraft");
    expect(figcraft?.command).toBe(path.join(home, "artCraft", "figcraft-cli", "figcraft-cli"));
    expect(figcraft?.args).toEqual([
      "mcp",
      "--automation-read-root",
      home,
      "--automation-write-root",
      path.join(home, "Documents", "TaskTrooper Designs"),
    ]);
  });

  it("are the templates the backend offers, less the one whose argument is a password", () => {
    const source = ["../server", "../backend/agent-server"]
      .map((dir) => path.join(dir, "internal/domain/mcp_template.go"))
      .find((file) => existsSync(file));
    expect(source).toBeDefined();
    const artCraft = (source as string).replace("mcp_template.go", "mcp_template_artcraft.go");
    const ids = [source as string, artCraft]
      .filter((file) => existsSync(file))
      .flatMap((file) => [...readFileSync(file, "utf8").matchAll(/ID: "([^"]+)"/g)].map((m) => m[1]));
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
    expect(JSON.parse(readFileSync(path.join(where, "mcp-defaults.json"), "utf8"))).toMatchObject({ version: MCP_DEFAULTS_VERSION });
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

describe("ArtCraft CLIs wherever they are", () => {
  const home = "/home/me";

  it("finds a CLI in an app bundle or this app's install folder, not only ~/artCraft", () => {
    const installed = path.join(home, ".local", "bin", "figcraft-cli");
    expect(artCraftCli("figcraft", home, (f) => f === installed)).toBe(installed);
    expect(artCraftCli("figcraft", home, () => false)).toBe(installed);
    if (process.platform === "darwin") {
      const bundle = "/Applications/PhotoCraft.app/Contents/MacOS/photocraft-cli";
      expect(artCraftCli("photocraft", home, (f) => f === bundle)).toBe(bundle);
    }
  });

  it("points a server at the CLI installed later and switches it on once", () => {
    const found = path.join(home, "artCraft", "figcraft-cli", "figcraft-cli");
    const before = [{ name: "figcraft", command: "/gone/figcraft-cli", args: ["mcp"], disabled: true }];
    const first = reconcileArtCraft(before, new Set(), home, (f) => f === found);
    expect(first.changed).toBe(true);
    expect(first.servers[0]).toEqual({ name: "figcraft", command: found, args: ["mcp"] });
    expect([...first.switchedOn]).toEqual(["figcraft"]);

    const offAgain = [{ ...first.servers[0]!, disabled: true }];
    const second = reconcileArtCraft(offAgain, first.switchedOn, home, (f) => f === found);
    expect(second.changed).toBe(false);
    expect(second.servers[0]?.disabled).toBe(true);
  });

  it("leaves a server alone while its CLI is nowhere", () => {
    const list = [{ name: "vectorcraft", command: "/x/vectorcraft-cli", disabled: true }];
    expect(reconcileArtCraft(list, new Set(), home, () => false)).toMatchObject({ changed: false, servers: list });
  });
});
