import { chmodSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { PreflightId, PreflightItem, PreflightReport } from "../../ipc/types.js";

/**
 * The preflight, driven against real processes.
 *
 * `google-oauth.test.ts` set the standard this follows: where the thing under
 * test is a process or a socket, the test drives one. Every case below spawns
 * an actual fake `claude` — a shell script that answers `--version`, `auth
 * status --json` and `-p` the way a real CLI does. A mocked `execFile` would
 * assert that this file's `switch` works and nothing about the thing it is
 * written against.
 *
 * The Claude-plan cases are the reason this file exists. A free Claude account
 * installs the CLI, passes every "is it there" check, and fails several minutes
 * into a run with a message about a model. Those cases are exercised here from
 * the CLI's own observable output, so a change to how they are classified fails
 * a test rather than a user's first task.
 *
 * LM Studio's own two items were tested here once. They are gone along with
 * `probeLmStudio()` — embeddings are the bundled `embedder` child now, started
 * unconditionally rather than detected, so there is nothing left in this file
 * for a preflight test to assert about them.
 */

// The fake app bundle: `binDir()` reads `app.getAppPath()`, and a temporary one
// with a `bin/agent-server` inside makes that item deterministic — on CI the
// real `bin/` is empty, and a report whose readiness depended on it would
// assert something different there than here.
const paths = vi.hoisted(() => {
  const root = `${process.env.TMPDIR ?? "/tmp"}/tt-preflight-${process.pid}`.replace(/\/+/g, "/");
  return { appPath: root, userData: `${root}/userData` };
});

vi.mock("electron", () => ({
  app: { getAppPath: () => paths.appPath, getPath: () => paths.userData, isPackaged: false },
}));

// The developer's own shell profile is not part of what this suite tests: a
// login PATH that happens to hold an `appium` or a `claude` would change the
// reports below from one machine to the next.
vi.mock("./login-env.js", () => ({
  loginShellPath: () => Promise.resolve([]),
  knownLoginShellPath: () => [],
}));

const {
  androidSdkDefaultRoot,
  androidToolPath,
  classifyAuthStatus,
  fallbackDirs,
  firstBlocker,
  gitRemediation,
  preflight,
  preflightReady,
  probePostgres,
} = await import("./detect.js");

// --- the fake CLI ----------------------------------------------------------

interface FakeClaude {
  version?: string;
  /** What `claude auth status --json` prints on stdout, and its exit code. */
  auth?: { stdout: string; code: number };
  /** What `claude -p …` prints, and its exit code. */
  print?: { stdout: string; code: number };
}

let scripts = 0;

/**
 * Write a shell script that answers like the CLI. It is spawned for real, so
 * what is being tested is the whole path — PATH resolution, argv, exit codes,
 * stdout parsing — rather than a stubbed return value.
 */
function fakeClaude(spec: FakeClaude): string {
  scripts += 1;
  const dir = path.join(paths.appPath, "fakes", String(scripts));
  mkdirSync(dir, { recursive: true });
  const file = path.join(dir, "claude");

  const auth = spec.auth ?? { stdout: "", code: 1 };
  const print = spec.print ?? { stdout: "", code: 1 };
  const script = [
    "#!/bin/sh",
    'case "$1" in',
    `  --version) echo '${spec.version ?? "2.1.220"} (Claude Code)'; exit 0;;`,
    `  auth) printf '%s' '${auth.stdout}'; exit ${auth.code};;`,
    `  -p|--print) printf '%s\\n' '${print.stdout}'; exit ${print.code};;`,
    "esac",
    "exit 1",
    "",
  ].join("\n");
  writeFileSync(file, script);
  chmodSync(file, 0o755);
  return file;
}

function item(report: PreflightReport, id: PreflightId): PreflightItem {
  const found = report.items.find((i) => i.id === id);
  if (!found) throw new Error(`no ${id} item in the report`);
  return found;
}

async function run(claudeBin: string): Promise<PreflightReport> {
  return preflight({ overrides: { claudeBin } });
}

beforeAll(() => {
  // A `bin/agent-server` so that item passes; the real one is not in git.
  mkdirSync(path.join(paths.appPath, "bin"), { recursive: true });
  const server = path.join(paths.appPath, "bin", "agent-server");
  writeFileSync(server, "#!/bin/sh\nexit 0\n");
  chmodSync(server, 0o755);
});

// --- the account, from the CLI's own answer --------------------------------

/**
 * `classifyAuthStatus` on its own, because the mapping is the part that has to
 * be exactly right and there are more cases than it is worth spawning a process
 * for. The end-to-end path through a real process is exercised below.
 */
describe("classifyAuthStatus", () => {
  it("passes a Claude account on a plan that includes Claude Code", () => {
    for (const tier of ["pro", "max", "team", "enterprise"]) {
      const got = classifyAuthStatus({ loggedIn: true, authMethod: "claude.ai", subscriptionType: tier });
      expect(got.status, `${tier} should be usable`).toBe("ok");
    }
  });

  /**
   * The signal, and the reason this check can exist at all. The CLI maps an
   * account's raw type through a fixed table — claude_max, claude_pro,
   * claude_team, claude_enterprise — and anything else, a free account
   * included, falls out as null. So "signed into claude.ai with a null
   * subscriptionType" is the observable for "this account cannot run Claude
   * Code".
   */
  it("reads a null subscription on a claude.ai account as a plan problem, not a sign-in problem", () => {
    const got = classifyAuthStatus({ loggedIn: true, authMethod: "claude.ai", subscriptionType: null });
    expect(got.status).toBe("unusable");
    expect(got.remediation).toMatch(/upgrade/i);
    // The two failures must not be confused: telling somebody who IS signed in
    // to sign in is the confusion this whole item exists to remove.
    expect(got.remediation).not.toMatch(/sign in to your claude account in terminal/i);
    expect(got.command).toBeUndefined();
  });

  it("says the same for an explicitly free tier", () => {
    const got = classifyAuthStatus({ loggedIn: true, authMethod: "claude.ai", subscriptionType: "free" });
    expect(got.status).toBe("unusable");
    expect(got.detail).toMatch(/free account/i);
  });

  it("reports a signed-out CLI as a sign-in problem with the command that fixes it", () => {
    const got = classifyAuthStatus({ loggedIn: false, authMethod: "none" });
    expect(got.status).toBe("missing");
    expect(got.command).toBe("claude auth login");
  });

  /**
   * Console keys, Bedrock and Vertex bill through an API account rather than a
   * Claude plan, so `subscriptionType` is absent by design. Reading its absence
   * as "no plan" would report every API-key user as unable to run Claude Code —
   * the exact false negative this check must not produce.
   */
  it("does not ask about a plan when the account is not a Claude subscription", () => {
    for (const method of ["console", "apiKey", "bedrock", "vertex"]) {
      const got = classifyAuthStatus({ loggedIn: true, authMethod: method });
      expect(got.status, `${method} should be usable`).toBe("ok");
    }
  });

  /**
   * A tier this build has never heard of is almost certainly a new paid one.
   * Telling a paying customer their plan does not include Claude Code is a much
   * worse error than letting an unknown value through — the run itself will say
   * so, plainly, if it turns out not to qualify.
   */
  it("lets an unrecognised tier through rather than guessing against the user", () => {
    const got = classifyAuthStatus({ loggedIn: true, authMethod: "claude.ai", subscriptionType: "claude_ultra_2027" });
    expect(got.status).toBe("ok");
    expect(got.detail).toMatch(/unrecognised plan/i);
  });

  /**
   * ABSENT is not NULL, and the difference is the whole safety of this check.
   *
   * An explicit `null` is the CLI answering: it maps paid tiers through a fixed
   * table and everything else falls out as null, so null means "no Claude Code
   * plan on this account". An absent field is the CLI not answering — a release
   * that renamed the key, or one that emits it in another shape. Those used to
   * be folded together, which meant a single CLI rename would tell every paying
   * customer their plan does not qualify: unactionable, and indistinguishable
   * from the product being broken.
   */
  it("treats an absent subscription as 'could not tell', never as a plan problem", () => {
    const got = classifyAuthStatus({ loggedIn: true, authMethod: "claude.ai", email: "a@b.c" });
    expect(got.status).toBe("ok");
    expect(got.detail).toMatch(/did not report a subscription type/i);
    // Specifically NOT the confident refusal.
    expect(got.remediation).toBeUndefined();
    expect(got.detail).not.toMatch(/does not include Claude Code/i);
  });

  it("treats a wrong-typed subscription as 'could not tell' too", () => {
    for (const weird of [{ name: "max" }, 42, ["max"], true]) {
      const got = classifyAuthStatus({ loggedIn: true, authMethod: "claude.ai", subscriptionType: weird });
      expect(got.status, `${JSON.stringify(weird)} should not be a plan refusal`).toBe("ok");
      expect(got.detail).toMatch(/shape this app does not know/i);
    }
  });

  /**
   * The asymmetry, stated as one assertion so it cannot drift: of the three
   * "we do not recognise this" shapes, only an explicit null refuses.
   */
  it("refuses on an explicit null and on nothing else it does not recognise", () => {
    const claudeAi = (subscriptionType: unknown): unknown =>
      classifyAuthStatus({ loggedIn: true, authMethod: "claude.ai", subscriptionType }).status;
    expect(claudeAi(null)).toBe("unusable");
    expect(claudeAi(undefined)).toBe("ok");
    expect(claudeAi("something_new")).toBe("ok");
    expect(claudeAi({})).toBe("ok");
  });
});

// Each case runs the whole preflight sweep against real child processes, and
// one case runs it twice: on a loaded machine that outlasts vitest's 5 s default
// without anything being wrong.
describe("preflight: the Claude account, end to end", { timeout: 30_000 }, () => {
  it("passes a real CLI that reports a Max subscription", async () => {
    const bin = fakeClaude({
      auth: {
        stdout: '{"loggedIn":true,"authMethod":"claude.ai","email":"a@b.c","subscriptionType":"max"}',
        code: 0,
      },
    });
    const report = await run(bin);
    expect(item(report, "claude").status).toBe("ok");
    expect(item(report, "claude").version).toBe("2.1.220");
    expect(item(report, "claude-account").status).toBe("ok");
  });

  it("distinguishes not signed in from a plan without Claude Code", async () => {
    const signedOut = await run(
      fakeClaude({ auth: { stdout: '{"loggedIn":false,"authMethod":"none","apiProvider":"firstParty"}', code: 1 } }),
    );
    const freePlan = await run(
      fakeClaude({
        auth: {
          stdout: '{"loggedIn":true,"authMethod":"claude.ai","email":"a@b.c","subscriptionType":null}',
          code: 0,
        },
      }),
    );

    const out = item(signedOut, "claude-account");
    const free = item(freePlan, "claude-account");

    expect(out.status).toBe("missing");
    expect(free.status).toBe("unusable");
    // Two different messages for two different problems, which is the point.
    expect(out.remediation).not.toBe(free.remediation);
    expect(out.remediation).toMatch(/sign in/i);
    expect(free.remediation).toMatch(/pro, max, team or enterprise/i);
  });

  /**
   * A CLI too old to have `auth status --json` prints a usage error instead of
   * JSON. The fallback runs the smallest real session and reads the CLI's own
   * sentence — which is quoted rather than replaced, because it is more
   * accurate than anything this code could invent.
   */
  it("falls back to a one-turn session when the CLI cannot be asked directly", async () => {
    const report = await run(
      fakeClaude({
        version: "2.0.5",
        auth: { stdout: "", code: 1 },
        print: { stdout: "Not logged in · Please run /login", code: 1 },
      }),
    );
    const account = item(report, "claude-account");
    expect(account.status).toBe("missing");
    expect(account.detail).toMatch(/Not logged in/);
  });

  /**
   * The honest weaker answer. When the CLI is too old to be asked and the run
   * failed for a reason that names neither cause, the message says exactly that
   * instead of picking one and being confidently wrong.
   */
  it("says it could not tell rather than guessing, when nothing identifies the cause", async () => {
    const report = await run(
      fakeClaude({
        version: "2.0.5",
        auth: { stdout: "", code: 1 },
        print: { stdout: "Error: connect ETIMEDOUT", code: 1 },
      }),
    );
    const account = item(report, "claude-account");
    expect(account.status).toBe("unusable");
    expect(account.detail).toMatch(/ETIMEDOUT/);
    expect(account.remediation).toMatch(/Update the Claude Code CLI/i);
    // Neither of the two confident messages.
    expect(account.remediation).not.toMatch(/claude\.ai\/upgrade/);
  });

  it("does not ask about the account when the CLI itself is unusable", async () => {
    const report = await run(fakeClaude({ version: "1.4.0" }));
    const cli = item(report, "claude");
    expect(cli.status).toBe("unusable");
    expect(cli.detail).toMatch(/older than/);
    // One problem, one blocker. A second failure for the same cause would put
    // two things in front of a user who has one thing to fix.
    expect(item(report, "claude-account").detail).toMatch(/Not checked/);
  });

  it("reports a CLI that is not installed as missing, with the install command", async () => {
    const report = await preflight({ overrides: { claudeBin: "/nowhere/claude" } });
    const cli = item(report, "claude");
    expect(cli.status).toBe("missing");
    expect(cli.command).toBe("npm install -g @anthropic-ai/claude-code");
  });
});

// --- readiness and the blocker ---------------------------------------------

describe("preflight: what blocks the start", () => {
  /**
   * `ready` is asserted against the FACTS of the machine this ran on, not
   * against its own definition.
   *
   * It used to read `expect(report.ready).toBe(requiredFailures.length === 0)`,
   * which is `preflight()`'s implementation copied into the test: `ready` is
   * computed as exactly that expression, so the assertion held for every
   * possible report and could not fail. A test that cannot fail is worse than
   * no test, because the suite counts it.
   */
  it("is ready when every required item passed, and says which one did not when it is not", async () => {
    const report = await run(
      fakeClaude({ auth: { stdout: '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max"}', code: 0 } }),
    );

    const optional = report.items.filter((i) => !i.required).map((i) => i.id);
    expect(optional).toContain("chrome");
    // "xcode-clt" only appears on the platform the test actually ran on.
    if (process.platform === "darwin") expect(optional).toContain("xcode-clt");
    else expect(optional).not.toContain("xcode-clt");

    // Everything required did pass on this fixture, so `ready` must be true —
    // a constant, not an expression derived from the report.
    expect(report.items.filter((i) => i.required && i.status !== "ok")).toEqual([]);
    expect(report.ready).toBe(true);
    expect(firstBlocker(report)).toBeUndefined();

    // And the other direction, which the old assertion also could not see: one
    // required item failing flips `ready` and produces a blocker naming it.
    const withFailure: PreflightReport = {
      ...report,
      items: report.items.map((i) => (i.id === "git" ? { ...i, status: "missing" as const } : i)),
    };
    expect(preflightReady(withFailure)).toBe(false);
    expect(firstBlocker(withFailure)?.id).toBe("git");
  });

  /**
   * The start is refused while a required item fails, and the refusal NAMES the
   * item and carries its remediation.
   */
  it("produces a blocker that names the failing item and carries its remediation", async () => {
    const report = await run(
      fakeClaude({ auth: { stdout: '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max"}', code: 0 } }),
    );
    const gitMissing: PreflightReport = {
      ...report,
      items: report.items.map((i) =>
        i.id === "git"
          ? { ...i, status: "missing" as const, remediation: "Install the Xcode command line tools.", command: "xcode-select --install" }
          : i,
      ),
    };
    const blocker = firstBlocker(gitMissing);
    expect(blocker?.id).toBe("git");
    expect(blocker?.title).toMatch(/git/i);
    expect(blocker?.remediation).toMatch(/xcode/i);
  });

  /**
   * Agents can run on Cursor, Antigravity, OpenCode or an API key, so a Claude
   * account without Claude Code is reported and never stops the start.
   */
  it("reports a Claude plan without Claude Code but does not block on it", async () => {
    const report = await run(
      fakeClaude({
        auth: { stdout: '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":null}', code: 0 },
      }),
    );
    const account = report.items.find((i) => i.id === "claude-account");
    expect(account?.status).toBe("unusable");
    expect(account?.required).toBe(false);
    expect(report.ready).toBe(true);
    expect(firstBlocker(report)).toBeUndefined();
  });

  it("never blocks on an optional item", async () => {
    const report = await run(
      fakeClaude({ auth: { stdout: '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max"}', code: 0 } }),
    );
    // Force every optional item to fail, and readiness must not move.
    const optionalFailures: PreflightReport = {
      ...report,
      items: report.items.map((i) => (i.required ? i : { ...i, status: "missing" as const })),
    };
    expect(firstBlocker(optionalFailures)).toBeUndefined();
  });

  it("orders the list so a person can work down it", async () => {
    const report = await run(
      fakeClaude({ auth: { stdout: '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max"}', code: 0 } }),
    );
    // The CLI has to be usable before its account can be asked about, so the
    // two must appear in that order or the UI would show a consequence above
    // its cause.
    const ids = report.items.map((i) => i.id);
    expect(ids.indexOf("claude")).toBeLessThan(ids.indexOf("claude-account"));
  });
});

// --- the mobile toolchain ---------------------------------------------------

/**
 * Write a shell script that answers like Appium. `driver list --installed`
 * writes its whole answer to STDERR, which is not an accident of this fake —
 * it is what the real CLI does, and reading the wrong stream reported both
 * drivers missing on a machine that had just loaded them by name.
 */
function fakeAppium(drivers: string[]): string {
  scripts += 1;
  const dir = path.join(paths.appPath, "fakes", `appium-${scripts}`);
  mkdirSync(dir, { recursive: true });
  const file = path.join(dir, "appium");
  const listed = drivers.map((d) => `- ${d}@4.0.0 [installed (npm)]`).join("\\n");
  const script = [
    "#!/bin/sh",
    'case "$1" in',
    "  --version) echo '2.11.3'; exit 0;;",
    `  driver) printf '%b\\n' '${listed}' >&2; exit 0;;`,
    "esac",
    "exit 1",
    "",
  ].join("\n");
  writeFileSync(file, script);
  chmodSync(file, 0o755);
  return file;
}

const signedIn = { stdout: '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max"}', code: 0 };

describe("preflight: the mobile toolchain", () => {
  /**
   * The whole reason these items exist. A QA task that reaches a Mac with no
   * Appium fails several minutes in, inside a session, with a connection error
   * about a port — and "npm install -g appium" could have been said before
   * anybody pressed Connect. It is reported, and it never blocks: this app
   * cannot know whether this member does mobile work.
   */
  it("reports a missing Appium as an optional item with the command that fixes it", async () => {
    const report = await preflight({
      overrides: { claudeBin: fakeClaude({ auth: signedIn }), appiumBin: "/nonexistent/appium" },
    });

    const appium = item(report, "appium");
    expect(appium.required).toBe(false);
    expect(appium.status).toBe("missing");
    expect(appium.command).toBe("npm install -g appium");
    expect(report.ready).toBe(true);
    expect(firstBlocker(report)).toBeUndefined();

    // One problem, one row. Three missing rows about a Mac with no Appium at
    // all would be three ways of saying the same thing, and the drivers cannot
    // be installed before the thing they are drivers for.
    expect(report.items.map((i) => i.id)).not.toContain("appium-xcuitest");
    expect(report.items.map((i) => i.id)).not.toContain("appium-uiautomator2");
  });

  /**
   * A driver is a separate item because it is a separate situation with its own
   * one-line fix — and it is the one that otherwise fails at session creation
   * rather than at install time.
   */
  it("reports each driver separately once Appium itself is there", async () => {
    const report = await preflight({
      overrides: { claudeBin: fakeClaude({ auth: signedIn }), appiumBin: fakeAppium(["uiautomator2"]) },
    });

    const appium = item(report, "appium");
    expect(appium.status).toBe("ok");
    expect(appium.version).toBe("2.11.3");
    // The hub's address is stated before it matters, so nobody has to find out
    // which port this app uses by reading a failure.
    expect(appium.detail).toContain("127.0.0.1:4723");

    expect(item(report, "appium-uiautomator2").status).toBe("ok");

    if (process.platform === "darwin") {
      const missing = item(report, "appium-xcuitest");
      expect(missing.status).toBe("missing");
      expect(missing.required).toBe(false);
      expect(missing.command).toBe("appium driver install xcuitest");
    }

    // Still optional, still not a blocker.
    expect(report.ready).toBe(true);
  });

  it("always reports the Android SDK, and never blocks on it", async () => {
    const report = await preflight({
      overrides: { claudeBin: fakeClaude({ auth: signedIn }), appiumBin: "/nonexistent/appium" },
    });
    const sdk = item(report, "android-sdk");
    expect(sdk.required).toBe(false);
    expect(["ok", "missing", "unusable"]).toContain(sdk.status);
    expect(firstBlocker(report)).toBeUndefined();
  });
});

// --- Windows and Linux: right place, right remediation ----------------------

/**
 * Runs `fn` as if on `platform`, until it has finished — including, for an
 * async `fn`, everything after its first await. `preflight()` awaits the login
 * shell before it probes, so restoring the platform when the promise is merely
 * returned would run every probe on the real one.
 */
function withPlatform<T>(platform: NodeJS.Platform, fn: () => T): T {
  const original = Object.getOwnPropertyDescriptor(process, "platform");
  const restore = (): void => {
    if (original) Object.defineProperty(process, "platform", original);
  };
  Object.defineProperty(process, "platform", { value: platform, configurable: true });
  let result: T;
  try {
    result = fn();
  } catch (err) {
    restore();
    throw err;
  }
  if (result instanceof Promise) return result.finally(restore) as T;
  restore();
  return result;
}

describe("git remediation, by platform", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("points Windows at winget, not xcode-select", () => {
    const got = withPlatform("win32", gitRemediation);
    expect(got.command).toBe("winget install --id Git.Git -e --source winget");
    expect(got.remediation).not.toMatch(/xcode/i);
  });

  it("gives Linux a sentence and no non-functional command", () => {
    const got = withPlatform("linux", gitRemediation);
    expect(got.remediation).not.toMatch(/xcode/i);
    expect(got.command).toBeUndefined();
  });

  it("keeps the Xcode CLT remediation on macOS", () => {
    const got = withPlatform("darwin", gitRemediation);
    expect(got.command).toBe("xcode-select --install");
  });
});

