import { chmodSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";

/**
 * Installers driven as real processes, the way `services/detect.test.ts`
 * drives fake CLIs: each case writes a shell script that behaves like an
 * installer (colours, progress bars, stdin prompts, a child it leaves
 * running) and spawns it for real — argv, environment, process group, exit
 * code and the kill on cancel are all the actual path.
 */

const root = vi.hoisted(() => `${process.env.TMPDIR ?? "/tmp"}/tt-toolchain-proc-${process.pid}`.replace(/\/+/g, "/"));

vi.mock("electron", () => ({
  app: { getAppPath: () => root, getPath: () => `${root}/userData`, isPackaged: false },
}));
vi.mock("../services/login-env.js", () => ({
  loginShellPath: () => Promise.resolve([]),
  knownLoginShellPath: () => [],
}));

const { execLogged } = await import("./process.js");
const { createStepMachine } = await import("./machine.js");
const { ToolchainJobRunner } = await import("./job.js");
const { CancelledError, InstallError } = await import("./types.js");

let scripts = 0;

function fakeInstaller(body: string): string {
  scripts += 1;
  const dir = path.join(root, "fakes", String(scripts));
  mkdirSync(dir, { recursive: true });
  const file = path.join(dir, "installer");
  writeFileSync(file, `#!/bin/sh\n${body}\n`);
  chmodSync(file, 0o755);
  return file;
}

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

async function until(check: () => boolean, ms = 3_000): Promise<void> {
  const deadline = Date.now() + ms;
  while (!check()) {
    if (Date.now() > deadline) throw new Error("timed out waiting");
    await new Promise((r) => setTimeout(r, 20));
  }
}

describe.skipIf(process.platform === "win32")("execLogged, against a real process", () => {
  it("streams both outputs as lines, progress redraws included, and resolves with the exit code", async () => {
    const installer = fakeInstaller(
      [
        "printf '\\033[32m==>\\033[0m Downloading\\n'",
        "printf '[==  ] 10%%\\r[====] 100%%\\n'",
        "echo 'warning: something' >&2",
        "exit 3",
      ].join("\n"),
    );
    const lines: string[] = [];
    const code = await execLogged(installer, [], { signal: new AbortController().signal, onLine: (l) => lines.push(l) });
    expect(code).toBe(3);
    expect(lines).toContain("\u001b[32m==>\u001b[0m Downloading");
    expect(lines).toContain("[==  ] 10%");
    expect(lines).toContain("[====] 100%");
    expect(lines).toContain("warning: something");
  });

  it("passes arguments as they are, the recipe's environment, and answers on stdin", async () => {
    const installer = fakeInstaller(['echo "arg=$1"', 'echo "mode=$TT_MODE"', "read answer", 'echo "answer=$answer"'].join("\n"));
    const lines: string[] = [];
    const code = await execLogged(installer, ["platforms;android-35 $(touch /tmp/never)"], {
      signal: new AbortController().signal,
      env: { TT_MODE: "noninteractive" },
      input: "y\n",
      onLine: (l) => lines.push(l),
    });
    expect(code).toBe(0);
    expect(lines).toEqual(["arg=platforms;android-35 $(touch /tmp/never)", "mode=noninteractive", "answer=y"]);
  });

  it("on cancel, kills the installer and what it started, and rejects as cancelled", async () => {
    const pidFile = path.join(root, `child-${Date.now()}.pid`);
    const installer = fakeInstaller([`sleep 30 & echo $! > '${pidFile}'`, "echo started", "wait"].join("\n"));
    const controller = new AbortController();
    const lines: string[] = [];
    const running = execLogged(installer, [], { signal: controller.signal, onLine: (l) => lines.push(l) });
    await until(() => lines.includes("started") && existsSync(pidFile));
    const child = Number.parseInt(readFileSync(pidFile, "utf8"), 10);
    expect(alive(child)).toBe(true);

    controller.abort();
    await expect(running).rejects.toBeInstanceOf(CancelledError);
    await until(() => !alive(child));
  });

  it("does not wait on a background process the installer left holding its output", async () => {
    const pidFile = path.join(root, `daemon-${Date.now()}.pid`);
    const installer = fakeInstaller([`sleep 30 & echo $! > '${pidFile}'`, "echo registered", "exit 0"].join("\n"));
    const started = Date.now();
    const code = await execLogged(installer, [], { signal: new AbortController().signal });
    expect(code).toBe(0);
    expect(Date.now() - started).toBeLessThan(4_000);
    process.kill(Number.parseInt(readFileSync(pidFile, "utf8"), 10), "SIGKILL");
  });

  it("says what could not be started", async () => {
    await expect(
      execLogged(path.join(root, "no-such-installer"), [], { signal: new AbortController().signal }),
    ).rejects.toBeInstanceOf(InstallError);
  });
});

describe.skipIf(process.platform === "win32")("a job step running a real installer", () => {
  it("logs its output, and is done once detection finds what it installed", async () => {
    const marker = path.join(root, `installed-${Date.now()}`);
    const installer = fakeInstaller(["printf '\\033[1mInstalling\\033[0m\\n'", `touch '${marker}'`, "echo finished"].join("\n"));
    const runner = new ToolchainJobRunner({
      installerFor: () => ({
        installKind: "auto",
        method: "Fake",
        command: "installer",
        dependsOn: [],
        run: async (ctx) => {
          const code = await ctx.exec(installer, []);
          if (code !== 0) throw new InstallError(`exit ${code}`);
        },
      }),
      label: () => "Go",
      affectsRuntime: () => true,
      detect: () => Promise.resolve(existsSync(marker) ? { status: "installed", path: marker } : { status: "missing" }),
      machine: createStepMachine(path.join(root, "work")),
    });
    runner.enqueue(["go"]);
    await runner.idle();
    const step = runner.snapshot().steps[0];
    expect(step).toMatchObject({ state: "done", log: ["Installing", "finished"] });
    expect(runner.snapshot().restartRecommended).toBe(true);
  });
});
