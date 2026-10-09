import path from "node:path";
import type { PreflightReport, RunnerPairingBundle, UserSettings } from "../../ipc/types.js";
import type { ProviderConfig } from "../config/providers.js";
import { emulatorPathFor, itemById } from "../services/detect.js";

/**
 * What the runner is started with, account mode's one child process.
 *
 * One JSON document on stdin, no environment variables at all — the runner
 * reads none of it for configuration. Its token and the member's API keys are
 * secrets, and on macOS a same-user process can read another's environment
 * through `KERN_PROCARGS2` (`ps eww <pid>` does exactly that), so a secret in
 * a child's environment is readable by any app the user launches. A pipe has
 * no such interface, and argv is worse still: it is world-readable.
 *
 * `desktop/runner/main_test.go`'s
 * `TestEveryKeyTheSupervisorSendsIsAFieldThisBinaryDeclares` reads this file's
 * source and checks every key `runnerConfig` sends against the runner's own
 * `wireConfig` struct — it reads the object literal `runnerConfig` builds and
 * stringifies below, so nothing outside that literal is inspected and nothing
 * inside it may name a field the runner does not declare
 * (`DisallowUnknownFields` refuses to start otherwise). Nested objects (the
 * providers) are built outside the literal for that reason. That test greps
 * this file's own text for the exact expression `runnerConfig` returns, so do
 * not quote that expression elsewhere in this file — including in a comment.
 */

export interface RunnerConfigInputs {
  bundle: RunnerPairingBundle;
  settings: UserSettings;
  preflight: PreflightReport;
  /** The member's own providers, keys included; handed on to the executor. */
  providers: ProviderConfig[];
  /** `executorDataDir()`, sent with the executor's path when there is one. */
  executorDataDir: string;
  /**
   * The local embedder's URL for the executor. Absent until account mode runs
   * the embedder (`ACCOUNT_MODE_RUNS_EMBEDDER`).
   */
  embeddingsBaseURL?: string;
}

/** The CLIs the runner execs, whose directories PATH must reach. */
const RUNNER_CLIS = ["claude", "git", "opencode", "cursor-agent", "appium"] as const;

/**
 * The environment the runner child inherits: this process's, minus what would
 * leak Electron's own state into it, with PATH made able to run what detection
 * found.
 *
 * The runner reads none of this for configuration — that is the stdin
 * document — but it spawns `claude`, `git`, `cursor-agent`, `opencode`, the
 * Appium hub and the executor, and the shebang of `claude` and `appium` is
 * `#!/usr/bin/env node`: launchd's PATH has no node in it, so a GUI-launched
 * runner would fail every session with "env: node: No such file or directory"
 * unless this is put back. So PATH gains each detected CLI's own directory (an
 * nvm or npm-prefix install keeps its node beside it) and NVM_BIN; a directory
 * already on PATH keeps its place, so the user's own order still decides which
 * of two tools wins.
 *
 * On Windows the variable is spelled `Path` and names are case-insensitive,
 * which a plain copy of the environment is not: the existing key is found
 * whatever its case, written back under that one name, and any other spelling
 * dropped so the child never sees two PATHs.
 */
export function childEnv(
  preflight?: PreflightReport,
  base: NodeJS.ProcessEnv = process.env,
  platform: NodeJS.Platform = process.platform,
): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...base };

  // Electron sets these for its own child processes. ELECTRON_RUN_AS_NODE in
  // particular makes any Node-based CLI a child launches behave as if it were
  // Electron, which is a failure with no useful message.
  delete env.ELECTRON_RUN_AS_NODE;
  delete env.ELECTRON_IS_DEV;
  delete env.NODE_OPTIONS;

  const win = platform === "win32";
  const paths = win ? path.win32 : path.posix;
  const pathKeys = Object.keys(env).filter((k) => (win ? k.toUpperCase() === "PATH" : k === "PATH"));
  const key = pathKeys[0] ?? "PATH";
  const same = (a: string, b: string): boolean => (win ? a.toLowerCase() === b.toLowerCase() : a === b);

  const parts = (env[key] ?? "").split(paths.delimiter).filter((p) => p !== "");
  const front: string[] = [];
  const ensure = (dir: string | undefined, into: string[]): void => {
    if (!dir || !paths.isAbsolute(dir)) return;
    if (parts.some((p) => same(p, dir)) || front.some((p) => same(p, dir))) return;
    into.push(dir);
  };
  for (const id of RUNNER_CLIS) {
    const bin = preflight ? itemById(preflight, id)?.path : undefined;
    ensure(bin ? paths.dirname(bin) : undefined, front);
  }
  ensure(base.NVM_BIN, front);
  if (platform === "darwin") {
    for (const extra of ["/opt/homebrew/bin", "/usr/local/bin"]) ensure(extra, parts);
  }

  for (const k of pathKeys) delete env[k];
  env[key] = [...front, ...parts].join(paths.delimiter);
  return env;
}