describe("preflight: xcode-clt is macOS-only", () => {
  it("does not appear on Windows or Linux", async () => {
    for (const platform of ["win32", "linux"] as const) {
      const report = await withPlatform(platform, () => run(fakeClaude({ auth: signedIn })));
      expect(report.items.map((i) => i.id)).not.toContain("xcode-clt");
    }
  });
});

describe("preflight: iOS tooling is macOS-only", () => {
  it("has no xcuitest row and promises no iOS simulator on Windows or Linux", async () => {
    for (const platform of ["win32", "linux"] as const) {
      const withAppium = await withPlatform(platform, () =>
        preflight({ overrides: { claudeBin: fakeClaude({ auth: signedIn }), appiumBin: fakeAppium(["uiautomator2"]) } }),
      );
      expect(withAppium.items.map((i) => i.id)).not.toContain("appium-xcuitest");

      const noAppium = await withPlatform(platform, () =>
        preflight({ overrides: { claudeBin: fakeClaude({ auth: signedIn }), appiumBin: "/nonexistent/appium" } }),
      );
      expect(item(noAppium, "appium").detail).not.toMatch(/iOS|Mac/);
    }
  });
});

describe("Android SDK default install root, by platform", () => {
  it("uses LOCALAPPDATA on Windows", () => {
    vi.stubEnv("LOCALAPPDATA", "C:\\Users\\me\\AppData\\Local");
    const root = withPlatform("win32", androidSdkDefaultRoot);
    expect(root).toBe(path.join("C:\\Users\\me\\AppData\\Local", "Android", "Sdk"));
  });

  it("uses ~/Android/Sdk on Linux", () => {
    const root = withPlatform("linux", androidSdkDefaultRoot);
    expect(root.endsWith(path.join("Android", "Sdk"))).toBe(true);
    expect(root).not.toContain("Library");
  });

  it("uses ~/Library/Android/sdk on macOS", () => {
    const root = withPlatform("darwin", androidSdkDefaultRoot);
    expect(root.endsWith(path.join("Library", "Android", "sdk"))).toBe(true);
  });
});

