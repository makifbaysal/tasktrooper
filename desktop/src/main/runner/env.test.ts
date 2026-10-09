import { describe, expect, it, vi } from "vitest";
import type { PreflightReport, RunnerPairingBundle, UserSettings } from "../../ipc/types.js";

vi.mock("electron", () => ({
  app: { getAppPath: () => "/app", getPath: () => "/userData", isPackaged: false },
}));

const { childEnv, preflightMessage, runnerConfig } = await import("./env.js");

const bundle: RunnerPairingBundle = {
  runner_token: "rtok-1",
  tm_base_url: "https://app.tasktrooper.ai",
  tenant_id: "tenant-1",
  member_uid: "member-1",
  paired_at: "2026-09-28T00:00:00Z",
  label: "Akif's MacBook",
};

const settings: UserSettings = {
  workspaceDir: "/Users/me/TaskTrooper",
  launchAtLogin: false,
  autoConnect: true,
  notifications: { enabled: false, analizReview: false, humanUat: false, humanNeeded: false, agentComments: false, agentChatReplies: false },
  mode: "account",
  accountOrigin: "https://app.tasktrooper.ai",
};

const config = (preflight: PreflightReport, extra: Partial<Parameters<typeof runnerConfig>[0]> = {}): Record<string, unknown> =>
  JSON.parse(runnerConfig({ bundle, settings, preflight, providers: [], executorDataDir: "/userData/executor", ...extra })) as Record<
    string,
    unknown
  >;

const report = (extra: PreflightReport["items"] = []): PreflightReport => ({
  generatedAt: 1,
  ready: true,
  items: [
    { id: "runner", label: "TaskTrooper runner", required: true, status: "ok", path: "/app/bin/runner" },
    { id: "git", label: "git", required: true, status: "ok", path: "/usr/bin/git" },
    { id: "claude", label: "Claude Code CLI", required: true, status: "ok", path: "/opt/homebrew/bin/claude" },
    ...extra,
  ],
});

/**
 * `desktop/runner/main_test.go`'s `TestEveryKeyTheSupervisorSendsIsAFieldThisBinaryDeclares`
 * reads THIS FILE's compiled object literal and checks every key it sends
 * against the runner's own `wireConfig` — so this suite's job is to prove
 * `runnerConfig` actually produces the keys that test extracts, not just that
 * the function returns something.
 */
describe("runnerConfig", () => {
  it("sends the required fields the runner refuses to start without", () => {
    const parsed = config(report());
    expect(parsed.tm_base_url).toBe("https://app.tasktrooper.ai");
    expect(parsed.runner_token).toBe("rtok-1");
    expect(parsed.tenant_id).toBe("tenant-1");
    expect(parsed.member_uid).toBe("member-1");
    expect(parsed.workspace_dir).toBe("/Users/me/TaskTrooper");
    expect(parsed.claude_bin).toBe("/opt/homebrew/bin/claude");
    expect(parsed.git_bin).toBe("/usr/bin/git");
    expect(parsed.reconnect_max_backoff).toBe("30s");
  });

  it("ends with exactly one newline, which is what a line-oriented stdin reader needs", () => {
    const raw = runnerConfig({ bundle, settings, preflight: report(), providers: [], executorDataDir: "/userData/executor" });
    expect(raw.endsWith("\n")).toBe(true);
    expect(raw.slice(0, -1).includes("\n")).toBe(false);
  });

  it("omits every optional binary this Mac does not have, rather than sending an empty path", () => {
    const parsed = config(report());
    for (const key of [
      "xcrun_bin",
      "adb_bin",
      "emulator_bin",
      "appium_base_url",
      "appium_bin",
      "cursor_agent_bin",
      "opencode_bin",
    ]) {
      expect(parsed).not.toHaveProperty(key);
    }
  });

  it("sends the optional binaries this Mac does have — xcrun, appium, cursor-agent, opencode", () => {
    const parsed = config(
      report([
        { id: "xcode-clt", label: "Xcode command line tools", required: false, status: "ok", path: "/usr/bin/xcrun" },
        { id: "appium", label: "Appium", required: false, status: "ok", path: "/opt/homebrew/bin/appium" },
        { id: "cursor-agent", label: "Cursor CLI", required: false, status: "ok", path: "/usr/local/bin/cursor-agent" },
        { id: "opencode", label: "OpenCode CLI", required: false, status: "ok", path: "/opt/homebrew/bin/opencode" },
      ]),
    );
    expect(parsed.xcrun_bin).toBe("/usr/bin/xcrun");
    expect(parsed.appium_base_url).toBe("http://127.0.0.1:4723");
    // The runner starts the hub on demand, so it needs the binary as well as
    // the address.
    expect(parsed.appium_bin).toBe("/opt/homebrew/bin/appium");
    expect(parsed.cursor_agent_bin).toBe("/usr/local/bin/cursor-agent");
    expect(parsed.opencode_bin).toBe("/opt/homebrew/bin/opencode");
  });

  it("never sends antigravity_bin — the field the runner's wireConfig no longer declares", () => {
    const parsed = config(report());
    expect(parsed).not.toHaveProperty("antigravity_bin");
  });

  it("never sends the embeddings fields while account mode runs no embedder", () => {
    const parsed = config(report());
    expect(parsed).not.toHaveProperty("embeddings_base_url");
    expect(parsed).not.toHaveProperty("embedding_model");
  });

  it("passes the embedder's URL on for the executor once there is one", () => {
    expect(config(report(), { embeddingsBaseURL: "http://127.0.0.1:5123" }).embeddings_base_url).toBe("http://127.0.0.1:5123");
  });

  it("sends the executor and its data directory only when this build has one", () => {
    expect(config(report())).not.toHaveProperty("executor_bin");
    expect(config(report())).not.toHaveProperty("executor_data_dir");
    const missing = config(
      report([{ id: "executor", label: "TaskTrooper executor", required: false, status: "missing" }]),
    );
    expect(missing).not.toHaveProperty("executor_bin");
    const parsed = config(
      report([{ id: "executor", label: "TaskTrooper executor", required: false, status: "ok", path: "/app/bin/executor" }]),
    );
    expect(parsed.executor_bin).toBe("/app/bin/executor");
    expect(parsed.executor_data_dir).toBe("/userData/executor");
  });

  /** The keys travel on this pipe and nowhere else: not argv, not the environment. */
  it("sends the member's providers, keys included, and omits the field when there are none", () => {
    expect(config(report())).not.toHaveProperty("providers");
    const providers = [
      { id: "openai", type: "openai", api_key: "sk-1", models: ["gpt-4.1"] },
      { id: "lmstudio", type: "openai", base_url: "http://127.0.0.1:1234/v1" },
    ];
    expect(config(report(), { providers }).providers).toEqual(providers);
  });

  it("sends no policy: the runner's defaults are the rules this app runs under", () => {
    expect(config(report())).not.toHaveProperty("policy");
  });
});

