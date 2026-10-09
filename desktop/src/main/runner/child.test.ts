import { mkdtempSync, chmodSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RunnerChild } from "./child.js";

/**
 * Driven against real spawned processes, the same standard `detect.test.ts`
 * sets for this codebase: a mocked `child_process.spawn` would assert that
 * this file's own bookkeeping runs, not that the process it manages actually
 * receives its stdin document, ignores SIGTERM the way a real hung process
 * can, or gets reaped.
 */

let dir: string | undefined;
function scriptDir(): string {
  dir ??= mkdtempSync(path.join(tmpdir(), "tt-runner-child-"));
  return dir;
}

let scripts = 0;
function writeScript(body: string): string {
  scripts += 1;
  const file = path.join(scriptDir(), `fake-runner-${scripts}.sh`);
  writeFileSync(file, `#!/bin/sh\n${body}`);
  chmodSync(file, 0o755);
  return file;
}

const children: RunnerChild[] = [];
function child(): RunnerChild {
  const c = new RunnerChild();
  children.push(c);
  return c;
}

afterEach(async () => {
  await Promise.all(children.splice(0).map((c) => c.stop()));
});

describe("start", () => {
  it("writes the config line to stdin and leaves the pipe open", async () => {
    const captured = path.join(scriptDir(), "captured.txt");
    // `read` writes through the shell's own unbuffered redirection, unlike
    // `cat`, which block-buffers when its output is not a terminal and can sit
    // on a single short line indefinitely.
    const script = writeScript(`IFS= read -r line\nprintf '%s\\n' "$line" > "${captured}"\nsleep 5\n`);
    const c = child();
    await c.start({ command: script, args: [], env: process.env, stdin: '{"tm_base_url":"https://x"}\n' });
    // Polled, not a fixed sleep: a loaded machine can take longer than any
    // fixed wait to start the shell and let it write the file.
    await vi.waitFor(() => expect(readFileSync(captured, "utf8")).toBe('{"tm_base_url":"https://x"}\n'), {
      timeout: 4000,
      interval: 50,
    });
    expect(c.running).toBe(true);
  });

  it("reports waiting-health right after spawn, and running() true", async () => {
    const script = writeScript("sleep 5\n");
    const c = child();
    await c.start({ command: script, args: [], env: process.env, stdin: "\n" });
    expect(c.state).toBe("waiting-health");
    expect(c.running).toBe(true);
    c.markHealthy();
    expect(c.state).toBe("healthy");
  });

  it("rejects when the binary does not exist", async () => {
    const c = child();
    await expect(
      c.start({ command: "/no/such/binary-xyz", args: [], env: process.env, stdin: "\n" }),
    ).rejects.toBeTruthy();
    expect(c.state).toBe("failed");
  });
});

describe("send", () => {
  it("returns false once the process has exited — the ordinary case between a crash and a restart", async () => {
    const script = writeScript("exit 0\n");
    const c = child();
    await c.start({ command: script, args: [], env: process.env, stdin: "\n" });
    await new Promise((resolve) => c.once("exited", resolve));
    expect(c.send("more\n")).toBe(false);
  });
});

describe("crashed vs stopped", () => {
  it("an unexpected exit emits crashed", async () => {
    const script = writeScript("exit 3\n");
    const c = child();
    const crashed = new Promise<number | null>((resolve) => c.once("crashed", (code) => resolve(code)));
    await c.start({ command: script, args: [], env: process.env, stdin: "\n" });
    const code = await crashed;
    expect(code).toBe(3);
    expect(c.state).toBe("crashed");
  });

  it("stop() ends stdin first, so a runner that ignores SIGTERM but drains on EOF still stops promptly", async () => {
    // The Windows case on any OS: no signal reaches it, and closing the
    // control channel is the whole request.
    const script = writeScript("trap '' TERM\ncat >/dev/null\nexit 0\n");
    const c = child();
    const stderr: string[] = [];
    c.on("log", (stream, text) => {
      if (stream === "stderr") stderr.push(text);
    });
    await c.start({ command: script, args: [], env: process.env, stdin: "\n" });
    await new Promise((resolve) => setTimeout(resolve, 300));
    const t0 = Date.now();
    await c.stop();
    expect(c.state).toBe("stopped");
    expect(Date.now() - t0).toBeLessThan(5_000);
    expect(stderr.some((line) => line.includes("SIGKILL"))).toBe(false);
  });

  it("stop() on a process that exits promptly does not escalate to SIGKILL", async () => {
    // No trap: SIGTERM's default disposition already terminates a shell
    // script immediately, even mid-`sleep` — no custom handler needed for
    // "responds promptly", and it sidesteps whether a given `/bin/sh` runs a
    // trap while blocked in wait() for a foreground child.
    const script = writeScript("sleep 5\n");
    const c = child();
    await c.start({ command: script, args: [], env: process.env, stdin: "\n" });
    await new Promise((resolve) => setTimeout(resolve, 300));
    const t0 = Date.now();
    await c.stop();
    expect(c.state).toBe("stopped");
    expect(Date.now() - t0).toBeLessThan(1_000);
  });
});

describe("scheduleRestart", () => {
  /**
   * Each call reschedules (and so cancels the previous pending timer), which
   * is what lets this assert the whole progression without waiting for any
   * of them to actually fire.
   */
  it("starts at the floor, doubles each call, and caps at 30s", () => {
    const c = child();
    expect(c.scheduleRestart(() => undefined)).toBe(1_000);
    expect(c.scheduleRestart(() => undefined)).toBe(2_000);
    expect(c.scheduleRestart(() => undefined)).toBe(4_000);
    for (let i = 0; i < 10; i += 1) c.scheduleRestart(() => undefined);
    expect(c.scheduleRestart(() => undefined)).toBe(30_000);
    c.resetCounters();
    expect(c.scheduleRestart(() => undefined)).toBe(1_000);
  });
});

/**
 * `TERM_GRACE_MS` is derived from the runner's own worst-case drain
 * (`RUNNER_CLAUDE_GRACE_MS` + `RUNNER_REAP_TIMEOUT_MS` + a margin) — see the
 * constants and comment in child.ts. This drives the real escalation against
 * a script that ignores SIGTERM outright, the same class of test
 * `desktop/runner/session_test.go` runs for the Go side, and for the same
 * reason: asserting that `proc.kill` was CALLED would miss a process that is
 * still alive.
 */
describe("stop", () => {
  it(
    "escalates to SIGKILL when the child ignores SIGTERM",
    async () => {
      const script = writeScript("trap '' TERM\necho trapped\nsleep 60\n");
      const c = child();
      const stderr: string[] = [];
      let trapped!: () => void;
      const trapInstalled = new Promise<void>((resolve) => (trapped = resolve));
      c.on("log", (stream, text) => {
        if (stream === "stderr") stderr.push(text);
        if (text.includes("trapped")) trapped();
      });
      await c.start({ command: script, args: [], env: process.env, stdin: "\n" });
      // `start()` resolves on the 'spawn' event, which fires as soon as
      // fork() succeeds — before the shell has run its `trap` line. A
      // SIGTERM delivered then is handled by the DEFAULT disposition, and a
      // fixed settle delay lost that race under a loaded test run; the
      // script says when the trap is in place instead.
      await trapInstalled;
      await c.stop();
      expect(c.state).toBe("stopped");
      expect(stderr.some((line) => line.includes("SIGKILL"))).toBe(true);
    },
    40_000,
  );
});