describe("Android SDK tool paths, by platform", () => {
  /**
   * The SDK's binaries are `adb.exe` and `emulator.exe` on Windows. Looking for
   * the bare name there found neither, on every Windows machine.
   */
  it("carries .exe on Windows and nothing elsewhere", () => {
    expect(withPlatform("win32", () => androidToolPath("SDK", "platform-tools", "adb"))).toBe(
      path.join("SDK", "platform-tools", "adb.exe"),
    );
    expect(withPlatform("win32", () => androidToolPath("SDK", "emulator", "emulator"))).toBe(
      path.join("SDK", "emulator", "emulator.exe"),
    );
    for (const platform of ["darwin", "linux"] as const) {
      expect(withPlatform(platform, () => androidToolPath("SDK", "platform-tools", "adb"))).toBe(
        path.join("SDK", "platform-tools", "adb"),
      );
    }
  });
});

describe("fallback search directories, by platform", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("looks where Git for Windows and per-user installers put things, under %APPDATA%-style roots", () => {
    vi.stubEnv("ProgramFiles", "PF");
    vi.stubEnv("ProgramFiles(x86)", "PF86");
    vi.stubEnv("LOCALAPPDATA", "LAD");
    const dirs = withPlatform("win32", () => fallbackDirs("HOME")).map((d) => d.dir);
    expect(dirs).toEqual(
      expect.arrayContaining([
        path.join("PF", "Git", "cmd"),
        path.join("PF86", "Git", "cmd"),
        path.join("LAD", "Programs", "Git", "cmd"),
        path.join("PF", "nodejs"),
        path.join("LAD", "pnpm"),
      ]),
    );
  });

  it("covers the version managers and Linuxbrew a GUI session on Linux knows nothing about", () => {
    vi.stubEnv("PNPM_HOME", "/home/me/pnpm-home");
    vi.stubEnv("XDG_DATA_HOME", "");
    const dirs = withPlatform("linux", () => fallbackDirs("/home/me")).map((d) => d.dir);
    expect(dirs).toEqual(
      expect.arrayContaining([
        "/home/me/.volta/bin",
        "/home/me/pnpm-home",
        "/home/me/.fnm",
        "/home/linuxbrew/.linuxbrew/bin",
        "/usr/local/bin",
      ]),
    );
    expect(dirs.some((d) => d.includes("Program Files"))).toBe(false);
    // XDG_DATA_HOME="" means the default, not a directory relative to the cwd.
    expect(dirs).toContain("/home/me/.local/share/fnm/aliases/default/bin");
    expect(dirs.every((d) => d.startsWith("/"))).toBe(true);
  });
});

