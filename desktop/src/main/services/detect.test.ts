import { chmodSync, mkdirSync, writeFileSync } from "node:fs";
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

const { androidSdkDefaultRoot, classifyAuthStatus, firstBlocker, gitRemediation, preflight, preflightReady, probePostgres } =
  await import("./detect.js");

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

    const missing = item(report, "appium-xcuitest");
    expect(missing.status).toBe("missing");
    expect(missing.required).toBe(false);
    expect(missing.command).toBe("appium driver install xcuitest");

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

function withPlatform<T>(platform: NodeJS.Platform, fn: () => T): T {
  const original = Object.getOwnPropertyDescriptor(process, "platform");
  Object.defineProperty(process, "platform", { value: platform, configurable: true });
  try {
    return fn();
  } finally {
    if (original) Object.defineProperty(process, "platform", original);
  }
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
