import { EventEmitter } from "node:events";
import { describe, expect, it, vi } from "vitest";
import { armParentWatchdog, PARENT_POLL_MS } from "./watchdog.js";

function setup(
  env: NodeJS.ProcessEnv,
  kill: (pid: number, signal: 0) => void = () => undefined,
  ppid: () => number = () => 4242,
) {
  const stdin = Object.assign(new EventEmitter(), { resume: vi.fn() });
  const shutdown = vi.fn();
  const unref = vi.fn();
  let poll: (() => void) | undefined;
  let ms = 0;
  armParentWatchdog({
    env,
    stdin,
    ppid,
    kill,
    setInterval: (fn, interval) => {
      poll = fn;
      ms = interval;
      return { unref };
    },
    shutdown,
  });
  return { stdin, shutdown, unref, poll: () => poll!(), ms: () => ms };
}

const errno = (code: string): NodeJS.ErrnoException => Object.assign(new Error(code), { code });

describe("armParentWatchdog", () => {
  it("polls every 30 s, without keeping the process alive", () => {
    const w = setup({});
    expect(PARENT_POLL_MS).toBe(30_000);
    expect(w.ms()).toBe(PARENT_POLL_MS);
    expect(w.unref).toHaveBeenCalled();
  });

  it("does not touch stdin unless asked to", () => {
    const w = setup({});
    expect(w.stdin.resume).not.toHaveBeenCalled();
    expect(w.stdin.listenerCount("close")).toBe(0);
  });

  it("shuts down once when stdin closes", () => {
    const w = setup({ TASKTROOPER_EXIT_ON_STDIN_CLOSE: "1" });
    expect(w.stdin.resume).toHaveBeenCalled();
    w.stdin.emit("end");
    w.stdin.emit("close");
    w.stdin.emit("error");
    expect(w.shutdown).toHaveBeenCalledTimes(1);
  });

  /**
   * The orphan that ran for 9.5 hours: launchd had adopted it, and nothing
   * it watched said so.
   */
  it("shuts down once re-parented to pid 1, even run by hand with no env vars", () => {
    let ppid = 4242;
    const w = setup({}, () => undefined, () => ppid);
    w.poll();
    expect(w.shutdown).not.toHaveBeenCalled();

    ppid = 1;
    w.poll();
    w.poll();
    expect(w.shutdown).toHaveBeenCalledTimes(1);
  });

  it("shuts down once adopted by a subreaper, whose pid is not 1", () => {
    let ppid = 4242;
    const w = setup({}, () => undefined, () => ppid);
    ppid = 977;
    w.poll();
    expect(w.shutdown).toHaveBeenCalledTimes(1);
  });

  it("shuts down when it was already an orphan by the time it armed", () => {
    const w = setup({ TASKTROOPER_PARENT_PID: "4242" }, () => undefined, () => 1);
    w.poll();
    expect(w.shutdown).toHaveBeenCalledTimes(1);
  });

  it("stays up under a parent that really is pid 1", () => {
    const w = setup({ TASKTROOPER_PARENT_PID: "1" }, () => undefined, () => 1);
    w.poll();
    expect(w.shutdown).not.toHaveBeenCalled();
  });

  it("shuts down when the parent pid is gone, for a parent that never re-parents us (Windows)", () => {
    const w = setup({ TASKTROOPER_PARENT_PID: "4242" }, () => {
      throw errno("ESRCH");
    });
    w.poll();
    expect(w.shutdown).toHaveBeenCalledTimes(1);
  });

  it("treats EPERM as a reused pid, so the parent is gone", () => {
    const w = setup({ TASKTROOPER_PARENT_PID: "4242" }, () => {
      throw errno("EPERM");
    });
    w.poll();
    expect(w.shutdown).toHaveBeenCalledTimes(1);
  });

  it("stays up while the parent answers and the ppid holds", () => {
    const kill = vi.fn();
    const w = setup({ TASKTROOPER_PARENT_PID: "4242" }, kill);
    w.poll();
    w.poll();
    expect(kill).toHaveBeenCalledWith(4242, 0);
    expect(w.shutdown).not.toHaveBeenCalled();
  });

  it("ignores an invalid parent pid but still watches the ppid", () => {
    const kill = vi.fn();
    for (const raw of ["abc", "0"]) {
      const w = setup({ TASKTROOPER_PARENT_PID: raw }, kill);
      w.poll();
      expect(w.shutdown).not.toHaveBeenCalled();
    }
    expect(kill).not.toHaveBeenCalled();
  });
});