/**
 * The runner's stdin document, which is also the first line of its control
 * channel. Field names are the pairing bundle's where they overlap, so this
 * is a projection rather than a translation.
 *
 * The binary paths are passed rather than looked up again on the Go side:
 * detection lives in `services/detect.ts` and nowhere else — a second search
 * with slightly different rules is how a computer ends up running one `claude`
 * and reporting another.
 *
 * No `policy`: the runner's defaults are the TaskTrooper cloud's rules, and
 * this app has nothing to relax.
 */
export function runnerConfig(inputs: RunnerConfigInputs): string {
  const { bundle, settings, preflight } = inputs;
  const xcrun = itemById(preflight, "xcode-clt");
  const adb = itemById(preflight, "android-sdk");
  const appium = itemById(preflight, "appium");
  const emulator = emulatorPathFor(preflight);
  const cursorAgent = itemById(preflight, "cursor-agent");
  const opencode = itemById(preflight, "opencode");
  const executor = itemById(preflight, "executor");
  const claude = itemById(preflight, "claude");
  const executorBin = executor?.status === "ok" && executor.path ? executor.path : "";
  const providerList = inputs.providers.map((p) => ({ ...p }));
  const embeddings = inputs.embeddingsBaseURL ?? "";

  return `${JSON.stringify({
    tm_base_url: bundle.tm_base_url,
    runner_token: bundle.runner_token,
    tenant_id: bundle.tenant_id,
    // The member this computer belongs to. The control plane routes a task's
    // run to the computer of its assignee, so a runner that could not say who
    // it is would be a tenant-wide runner.
    member_uid: bundle.member_uid,
    workspace_dir: settings.workspaceDir,
    git_bin: itemById(preflight, "git")?.path ?? "",

    // The mobile toolchain and the three host-executed CLIs, all OMITTED
    // rather than sent empty when this computer does not have them. The runner
    // refuses an empty path and treats an absent field as "this computer
    // cannot do that part" — a state it reports itself, instead of exec'ing ""
    // inside a call minutes later.
    ...(xcrun?.status === "ok" && xcrun.path ? { xcrun_bin: xcrun.path } : {}),
    ...(adb?.path && adb.status !== "missing" ? { adb_bin: adb.path } : {}),
    ...(emulator !== "" ? { emulator_bin: emulator } : {}),
    ...(appium?.status === "ok" ? { appium_base_url: "http://127.0.0.1:4723" } : {}),
    // The runner starts the hub on that address when an Appium call needs one
    // and stops it once idle; nothing here runs a hub of its own.
    ...(appium?.status === "ok" && appium.path ? { appium_bin: appium.path } : {}),
    // Claude Code too: a member who works with their own API keys alone has
    // none, and claude.run answers not_ready for them.
    ...(claude?.status === "ok" && claude.path ? { claude_bin: claude.path } : {}),
    ...(cursorAgent?.status === "ok" && cursorAgent.path ? { cursor_agent_bin: cursorAgent.path } : {}),
    ...(opencode?.status === "ok" && opencode.path ? { opencode_bin: opencode.path } : {}),

    // The executor, which the runner starts and hands the providers to. Its
    // data directory travels with it; without an executor neither is sent and
    // agent.run answers not_ready.
    ...(executorBin !== "" ? { executor_bin: executorBin } : {}),
    ...(executorBin !== "" ? { executor_data_dir: inputs.executorDataDir } : {}),
    ...(providerList.length > 0 ? { providers: providerList } : {}),
    ...(embeddings !== "" ? { embeddings_base_url: embeddings } : {}),

    reconnect_max_backoff: "30s",
  })}\n`;
}

/**
 * A preflight update, as one line on the runner's still-open stdin.
 *
 * The runner serves the environment report over the tunnel but does not
 * PRODUCE it: probing lives in `services/detect.ts`, the one place that knows
 * how this computer's tools are found. Pushed after every re-detect so the
 * control plane's first `preflight.report` after Connect is answered with what
 * was just checked, not "not yet".
 */
export function preflightMessage(report: PreflightReport): string {
  return `${JSON.stringify({ type: "preflight", report })}\n`;
}
