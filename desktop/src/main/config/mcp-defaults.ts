import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import type { McpServerConfig } from "./mcp-servers.js";

/**
 * The MCP servers every computer starts with in account mode, added to its
 * own list (`mcp-servers.bin`) by this app — no cloud keeps them. The same
 * servers the local edition seeds (`server/internal/domain/mcp_template.go`),
 * without a secret or a placeholder: a token is typed in the key window, an
 * address or a path where the template leaves it empty is the member's to
 * add. Only `browser` starts switched on, as it did in the cloud.
 *
 * `since` is the version a default first shipped in. A computer seeded at
 * version N gets only the defaults newer than N, so one the member removed
 * stays removed.
 */
export const MCP_DEFAULTS_VERSION = 2;

interface McpDefault extends McpServerConfig {
  since: number;
}

const off = { disabled: true, since: 1 } as const;

/**
 * The ArtCraft apps' CLIs (getartcraft.com), where the ArtCraft Launcher puts
 * them; each serves MCP with `<app>-cli mcp`. Kept in step with
 * `server/internal/domain/mcp_template_artcraft.go`: switched on when the CLI
 * is installed, allowed to read the home folder and to write only the design
 * files folder.
 */
export const ARTCRAFT_APPS = [
  "figcraft",
  "vectorcraft",
  "photocraft",
  "anicraft",
  "effectcraft",
  "filmcraft",
  "lightcraft",
  "solidcraft",
  "soundcraft",
] as const;

export function designFilesDir(home = os.homedir()): string {
  return process.env.TASKTROOPER_DESIGNS_DIR || path.join(home, "Documents", "TaskTrooper Designs");
}

const ARTCRAFT_BUNDLES: Record<(typeof ARTCRAFT_APPS)[number], string> = {
  figcraft: "FigCraft",
  vectorcraft: "VectorCraft",
  photocraft: "PhotoCraft",
  anicraft: "AniCraft",
  effectcraft: "EffectCraft",
  filmcraft: "FilmCraft",
  lightcraft: "LightCraft",
  solidcraft: "SolidCraft",
  soundcraft: "SoundCraft",
};

function isArtCraftApp(name: string): name is (typeof ARTCRAFT_APPS)[number] {
  return (ARTCRAFT_APPS as readonly string[]).includes(name);
}

/** Where the toolchain installer puts one (`toolchain/locations.ts#userBinDir`). */
function artCraftInstallPath(app: string, home: string): string {
  const cli = `${app}-cli`;
  if (process.platform === "win32") {
    const local = process.env.LOCALAPPDATA || path.join(home, "AppData", "Local");
    return path.join(local, "Programs", cli, `${cli}.exe`);
  }
  return path.join(home, ".local", "bin", cli);
}

/**
 * The first copy of an ArtCraft CLI on this computer, in the order the backend
 * looks (`mcp_template_artcraft.go#artCraftCandidates`), else where the
 * toolchain installer would put it.
 */
export function artCraftCli(app: (typeof ARTCRAFT_APPS)[number], home: string, installed: (file: string) => boolean = existsSync): string {
  const cli = `${app}-cli`;
  const exe = process.platform === "win32" ? `${cli}.exe` : cli;
  const candidates: string[] = [];
  if (process.env.TASKTROOPER_ARTCRAFT_DIR) candidates.push(path.join(process.env.TASKTROOPER_ARTCRAFT_DIR, cli, exe));
  candidates.push(path.join(home, "artCraft", cli, exe), artCraftInstallPath(app, home));
  if (process.platform === "darwin") {
    const bundle = `${ARTCRAFT_BUNDLES[app]}.app`;
    candidates.push(path.join("/Applications", bundle, "Contents", "MacOS", exe), path.join(home, "Applications", bundle, "Contents", "MacOS", exe));
  }
  return candidates.find((file) => installed(file)) ?? artCraftInstallPath(app, home);
}

/**
 * Points every ArtCraft server at the CLI this computer has now and switches
 * each on the first time its CLI is found; `switchedOn` names those already
 * switched on once, whose on/off is the person's from then on.
 */