describe("preflightMessage", () => {
  it("wraps the report in the control channel's envelope", () => {
    const rep = report();
    expect(JSON.parse(preflightMessage(rep))).toEqual({ type: "preflight", report: rep });
  });
});

describe("childEnv", () => {
  it("strips the variables that would make the runner think it is Electron", () => {
    process.env.ELECTRON_RUN_AS_NODE = "1";
    process.env.NODE_OPTIONS = "--inspect";
    try {
      const e = childEnv();
      expect(e.ELECTRON_RUN_AS_NODE).toBeUndefined();
      expect(e.NODE_OPTIONS).toBeUndefined();
      if (process.platform === "darwin") expect(e.PATH).toContain("/opt/homebrew/bin");
    } finally {
      delete process.env.ELECTRON_RUN_AS_NODE;
      delete process.env.NODE_OPTIONS;
    }
  });

  it("puts each detected CLI's directory and NVM_BIN on PATH, ahead of what was there", () => {
    const nvm = "/Users/me/.nvm/versions/node/v22.1.0/bin";
    const e = childEnv(
      report([
        { id: "opencode", label: "OpenCode CLI", required: false, status: "ok", path: "/Users/me/.opencode/bin/opencode" },
      ]),
      { PATH: "/usr/bin:/bin", NVM_BIN: nvm },
      "linux",
    );
    expect(e.PATH).toBe(`/opt/homebrew/bin:/Users/me/.opencode/bin:${nvm}:/usr/bin:/bin`);
  });

  /** The runner starts the hub itself, and `appium` finds node through PATH. */
  it("puts the directory of an nvm-installed appium on PATH", () => {
    const dir = "/Users/me/.nvm/versions/node/v20.11.0/bin";
    const e = childEnv(
      report([{ id: "appium", label: "Appium", required: false, status: "ok", path: `${dir}/appium` }]),
      { PATH: "/usr/bin:/bin" },
      "linux",
    );
    expect(e.PATH?.split(":")).toContain(dir);
  });

  it("leaves a directory that is already on PATH where the user put it", () => {
    const e = childEnv(report(), { PATH: "/usr/local/bin:/usr/bin:/opt/homebrew/bin" }, "darwin");
    expect(e.PATH).toBe("/usr/local/bin:/usr/bin:/opt/homebrew/bin");
  });

  it("adds the Homebrew prefixes on macOS only", () => {
    expect(childEnv(undefined, { PATH: "/usr/bin" }, "darwin").PATH).toBe("/usr/bin:/opt/homebrew/bin:/usr/local/bin");
    expect(childEnv(undefined, { PATH: "/usr/bin" }, "linux").PATH).toBe("/usr/bin");
  });

  /**
   * Windows spells it `Path`, and a copy of `process.env` is a plain object
   * that no longer matches names case-insensitively. Reading `env.PATH` there
   * found nothing and wrote a second, mac-shaped PATH beside the real one.
   */
  it("keeps Windows' own spelling, its `;` delimiter, and exactly one PATH key", () => {
    const windows: PreflightReport = {
      generatedAt: 1,
      ready: true,
      items: [
        { id: "claude", label: "Claude Code CLI", required: true, status: "ok", path: "C:\\Users\\me\\AppData\\Roaming\\npm\\claude.cmd" },
        { id: "git", label: "git", required: true, status: "ok", path: "C:\\Program Files\\Git\\cmd\\git.exe" },
      ],
    };
    const e = childEnv(
      windows,
      { Path: "C:\\Windows\\system32;c:\\program files\\git\\cmd", PATH: "stale", SystemRoot: "C:\\Windows" },
      "win32",
    );
    const keys = Object.keys(e).filter((k) => k.toUpperCase() === "PATH");
    expect(keys).toEqual(["Path"]);
    expect(e.Path).toBe("C:\\Users\\me\\AppData\\Roaming\\npm;C:\\Windows\\system32;c:\\program files\\git\\cmd");
    expect(e.Path).not.toContain("/opt/homebrew/bin");
    expect(e.SystemRoot).toBe("C:\\Windows");
  });
});
