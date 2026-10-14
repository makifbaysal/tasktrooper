import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, statSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { gzipSync } from "node:zlib";
import { describe, expect, it, vi } from "vitest";
import type { ToolchainId } from "../../../ipc/types.js";
import type { Detection, ExecOptions, HostFacts, StepContext, StepProgress, ToolDefinition } from "../types.js";

vi.mock("electron", () => ({ app: { getAppPath: () => "/app", getPath: () => "/tmp/tt-recipes", isPackaged: false } }));
vi.mock("../../services/login-env.js", () => ({
  loginShellPath: () => Promise.resolve([]),
  knownLoginShellPath: () => [],
}));

const { installerFor } = await import("./index.js");
const { packageProgress } = await import("./android-sdk.js");
const { askpassScript } = await import("./brew.js");
const { terminalScript } = await import("./interactive.js");
const { TOOLCHAIN_DEFINITIONS } = await import("../registry.js");
const { InstallError } = await import("../types.js");
const { execLogged } = await import("../process.js");

/**
 * What each strategy actually runs, through a recording step context: the
 * program, its argv, the environment it adds, what it downloads and where.
 * The argv is the contract with the outside tool — and the place a secret or
 * a shell would show up if one ever crept in.
 */

interface Call {
  command: string;
  args: readonly string[];
  env?: Readonly<Record<string, string>>;
  input?: string;
}

interface Answers {
  exit?: number;
  lines?: string[];
  detect?: () => Detection;
  /** What a download writes to its destination; a small script by default. */
  serve?: (url: string, dest: string) => void;
  /** Run commands for real (tar), still recorded. */
  real?: boolean;
}

function context(id: ToolchainId, host: Partial<HostFacts> & Pick<HostFacts, "platform">, answers: Answers = {}) {
  const workDir = mkdtempSync(path.join(os.tmpdir(), "tt-recipe-"));
  const calls: Call[] = [];
  const downloads: { url: string; dest: string; sha256?: string }[] = [];
  const progress: StepProgress[] = [];
  const ctx: StepContext = {
    id,
    label: id,
    signal: new AbortController().signal,
    host: { arch: "x64", home: workDir, env: {}, which: (name) => `/fake/${name.replace(/^\/.*\//, "")}`, ...host },
    workDir,
    exec: (command, args, opts: ExecOptions = {}) => {
      calls.push({ command, args, ...(opts.env ? { env: opts.env } : {}), ...(opts.input ? { input: opts.input } : {}) });
      if (answers.real) return execLogged(command, args, { ...opts, signal: new AbortController().signal });
      for (const line of answers.lines ?? []) opts.onLine?.(line);
      return Promise.resolve(answers.exit ?? 0);
    },
    download: (url, dest, opts = {}) => {
      downloads.push({ url, dest, ...(opts.sha256 ? { sha256: opts.sha256 } : {}) });
      if (answers.serve) answers.serve(url, dest);
      else writeFileSync(dest, "#!/bin/sh\n");
      return Promise.resolve();
    },
    progress: (p) => progress.push(p),
    log: () => undefined,
    detect: () => Promise.resolve(answers.detect?.() ?? { status: "installed" }),
    sleep: () => Promise.resolve(),
  };
  return { ctx, calls, downloads, progress, workDir };
}

function run(id: ToolchainId, c: ReturnType<typeof context>): Promise<void> {
  const installer = installerFor(TOOLCHAIN_DEFINITIONS[id] as ToolDefinition, c.ctx.host);
  if (!installer) throw new Error(`${id} does not apply`);
  return installer.run(c.ctx);
}

const linuxWith = (...present: string[]) => (name: string) =>
  present.includes(path.basename(name)) ? `/usr/bin/${path.basename(name)}` : null;