// --- the backend and its database ------------------------------------------

describe("the two items the user cannot install", () => {
  /**
   * A copy of the app with no `agent-server` in it is a broken build, not a
   * missing dependency — so the remediation must not offer a command that would
   * not help a user who installed a .app.
   */
  it("requires the bundled server and points a developer at the build", async () => {
    const report = await run(
      fakeClaude({ auth: { stdout: '{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max"}', code: 0 } }),
    );
    const server = item(report, "agent-server");
    expect(server.required).toBe(true);
    expect(server.status).toBe("ok");
    expect(server.path?.endsWith("/bin/agent-server")).toBe(true);
  });

  /**
   * Postgres downloads itself on the backend's first start, so this item must
   * never be able to block: it says the download is coming and stays `ok`.
   */
  it("never blocks on a Postgres that has not been downloaded yet", () => {
    const pg = probePostgres();
    expect(pg.required).toBe(false);
    expect(pg.status).toBe("ok");
    expect(pg.remediation).toBeUndefined();
  });
});

// --- the two halves, and what is remembered between launches ---------------

const { startPreflight } = await import("./detect.js");
const { PreflightCache } = await import("./preflight-cache.js");

/**
 * A fake `claude` that also records every time it is asked anything, so a
 * test can say not just what the report contains but how many processes it
 * took to get there.
 */
