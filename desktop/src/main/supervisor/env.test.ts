import { describe, expect, it, vi } from "vitest";
import type { PreflightReport } from "../../ipc/types.js";

vi.mock("electron", () => ({
  app: { getAppPath: () => "/app", getPath: () => "/userData", isPackaged: false },
}));

// Root-level /catalog does not exist on this machine, but catalogRoot()
// resolves to it in dev; pretending it is there lets a test pin that the
// backend is always handed the bundled catalog path.
vi.mock("node:fs", () => ({
  existsSync: (p: string) => p === "/catalog",
  // fnm's per-shell directory: a different name for the same binary each launch.
  realpathSync: (p: string) => p.replace(/^\/multishell\/\d+\//, "/fnm/default/"),
}));

const { agentServerEnv, childEnv, spawnFingerprint } = await import("./env.js");
const { APPIUM_BASE_URL } = await import("../services/detect.js");

const report = (extra: PreflightReport["items"] = []): PreflightReport => ({
  generatedAt: 1,
  ready: true,
  items: [
    { id: "claude", label: "Claude Code CLI", required: true, status: "ok", path: "/opt/homebrew/bin/claude" },
    { id: "git", label: "git", required: true, status: "ok", path: "/usr/bin/git" },
    ...extra,
  ],
});

const env = (extra: PreflightReport["items"] = [], embeddingsBaseURL?: string): NodeJS.ProcessEnv =>
  agentServerEnv({
    preflight: report(extra),
    dataDir: "/userData/data",
    postgresCacheDir: "/userData/postgres-bin",
    apiToken: "token-1",
    mcpSecretsKey: "key-1",
    ...(embeddingsBaseURL !== undefined ? { embeddingsBaseURL } : {}),
  });

describe("agentServerEnv", () => {
  /**
   * The backend picks its own port and prints it. Anything else would be two
   * settings for one fact, with nothing to notice when they drift apart.
   */
  it("asks the backend to choose its own port", () => {
    expect(env().PORT).toBe("0");
  });

  /**
   * Closing stdin is how the backend is stopped on every platform, so the watch
   * must be switched on for the spawn that gets the pipe.
   */
  it("tells the backend to stop when its stdin closes", () => {
    expect(env().SHUTDOWN_ON_STDIN_CLOSE).toBe("1");
  });

  /**
   * Empty DATABASE_URL is what selects embedded Postgres. Setting one here —
   * even to something harmless — would quietly turn the local mode off.
   */
  it("never names a database, because an absent DSN is what starts the embedded one", () => {
    expect(env().DATABASE_URL).toBeUndefined();
  });

  it("carries the generated secrets and the data directories", () => {
    const e = env();
    expect(e.SERVER_API_KEY).toBe("token-1");
    expect(e.MCP_SECRETS_KEY).toBe("key-1");
    expect(e.DATA_DIR).toBe("/userData/data");
    expect(e.EMBEDDED_POSTGRES_CACHE_DIR).toBe("/userData/postgres-bin");
    expect(e.ALLOWED_ROOTS).toBe("*");
  });

  it("respects TASKTROOPER_ALLOWED_ROOTS if set in the parent process", () => {
    vi.stubEnv("TASKTROOPER_ALLOWED_ROOTS", "/custom/path");
    try {
      expect(env().ALLOWED_ROOTS).toBe("/custom/path");
    } finally {
      vi.unstubAllEnvs();
    }
  });

  /**
   * Detection lives in services/detect.ts and nowhere else. A second search on
   * the Go side with slightly different rules is how a Mac ends up running one
   * `claude` and reporting another.
   */
  it("hands over the binaries detection actually found", () => {
    expect(env().CLAUDE_CODE_BIN).toBe("/opt/homebrew/bin/claude");
  });

  /**
   * The GUI app's PATH is not the user's shell PATH, so a CLI installed into
   * ~/.local/bin is only findable by the server if its path is handed over.
   */
  it("hands over every other agent CLI it found, and none it did not", () => {
    const e = env([
      { id: "cursor-agent", label: "Cursor CLI", required: false, status: "ok", path: "/Users/me/.local/bin/cursor-agent" },
      { id: "opencode", label: "OpenCode CLI", required: false, status: "ok", path: "/opt/homebrew/bin/opencode" },
      { id: "agy", label: "Antigravity CLI", required: false, status: "missing" },
    ]);
    expect(e.CURSOR_AGENT_BIN).toBe("/Users/me/.local/bin/cursor-agent");
    expect(e.OPENCODE_BIN).toBe("/opt/homebrew/bin/opencode");
    expect(e.ANTIGRAVITY_BIN).toBeUndefined();
  });

  /**
   * OMITTED, not empty: the backend treats an absent value as "this Mac cannot
   * do that" and an empty one as a path to exec.
   */
  it("says nothing about a capability this Mac does not have", () => {
    vi.stubEnv("CHROME_BIN", "/usr/bin/google-chrome");
    vi.stubEnv("MOBILE_APPIUM_HUB_URL", "http://10.0.0.9:4723");
    vi.stubEnv("APPIUM_BIN", "/somewhere/else/appium");
    vi.stubEnv("EMBEDDINGS_BASE_URL", "http://10.0.0.9:1234");
    try {
      const e = env();
      expect(e.CHROME_BIN).toBeUndefined();
      expect(e.MOBILE_APPIUM_HUB_URL).toBeUndefined();
      expect(e.APPIUM_BIN).toBeUndefined();
      expect(e.EMBEDDINGS_BASE_URL).toBeUndefined();
    } finally {
      vi.unstubAllEnvs();
    }
  });

  /**
   * The catalog ships with the app (Resources/catalog, or the monorepo's
   * catalog/ in dev). Leaving the backend without it is an install with no
   * role agents at all.
   */
  it("hands the backend the bundled role catalog path", () => {
    expect(env().AGENT_CATALOG_REPO).toBe("/catalog");
  });

  /**
   * A shell with DATABASE_URL exported must not steer the backend away from
   * its embedded database, nor may any other key the backend reads leak in.
   */
  it("never inherits a key the backend reads from the parent environment", () => {
    vi.stubEnv("DATABASE_URL", "postgres://someone:else@db.example:5432/prod");
    vi.stubEnv("PUBLIC_BASE_URL", "https://example.invalid");
    vi.stubEnv("CONFIG_PATH", "/etc/elsewhere.yml");
    vi.stubEnv("CORS_ORIGINS", "*");
    vi.stubEnv("AGENT_CATALOG_REPO", "/operator-picked/catalog");
    try {
      const e = env();
      expect(e.DATABASE_URL).toBeUndefined();
      expect(e.PUBLIC_BASE_URL).toBeUndefined();
      expect(e.CONFIG_PATH).toBeUndefined();
      expect(e.CORS_ORIGINS).toBeUndefined();
      // The parent shell's choice is discarded in favour of the bundled copy;
      // see "hands the backend the bundled role catalog path".
      expect(e.AGENT_CATALOG_REPO).toBe("/catalog");
      expect(e.PORT).toBe("0");
      expect(e.SERVER_API_KEY).toBe("token-1");
    } finally {
      vi.unstubAllEnvs();
    }
  });

  /**
   * The backend starts the hub itself, on demand, so it needs the binary as
   * well as the address — and the address stays Appium's own loopback default,
   * because a hub on 0.0.0.0 is a remote-control interface for every device
   * attached to somebody's laptop.
   */
  it("names the hub, the appium that serves it, and the browser when they were found", () => {
    const e = env([
      { id: "appium", label: "Appium", required: false, status: "ok", path: "/opt/homebrew/bin/appium" },
      { id: "chrome", label: "Chrome / Chromium", required: false, status: "ok", path: "/Applications/C.app/x" },
    ]);
    expect(e.MOBILE_APPIUM_HUB_URL).toBe(APPIUM_BASE_URL);
    expect(APPIUM_BASE_URL).toBe("http://127.0.0.1:4723");
    expect(e.APPIUM_BIN).toBe("/opt/homebrew/bin/appium");
    expect(e.CHROME_BIN).toBe("/Applications/C.app/x");
  });

  it("hands the backend no hub at all when Appium is not installed", () => {
    const e = env([{ id: "appium", label: "Appium", required: false, status: "missing" }]);
    expect(e.MOBILE_APPIUM_HUB_URL).toBeUndefined();
    expect(e.APPIUM_BIN).toBeUndefined();
  });

  it("passes the embedder's resolved address through as the OpenAI-compatible host", () => {
    expect(env([], "http://127.0.0.1:4319").EMBEDDINGS_BASE_URL).toBe("http://127.0.0.1:4319");
  });

  /**
   * Nothing that belonged to the cloud deployment may survive here; a value
   * that is still passed is a value someone will wire back up.
   */
  it("carries nothing from the control plane", () => {
    const e = env();
    for (const gone of ["INTERNAL_AUTH_KEY", "CONTROL_PLANE_URL", "TENANT_UID", "CLOUD_MODE"]) {
      expect(e[gone]).toBeUndefined();
    }
  });
});

describe("childEnv", () => {
  /**
   * ELECTRON_RUN_AS_NODE makes any Node-based CLI a child launches behave as if
   * it were Electron, which fails with no useful message. Appium is exactly
   * such a CLI.
   */
  it("strips the variables that would make a child think it is Electron", () => {
    process.env.ELECTRON_RUN_AS_NODE = "1";
    try {
      const e = childEnv(report());
      expect(e.ELECTRON_RUN_AS_NODE).toBeUndefined();
      if (process.platform === "darwin") expect(e.PATH).toContain("/opt/homebrew/bin");
    } finally {
      delete process.env.ELECTRON_RUN_AS_NODE;
    }
  });

  /**
   * A CLI installed under nvm needs the node beside it reachable, but the
   * user's own PATH order must win: every agent command inherits this PATH.
   */
  it("appends each found CLI's directory after the user's own PATH", () => {
    vi.stubEnv("PATH", "/usr/local/bin:/usr/bin:/bin");
    try {
      const e = childEnv(
        report([
          { id: "opencode", label: "OpenCode CLI", required: false, status: "ok", path: "/home/me/.nvm/versions/node/v22/bin/opencode" },
          { id: "agy", label: "Antigravity CLI", required: false, status: "missing", path: "/nope/agy" },
        ]),
      );
      const parts = (e.PATH ?? "").split(":");
      expect(parts.slice(0, 3)).toEqual(["/usr/local/bin", "/usr/bin", "/bin"]);
      expect(parts).toContain("/home/me/.nvm/versions/node/v22/bin");
      expect(parts.indexOf("/home/me/.nvm/versions/node/v22/bin")).toBeGreaterThan(parts.indexOf("/bin"));
      expect(parts).not.toContain("/nope");
    } finally {
      vi.unstubAllEnvs();
    }
  });

  /**
   * AppRun's LD_LIBRARY_PATH and PATH point at the AppImage's own libraries;
   * git, claude and Postgres must not load them. The embedder is this app's own
   * binary and needs them.
   */
  it("strips an AppImage's environment from every child but this app's own binary", () => {
    vi.stubEnv("APPIMAGE", "/home/me/TaskTrooper.AppImage");
    vi.stubEnv("APPDIR", "/tmp/.mount_TT");
    vi.stubEnv("PATH", "/tmp/.mount_TT:/tmp/.mount_TT/usr/sbin:/usr/bin");
    vi.stubEnv("LD_LIBRARY_PATH", "/tmp/.mount_TT/usr/lib");
    try {
      const external = childEnv(report());
      expect(external.PATH).not.toContain("/tmp/.mount_TT");
      expect(external.LD_LIBRARY_PATH).toBeUndefined();
      expect(external.APPIMAGE).toBeUndefined();

      const own = childEnv(report(), { ownBinary: true });
      expect(own.LD_LIBRARY_PATH).toBe("/tmp/.mount_TT/usr/lib");
    } finally {
      vi.unstubAllEnvs();
    }
  });
});

/**
 * The backend starts on the gating half of a sweep and the supervisor restarts
 * it only when the complete half changes what its environment is built from.
 * Both directions matter: a missed difference is a backend with the wrong
 * CLAUDE_CODE_BIN, and a false one is a restart on every launch.
 */
describe("spawnFingerprint", () => {
  const claude = (patch: Partial<PreflightReport["items"][number]>): PreflightReport["items"][number] => ({
    id: "claude",
    label: "Claude Code CLI",
    required: false,
    status: "ok",
    path: "/opt/homebrew/bin/claude",
    ...patch,
  });
  const with_ = (items: PreflightReport["items"]): PreflightReport => ({ generatedAt: 1, ready: true, items });

  it("changes when a provisionally usable claude turns out unusable", () => {
    expect(spawnFingerprint(with_([claude({})]))).not.toBe(spawnFingerprint(with_([claude({ status: "unusable" })])));
  });

  it("changes when a CLI appears that the backend was not told about", () => {
    const opencode = { id: "opencode" as const, label: "OpenCode", required: false, status: "ok" as const, path: "/o/opencode" };
    expect(spawnFingerprint(with_([claude({})]))).not.toBe(spawnFingerprint(with_([claude({}), opencode])));
  });

  it("does not change for what the backend never reads: versions, details, the account", () => {
    const account = { id: "claude-account" as const, label: "Claude account", required: false, status: "missing" as const };
    expect(spawnFingerprint(with_([claude({})]))).toBe(
      spawnFingerprint(with_([claude({ version: "2.1.0", detail: "x" }), account])),
    );
  });

  it("does not change when the same binary is reached through a different symlink", () => {
    expect(spawnFingerprint(with_([claude({ path: "/multishell/111/bin/claude" })]))).toBe(
      spawnFingerprint(with_([claude({ path: "/multishell/222/bin/claude" })])),
    );
  });
});
