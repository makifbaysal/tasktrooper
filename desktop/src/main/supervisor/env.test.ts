import { describe, expect, it, vi } from "vitest";
import type { PreflightReport } from "../../ipc/types.js";

vi.mock("electron", () => ({
  app: { getAppPath: () => "/app", getPath: () => "/userData", isPackaged: false },
}));

const { agentServerEnv, appiumArgs, childEnv } = await import("./env.js");
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
    vi.stubEnv("EMBEDDINGS_BASE_URL", "http://10.0.0.9:1234");
    try {
      const e = env();
      expect(e.CHROME_BIN).toBeUndefined();
      expect(e.MOBILE_APPIUM_HUB_URL).toBeUndefined();
      expect(e.EMBEDDINGS_BASE_URL).toBeUndefined();
    } finally {
      vi.unstubAllEnvs();
    }
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
    try {
      const e = env();
      expect(e.DATABASE_URL).toBeUndefined();
      expect(e.PUBLIC_BASE_URL).toBeUndefined();
      expect(e.CONFIG_PATH).toBeUndefined();
      expect(e.CORS_ORIGINS).toBeUndefined();
      expect(e.PORT).toBe("0");
      expect(e.SERVER_API_KEY).toBe("token-1");
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it("names the hub and the browser when they were found", () => {
    const e = env([
      { id: "appium", label: "Appium", required: false, status: "ok", path: "/opt/homebrew/bin/appium" },
      { id: "chrome", label: "Chrome / Chromium", required: false, status: "ok", path: "/Applications/C.app/x" },
    ]);
    expect(e.MOBILE_APPIUM_HUB_URL).toBe(APPIUM_BASE_URL);
    expect(e.CHROME_BIN).toBe("/Applications/C.app/x");
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
});

describe("appiumArgs", () => {
  /**
   * A hub on 0.0.0.0 is a remote-control interface for every device attached to
   * somebody's laptop.
   */
  it("binds the hub to loopback and to Appium's own port", () => {
    expect(appiumArgs()).toEqual(["--address", "127.0.0.1", "--port", "4723"]);
    expect(APPIUM_BASE_URL).toBe("http://127.0.0.1:4723");
  });
});