function countingClaude(version = "2.1.220"): { bin: string; calls: () => string[] } {
  scripts += 1;
  const dir = path.join(paths.appPath, "fakes", `counting-${scripts}`);
  mkdirSync(dir, { recursive: true });
  const bin = path.join(dir, "claude");
  const log = path.join(dir, "calls.log");
  writeFileSync(log, "");
  writeFileSync(
    bin,
    [
      "#!/bin/sh",
      `echo "$1" >> '${log}'`,
      'case "$1" in',
      `  --version) echo '${version} (Claude Code)'; exit 0;;`,
      `  auth) printf '%s' '${signedIn.stdout}'; exit 0;;`,
      "esac",
      "exit 1",
      "",
    ].join("\n"),
  );
  chmodSync(bin, 0o755);
  return { bin, calls: () => readFileSync(log, "utf8").split("\n").filter(Boolean) };
}

function cacheFile(): string {
  scripts += 1;
  return path.join(paths.appPath, `preflight-cache-${scripts}.json`);
}

describe("preflight: the gating half", { timeout: 30_000 }, () => {
  it("decides what the backend needs without running a single probe", async () => {
    const claude = countingClaude();
    const run = startPreflight({ overrides: { claudeBin: claude.bin, appiumBin: fakeAppium(["uiautomator2"]) } });
    const gating = await run.gating;

    // The two required items, fully answered.
    expect(item(gating, "agent-server").status).toBe("ok");
    expect(item(gating, "git").required).toBe(true);
    expect(gating.ready).toBe(true);
    expect(firstBlocker(gating)).toBeUndefined();

    // Found, provisionally usable, and not yet asked anything.
    expect(item(gating, "claude").status).toBe("ok");
    expect(item(gating, "claude").path).toBe(claude.bin);
    expect(item(gating, "claude").version).toBeUndefined();
    expect(claude.calls()).toEqual([]);

    // Only a process can answer these, so the gating half leaves them out
    // rather than guessing.
    const ids = gating.items.map((i) => i.id);
    expect(ids).not.toContain("claude-account");
    expect(ids).not.toContain("appium-uiautomator2");
    expect(item(gating, "appium").status).toBe("ok");

    const complete = await run.complete;
    expect(item(complete, "claude").version).toBe("2.1.220");
    expect(item(complete, "claude-account").status).toBe("ok");
    expect(item(complete, "appium-uiautomator2").status).toBe("ok");
    expect(complete.items.map((i) => i.id)).toEqual(
      expect.arrayContaining(["agent-server", "postgres", "git", "claude", "claude-account", "chrome", "appium", "android-sdk"]),
    );
  });

  it("keeps the same order in both halves", async () => {
    const run = startPreflight({ overrides: { claudeBin: countingClaude().bin } });
    const [gating, complete] = await Promise.all([run.gating, run.complete]);
    const order = complete.items.map((i) => i.id);
    expect(gating.items.map((i) => i.id)).toEqual(order.filter((id) => gating.items.some((i) => i.id === id)));
  });

  it("reports a missing required item in the gating half, so the start is refused without waiting", async () => {
    const gating = await startPreflight({ overrides: { claudeBin: countingClaude().bin, gitBin: "/nowhere/git" } }).gating;
    expect(item(gating, "git").status).toBe("missing");
    expect(firstBlocker(gating)?.id).toBe("git");
  });
});

