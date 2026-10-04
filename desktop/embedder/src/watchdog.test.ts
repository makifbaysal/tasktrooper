import { EventEmitter } from "node:events";
import { describe, expect, it, vi } from "vitest";
import { armParentWatchdog, PARENT_POLL_MS } from "./watchdog.js";

function setup(env: NodeJS.ProcessEnv, kill: (pid: number, signal: 0) => void = () => undefined) {
  const stdin = Object.assign(new EventEmitter(), { resume: vi.fn() });
  const shutdown = vi.fn();
  const unref = vi.fn();
  let poll: (() => void) | undefined;
  let ms = 0;
  armParentWatchdog({
    env,
    stdin,
    kill,
    setInterval: (fn, interval) => {
      poll = fn;
      ms = interval;
      return { unref };
    },
    shutdown,
  });
  return { stdin, shutdown, unref, poll: () => poll, ms: () => ms };
}

const errno = (code: string): NodeJS.ErrnoException => Object.assign(new Error(code), { code });

describe("armParentWatchdog", () => {
  it("arms nothing without the env vars", () => {
    const w = setup({});
    expect(w.stdin.resume).not.toHaveBeenCalled();
    expect(w.stdin.listenerCount("close")).toBe(0);
    expect(w.poll()).toBeUndefined();
  });

  it("shuts down once when stdin closes", () => {
    const w = setup({ TASKTROOPER_EXIT_ON_STDIN_CLOSE: "1" });
    expect(w.stdin.resume).toHaveBeenCalled();
    w.stdin.emit("end");
    w.stdin.emit("close");
    w.stdin.emit("error");
    expect(w.shutdown).toHaveBeenCalledTimes(1);
  });

  it("shuts down when the parent is gone", () => {
    const w = setup({ TASKTROOPER_PARENT_PID: "4242" }, () => {
      throw errno("ESRCH");
    });
    expect(w.ms()).toBe(PARENT_POLL_MS);
    expect(w.unref).toHaveBeenCalled();
    w.poll()?.();
    w.poll()?.();
    expect(w.shutdown).toHaveBeenCalledTimes(1);
  });

  it("treats EPERM and a live parent as alive", () => {
    const kill = vi.fn().mockImplementationOnce(() => undefined).mockImplementationOnce(() => {
      throw errno("EPERM");
    });
    const w = setup({ TASKTROOPER_PARENT_PID: "4242" }, kill);
    w.poll()?.();
    w.poll()?.();
    expect(kill).toHaveBeenCalledWith(4242, 0);
    expect(w.shutdown).not.toHaveBeenCalled();
  });

  it("ignores an invalid parent pid", () => {
    expect(setup({ TASKTROOPER_PARENT_PID: "abc" }).poll()).toBeUndefined();
    expect(setup({ TASKTROOPER_PARENT_PID: "0" }).poll()).toBeUndefined();
  });
});