describe("brew", () => {
  it("installs a cask unattended, with a native password prompt naming it and nothing secret on argv", async () => {
    const c = context("docker", { platform: "darwin", which: (n) => (n === "/opt/homebrew/bin/brew" ? n : null) });
    await run("docker", c);
    expect(c.calls).toHaveLength(1);
    const call = c.calls[0];
    expect(call?.command).toBe("/opt/homebrew/bin/brew");
    expect(call?.args).toEqual(["install", "--cask", "docker-desktop"]);
    expect(call?.env).toMatchObject({ NONINTERACTIVE: "1", HOMEBREW_NO_ENV_HINTS: "1", HOMEBREW_NO_INSTALL_CLEANUP: "1" });
    const askpass = call?.env?.SUDO_ASKPASS ?? "";
    expect(askpass.startsWith(c.workDir)).toBe(true);
    expect(statSync(askpass).mode & 0o777).toBe(0o700);
    const script = readFileSync(askpass, "utf8");
    expect(script).toContain("Docker (Homebrew cask docker-desktop)");
    expect(script).toContain("with hidden answer");
  });

  it("gives a formula no password prompt at all", async () => {
    const c = context("node", { platform: "darwin" });
    await run("node", c);
    expect(c.calls[0]?.env?.SUDO_ASKPASS).toBeUndefined();
  });

  it("makes Cancel in the password prompt a failure sudo stops on", () => {
    const fakeBin = mkdtempSync(path.join(os.tmpdir(), "tt-askpass-"));
    const script = path.join(fakeBin, "askpass.sh");
    const body = askpassScript("Docker", "docker-desktop").replace("/usr/bin/osascript", path.join(fakeBin, "osascript"));
    writeFileSync(script, body, { mode: 0o700 });
    writeFileSync(path.join(fakeBin, "osascript"), "#!/bin/sh\necho 'execution error: User canceled. (-128)' >&2\nexit 1\n", { mode: 0o755 });
    expect(() => execFileSync("/bin/sh", [script], { stdio: "pipe" })).toThrow();
    writeFileSync(path.join(fakeBin, "osascript"), "#!/bin/sh\necho 'hunter2'\n", { mode: 0o755 });
    expect(execFileSync("/bin/sh", [script], { encoding: "utf8" })).toBe("hunter2\n");
  });

  it("keeps anything but plain words out of the prompt", () => {
    expect(askpassScript(`Evil" & do shell script "rm -rf ~`, "x'y")).not.toMatch(/rm -rf ~|x'y|" &/);
  });

  it("fails with brew's last line when it exits non-zero", async () => {
    const c = context("node", { platform: "darwin" }, { exit: 1, lines: ['Error: No available formula with the name "node".'] });
    await expect(run("node", c)).rejects.toThrow('brew install node exited with code 1: Error: No available formula with the name "node".');
  });
});

describe("linux packages", () => {
  it("asks pkexec for apt-get itself, so the polkit prompt names the package manager", async () => {
    const c = context("git", { platform: "linux", which: linuxWith("pkexec", "apt-get") });
    await run("git", c);
    expect(c.calls).toEqual([
      {
        command: "/usr/bin/pkexec",
        args: ["/usr/bin/apt-get", "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold", "install", "-y", "git"],
      },
    ]);
  });

  it("uses dnf where there is no apt-get", async () => {
    const c = context("docker", { platform: "linux", which: linuxWith("pkexec", "dnf") });
    await run("docker", c);
    expect(c.calls[0]?.args).toEqual(["/usr/bin/dnf", "install", "-y", "moby-engine"]);
  });

  it("says the password prompt was dismissed rather than quoting an exit code", async () => {
    const c = context("git", { platform: "linux", which: linuxWith("pkexec", "apt-get") }, { exit: 126 });
    await expect(run("git", c)).rejects.toThrow("The administrator password prompt was dismissed.");
  });

  it("installs Chromium from the snap store, never a downloaded package file", async () => {
    const c = context("chrome", { platform: "linux", which: linuxWith("pkexec", "snap", "apt-get") });
    await run("chrome", c);
    expect(c.downloads).toEqual([]);
    expect(c.calls).toEqual([{ command: "/usr/bin/pkexec", args: ["/usr/bin/snap", "install", "chromium"] }]);
  });
});

describe("the vendor's own installer script", () => {
  it("is saved to a file and run by the interpreter it names — never piped", async () => {
    const c = context("claude-code", { platform: "linux" });
    await run("claude-code", c);
    const file = path.join(c.workDir, "claude-code-install.sh");
    expect(c.downloads).toEqual([{ url: "https://claude.ai/install.sh", dest: file }]);
    expect(c.calls).toEqual([{ command: "/bin/bash", args: [file] }]);
  });

  it("runs PowerShell with a file on Windows", async () => {
    const c = context("claude-code", { platform: "win32" });
    await run("claude-code", c);
    expect(c.calls[0]?.args).toEqual(["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path.join(c.workDir, "claude-code-install.ps1")]);
  });
});

describe("a pinned release binary", () => {
  it("goes to the person's own bin directory, checked against its digest, executable", async () => {
    const c = context("osv-scanner", { platform: "linux" });
    await run("osv-scanner", c);
    const dest = path.join(c.workDir, ".local", "bin", "osv-scanner");
    expect(c.downloads).toEqual([
      {
        url: "https://github.com/google/osv-scanner/releases/download/v2.7.0/osv-scanner_linux_amd64",
        dest,
        sha256: "160c6253e2922e9e0a6a9771c5c4c40a4b3c6090b34a3cdd5e80c96cc2e4218b",
      },
    ]);
    expect(statSync(dest).mode & 0o111).not.toBe(0);
  });

  it("unpacks a gzipped one (DuckDB) into the bin directory", async () => {
    const program = "#!/bin/sh\necho duckdb\n";
    const c = context("duckdb", { platform: "linux" }, { serve: (_url, dest) => writeFileSync(dest, gzipSync(program)) });
    await run("duckdb", c);
    expect(c.downloads[0]).toMatchObject({
      url: "https://github.com/duckdb/duckdb/releases/download/v1.5.6/duckdb_cli-linux-amd64.gz",
      sha256: "cce2687a38681db644c248198b99ecd86170ff47e324c5f33e50c0042f142839",
    });
    const dest = path.join(c.workDir, ".local", "bin", "duckdb");
    expect(readFileSync(dest, "utf8")).toBe(program);
    expect(statSync(dest).mode & 0o111).not.toBe(0);
  });

  it.skipIf(process.platform === "win32")("takes only the named programs out of a tarball (uv and uvx)", async () => {
    const staging = mkdtempSync(path.join(os.tmpdir(), "tt-uv-"));
    const inner = path.join(staging, "uv-x86_64-unknown-linux-gnu");
    mkdirSync(inner);
    writeFileSync(path.join(inner, "uv"), "uv");
    writeFileSync(path.join(inner, "uvx"), "uvx");
    writeFileSync(path.join(inner, "README"), "not a program");
    const tarball = path.join(staging, "uv.tar.gz");
    execFileSync("tar", ["-czf", tarball, "-C", staging, "uv-x86_64-unknown-linux-gnu"]);

    const c = context(
      "uv",
      { platform: "linux", which: (n) => (n === "tar" ? "tar" : null) },
      { real: true, serve: (_url, dest) => copyFileSync(tarball, dest) },
    );
    await run("uv", c);
    const bin = path.join(c.workDir, ".local", "bin");
    expect(readFileSync(path.join(bin, "uv"), "utf8")).toBe("uv");
    expect(readFileSync(path.join(bin, "uvx"), "utf8")).toBe("uvx");
    expect(() => statSync(path.join(bin, "README"))).toThrow();
    expect(c.calls[0]?.args.slice(0, 1)).toEqual(["-xzf"]);
  });
});

describe("npm and appium", () => {
  it("installs globally where the prefix is the person's, and under ~/.local where only root could write", async () => {
    const writable = mkdtempSync(path.join(os.tmpdir(), "tt-npm-prefix-"));
    const mine = context("appium", { platform: "linux" }, { lines: [writable] });
    await run("appium", mine);
    expect(mine.calls.map((c) => c.args)).toEqual([["prefix", "-g"], ["install", "-g", "appium"]]);

    if (process.getuid?.() === 0) return;
    const system = context("appium", { platform: "linux" }, { lines: ["/usr"] });
    await run("appium", system);
    expect(system.calls[1]?.args).toEqual(["install", "-g", "--prefix", path.join(system.workDir, ".local"), "appium"]);
  });

  it("refuses to run without the command an earlier step should have provided", async () => {
    const c = context("appium-uiautomator2", { platform: "linux", which: () => null });
    await expect(run("appium-uiautomator2", c)).rejects.toBeInstanceOf(InstallError);
    expect(c.calls).toEqual([]);
  });
});

describe("interactive installs", () => {
  it("opens macOS's own installer, then waits — re-checking — until the tools are there", async () => {
    let checks = 0;
    const c = context("xcode-clt", { platform: "darwin" }, { detect: () => ({ status: ++checks >= 3 ? "installed" : "missing" }) });
    await run("xcode-clt", c);
    expect(c.calls).toEqual([{ command: "/usr/bin/xcode-select", args: ["--install"] }]);
    expect(c.progress.some((p) => p.waiting === true)).toBe(true);
    expect(checks).toBe(3);
  });

  it("downloads Homebrew's installer first and has Terminal run that file — no curl in a shell", async () => {
    const c = context("homebrew", { platform: "darwin" });
    await run("homebrew", c);
    const installer = path.join(c.workDir, "homebrew-install.sh");
    const command = path.join(c.workDir, "homebrew-install.command");
    expect(c.downloads).toEqual([{ url: "https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh", dest: installer }]);
    expect(c.calls).toEqual([{ command: "/usr/bin/open", args: ["-a", "Terminal", command] }]);
    const script = readFileSync(command, "utf8");
    expect(script).toContain(`/bin/bash '${installer}'`);
    expect(script).not.toContain("curl");
    expect(statSync(command).mode & 0o111).not.toBe(0);
  });

  it.skipIf(process.platform === "win32")("fails loudly in Terminal when the downloaded installer is missing", () => {
    const dir = mkdtempSync(path.join(os.tmpdir(), "tt-command-"));
    const command = path.join(dir, "install.command");
    writeFileSync(command, terminalScript(path.join(dir, "it's gone.sh")), { mode: 0o755 });
    let message = "";
    try {
      execFileSync("/bin/bash", [command], { stdio: "pipe" });
    } catch (err) {
      message = String((err as { stderr?: Buffer }).stderr ?? "");
    }
    expect(message).toContain("is missing");
  });
});

describe("git on a Mac", () => {
  it("runs nothing of its own: it came with the command line tools its dependency installed", async () => {
    const c = context("git", { platform: "darwin" });
    await run("git", c);
    expect(c.calls).toEqual([]);
    expect(c.downloads).toEqual([]);
  });
});

describe("sdkmanager progress", () => {
  it("folds one bar per package into one rising number", () => {
    const next = packageProgress(2, 50, 100);
    const seen = ["[==   ] 40% Downloading", "[=====] 100% Unzipping", "noise", "[=    ] 10% Downloading", "[=====] 100% done"].map(next);
    expect(seen).toEqual([60, 75, null, 78, 100]);
  });
});