export function reconcileArtCraft(
  list: readonly McpServerConfig[],
  switchedOn: ReadonlySet<string>,
  home = os.homedir(),
  installed: (file: string) => boolean = existsSync,
): { servers: McpServerConfig[]; switchedOn: Set<string>; changed: boolean } {
  const on = new Set(switchedOn);
  let changed = false;
  const servers = list.map((server) => {
    if (!isArtCraftApp(server.name) || server.url || !server.command) return server;
    const found = artCraftCli(server.name, home, installed);
    if (!installed(found)) return server;
    let next = server;
    if (!installed(server.command) && server.command !== found) next = { ...next, command: found };
    if (!on.has(server.name)) {
      on.add(server.name);
      if (next.disabled) {
        const { disabled: _disabled, ...rest } = next;
        next = rest;
      }
    }
    if (next !== server) changed = true;
    return next;
  });
  return { servers, switchedOn: on, changed };
}

export function artCraftDefaults(home = os.homedir(), installed: (file: string) => boolean = existsSync): McpDefault[] {
  const roots = ["--automation-read-root", home, "--automation-write-root", designFilesDir(home)];
  const args: Record<(typeof ARTCRAFT_APPS)[number], string[]> = {
    figcraft: ["mcp", ...roots],
    vectorcraft: ["mcp", "--headless", ...roots],
    photocraft: ["mcp", ...roots],
    anicraft: ["mcp"],
    effectcraft: ["mcp", ...roots],
    filmcraft: ["mcp"],
    lightcraft: ["mcp", "--library", path.join(home, "Pictures", "LightCraftLibrary")],
    solidcraft: ["mcp", "--headless", "--allow-path", home],
    soundcraft: ["mcp", ...roots],
  };
  return ARTCRAFT_APPS.map((app) => {
    const command = artCraftCli(app, home, installed);
    return { name: app, command, args: args[app], ...(installed(command) ? {} : { disabled: true }), since: 2 };
  });
}

export const MCP_DEFAULTS: readonly McpDefault[] = [
  { name: "filesystem", command: "npx", args: ["-y", "@modelcontextprotocol/server-filesystem"], ...off },
  { name: "git", command: "uvx", args: ["mcp-server-git", "--repository", "."], ...off },
  { name: "github", url: "https://api.githubcopilot.com/mcp/", ...off },
  {
    name: "gitlab",
    command: "npx",
    args: ["-y", "@zereight/mcp-gitlab"],
    env: { GITLAB_API_URL: "https://gitlab.com/api/v4" },
    ...off,
  },
  { name: "slack", command: "npx", args: ["-y", "@modelcontextprotocol/server-slack"], ...off },
  { name: "huggingface", url: "https://huggingface.co/mcp", ...off },
  { name: "browser", command: "npx", args: ["-y", "@browsermcp/mcp@latest"], since: 1 },
  {
    name: "unity",
    command: "uvx",
    args: ["--from", "mcpforunityserver==10.3.0", "mcp-for-unity", "--transport", "stdio"],
    env: { DISABLE_TELEMETRY: "true" },
    ...off,
  },
  { name: "godot", command: "uvx", args: ["--from", "godot-ai==4.3.0", "godot-ai", "attach", "--disable-telemetry"], ...off },
  { name: "unreal", url: "http://127.0.0.1:8000/mcp", ...off },
  {
    name: "blender",
    command: "uvx",
    args: ["mcp-for-blender"],
    env: { DISABLE_TELEMETRY: "true", BLENDER_MCP_SAFE_MODE: "1" },
    ...off,
  },
  {
    name: "jupyter",
    command: "uvx",
    args: ["jupyter-mcp-server==2.2.3"],
    env: { JUPYTER_URL: "http://localhost:8888", ALLOW_IMG_OUTPUT: "true" },
    ...off,
  },
  { name: "duckdb", command: "uvx", args: ["mcp-server-motherduck", "--db-path", ":memory:", "--read-write"], ...off },
  { name: "dbt", command: "uvx", args: ["dbt-mcp"], ...off },
  {
    name: "mlflow",
    command: "uvx",
    args: ["--from", "mlflow[mcp]>=3.5.1", "mlflow", "mcp", "run"],
    env: { MLFLOW_TRACKING_URI: "http://localhost:5000", MLFLOW_MCP_TOOLS: "ml" },
    ...off,
  },
  {
    name: "semgrep",
    command: "uvx",
    args: ["--from", "semgrep==1.180.0", "semgrep", "mcp"],
    env: { SEMGREP_SEND_METRICS: "off" },
    ...off,
  },
  { name: "osv-scanner", command: "osv-scanner", args: ["experimental-mcp"], ...off },
  {
    name: "github-security",
    url: "https://api.githubcopilot.com/mcp/",
    headers: {
      "X-MCP-Toolsets": "repos,pull_requests,code_security,secret_protection,dependabot,security_advisories",
      "X-MCP-Readonly": "true",
      "X-MCP-Lockdown": "true",
    },
    ...off,
  },
  { name: "snyk", command: "npx", args: ["-y", "snyk@1.1307.4", "mcp", "-t", "stdio"], ...off },
  ...artCraftDefaults(),
];