describe("preflight: the version cache", { timeout: 30_000 }, () => {
  it("answers versions from the cache, in both halves, without asking the binary again", async () => {
    const claude = countingClaude();
    const file = cacheFile();
    const cache = new PreflightCache(file);

    await startPreflight({ overrides: { claudeBin: claude.bin }, cache }).complete;
    expect(claude.calls().filter((c) => c === "--version")).toHaveLength(1);
    await cache.flush();

    // A later launch: a new cache object over the same file.
    const later = new PreflightCache(file);
    const run = startPreflight({ overrides: { claudeBin: claude.bin }, cache: later });
    expect(item(await run.gating, "claude").version).toBe("2.1.220");
    expect(item(await run.complete, "claude").version).toBe("2.1.220");
    expect(claude.calls().filter((c) => c === "--version")).toHaveLength(1);
    // The account is never cached: it is the answer most likely to change.
    expect(claude.calls().filter((c) => c === "auth")).toHaveLength(2);
  });

  it("asks again when forced, and when the binary changed", async () => {
    const claude = countingClaude();
    const cache = new PreflightCache(cacheFile());
    await startPreflight({ overrides: { claudeBin: claude.bin }, cache }).complete;

    await startPreflight({ overrides: { claudeBin: claude.bin }, cache, force: true }).complete;
    expect(claude.calls().filter((c) => c === "--version")).toHaveLength(2);

    // An upgrade writes a different file.
    writeFileSync(claude.bin, readFileSync(claude.bin, "utf8").replace("2.1.220", "2.2.0") + "\n");
    const report = await startPreflight({ overrides: { claudeBin: claude.bin }, cache }).complete;
    expect(item(report, "claude").version).toBe("2.2.0");
    expect(claude.calls().filter((c) => c === "--version")).toHaveLength(3);
  });

  /**
   * A forced sweep asks every binary again; when the answer is now a failure,
   * the success remembered before it must not be what the next, unforced
   * sweep shows.
   */
  it("forgets a remembered version once a forced sweep finds the binary failing", async () => {
    scripts += 1;
    const dir = path.join(paths.appPath, "fakes", `git-${scripts}`);
    mkdirSync(dir, { recursive: true });
    const git = path.join(dir, "git");
    const broken = path.join(dir, "broken");
    const log = path.join(dir, "calls.log");
    writeFileSync(log, "");
    writeFileSync(
      git,
      ["#!/bin/sh", `echo "$1" >> '${log}'`, `[ -f '${broken}' ] && exit 1`, "echo 'git version 2.40.0'", ""].join("\n"),
    );
    chmodSync(git, 0o755);
    const versions = () => readFileSync(log, "utf8").split("\n").filter((c) => c === "--version").length;
    const overrides = { claudeBin: countingClaude().bin, gitBin: git };
    const cache = new PreflightCache(cacheFile());

    expect(item(await startPreflight({ overrides, cache }).complete, "git").version).toBe("2.40.0");
    expect(versions()).toBe(1);

    writeFileSync(broken, "");
    const forced = startPreflight({ overrides, cache, force: true });
    expect(item(await forced.gating, "git").version).toBe("2.40.0");
    expect(item(await forced.complete, "git").version).toBeUndefined();
    expect(versions()).toBe(2);

    const next = await startPreflight({ overrides, cache }).complete;
    expect(item(next, "git").version).toBeUndefined();
    expect(versions()).toBe(3);
  });

  /**
   * Remembered, so the next launch starts the backend on the same answer
   * (without CLAUDE_CODE_BIN) instead of a provisional "ok" it would then have
   * to restart over; asked again every sweep, so a fix is seen at once.
   */
  it("starts from a claude it cannot drive but asks it again every time", async () => {
    const claude = countingClaude("1.4.0");
    const cache = new PreflightCache(cacheFile());
    const first = await startPreflight({ overrides: { claudeBin: claude.bin }, cache }).complete;
    expect(item(first, "claude").status).toBe("unusable");
    expect(cache.version(claude.bin)?.fresh).toBe(false);

    const run = startPreflight({ overrides: { claudeBin: claude.bin }, cache });
    expect(item(await run.gating, "claude").status).toBe("unusable");
    expect(item(await run.complete, "claude").status).toBe("unusable");
    expect(claude.calls().filter((c) => c === "--version")).toHaveLength(2);
  });
});
