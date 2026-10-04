export const PARENT_POLL_MS = 5_000;

export interface WatchdogDeps {
  env: NodeJS.ProcessEnv;
  stdin: {
    resume(): unknown;
    on(event: "end" | "close" | "error", listener: () => void): unknown;
  };
  kill(pid: number, signal: 0): void;
  setInterval(fn: () => void, ms: number): { unref(): unknown };
  shutdown(): void;
}

/**
 * The supervisor closes our stdin when it stops us, and the OS closes it when
 * the supervisor dies without doing so; the pid poll covers a parent whose pipe
 * end somehow outlives it. Neither is armed when run by hand.
 */
export function armParentWatchdog(deps: WatchdogDeps): void {
  let fired = false;
  const trigger = (): void => {
    if (fired) return;
    fired = true;
    deps.shutdown();
  };

  if (deps.env.TASKTROOPER_EXIT_ON_STDIN_CLOSE === "1") {
    deps.stdin.on("error", trigger);
    deps.stdin.on("end", trigger);
    deps.stdin.on("close", trigger);
    deps.stdin.resume();
  }

  const parentPid = Number(deps.env.TASKTROOPER_PARENT_PID);
  if (Number.isInteger(parentPid) && parentPid > 0) {
    deps
      .setInterval(() => {
        try {
          deps.kill(parentPid, 0);
        } catch (err) {
          // EPERM means the process exists but is not ours to signal.
          if ((err as NodeJS.ErrnoException).code === "ESRCH") trigger();
        }
      }, PARENT_POLL_MS)
      .unref();
  }
}
