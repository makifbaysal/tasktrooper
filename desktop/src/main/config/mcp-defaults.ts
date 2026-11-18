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

function artCraftCli(app: string, home: string): string {
  const dir = process.env.TASKTROOPER_ARTCRAFT_DIR || path.join(home, "artCraft");
  const name = `${app}-cli`;
  return path.join(dir, name, process.platform === "win32" ? `${name}.exe` : name);
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
    const command = artCraftCli(app, home);
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

  #seededVersion(): number {
    try {
      const raw: unknown = JSON.parse(readFileSync(path.join(this.#dir, VERSION_FILE), "utf8"));
      const version = (raw as { version?: unknown }).version;
      return typeof version === "number" && Number.isInteger(version) && version >= 0 ? version : 0;
    } catch {
      return 0;
    }
  }

  #seed(): void {
    if (this.#checked) return;
    this.#checked = true;
    const seeded = this.#seededVersion();
    if (seeded >= MCP_DEFAULTS_VERSION) return;
    const list = this.#inner.readKnown();
    if (list === null) return;
    const next = withMcpDefaults(list, seeded);
    try {
      if (next.length > list.length) this.#inner.store(next);
      mkdirSync(designFilesDir(), { recursive: true });
      mkdirSync(this.#dir, { recursive: true });
      writeFileSync(path.join(this.#dir, VERSION_FILE), `${JSON.stringify({ version: MCP_DEFAULTS_VERSION })}\n`);
      this.#log(`[keys] added ${next.length - list.length} default MCP servers`);
    } catch (err) {
      this.#log(`[keys] could not add the default MCP servers: ${err instanceof Error ? err.message : String(err)}`);
    }
  }
}