/** `list` with every default newer than `seeded` that it does not already name. */
export function withMcpDefaults(list: readonly McpServerConfig[], seeded: number): McpServerConfig[] {
  const names = new Set(list.map((s) => s.name));
  const added = MCP_DEFAULTS.filter((d) => d.since > seeded && !names.has(d.name)).map(({ since: _since, ...server }) => server);
  return [...list, ...added];
}

const VERSION_FILE = "mcp-defaults.json";

interface SeedableStore {
  read(): McpServerConfig[];
  readKnown(): McpServerConfig[] | null;
  store(servers: McpServerConfig[]): void;
}

/**
 * The MCP server store the runner and the key window read, which adds the
 * defaults the first time it is read in this process. It writes only over a
 * list it could read: a file the key store cannot open is left as it is.
 */
export class SeededMcpServerStore {
  readonly #inner: SeedableStore;
  readonly #dir: string;
  readonly #log: (line: string) => void;
  #checked = false;

  constructor(inner: SeedableStore, dir: string, log: (line: string) => void = () => undefined) {
    this.#inner = inner;
    this.#dir = dir;
    this.#log = log;
  }

  read(): McpServerConfig[] {
    this.#seed();
    return this.#inner.read();
  }

  store(servers: McpServerConfig[]): void {
    this.#inner.store(servers);
  }

  #seededState(): { version: number; artcraftOn: string[] } {
    try {
      const raw = JSON.parse(readFileSync(path.join(this.#dir, VERSION_FILE), "utf8")) as { version?: unknown; artcraftOn?: unknown };
      const version = typeof raw.version === "number" && Number.isInteger(raw.version) && raw.version >= 0 ? raw.version : 0;
      const artcraftOn = Array.isArray(raw.artcraftOn) ? raw.artcraftOn.filter((n): n is string => typeof n === "string") : [];
      return { version, artcraftOn };
    } catch {
      return { version: 0, artcraftOn: [] };
    }
  }

  #seed(): void {
    if (this.#checked) return;
    this.#checked = true;
    const seeded = this.#seededState();
    const list = this.#inner.readKnown();
    if (list === null) return;
    const withDefaults = seeded.version < MCP_DEFAULTS_VERSION ? withMcpDefaults(list, seeded.version) : list;
    const added = withDefaults.slice(list.length);
    // A default added switched on was switched on by this app: its on/off is the person's from now on.
    const alreadyOn = new Set([...seeded.artcraftOn, ...added.filter((s) => isArtCraftApp(s.name) && !s.disabled).map((s) => s.name)]);
    const reconciled = reconcileArtCraft(withDefaults, alreadyOn);
    const marksChanged = reconciled.switchedOn.size !== seeded.artcraftOn.length;
    if (seeded.version >= MCP_DEFAULTS_VERSION && !reconciled.changed && !marksChanged) return;
    try {
      if (added.length > 0 || reconciled.changed) this.#inner.store(reconciled.servers);
      mkdirSync(designFilesDir(), { recursive: true });
      mkdirSync(this.#dir, { recursive: true });
      const state = { version: MCP_DEFAULTS_VERSION, artcraftOn: [...reconciled.switchedOn].sort() };
      writeFileSync(path.join(this.#dir, VERSION_FILE), `${JSON.stringify(state)}\n`);
      if (added.length > 0) this.#log(`[keys] added ${added.length} default MCP servers`);
      if (reconciled.changed) this.#log("[keys] pointed the ArtCraft MCP servers at the CLIs on this computer");
    } catch (err) {
      this.#log(`[keys] could not add the default MCP servers: ${err instanceof Error ? err.message : String(err)}`);
    }
  }
}
