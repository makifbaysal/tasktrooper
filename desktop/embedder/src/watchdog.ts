export const PARENT_POLL_MS = 30_000;

export interface WatchdogDeps {
  env: NodeJS.ProcessEnv;
  stdin: {
    resume(): unknown;
    on(event: "end" | "close" | "error", listener: () => void): unknown;
  };
  /** `process.ppid`, read on every call: it changes when the parent dies and the OS re-parents us. */
  ppid(): number;
  kill(pid: number, signal: 0): void;
  setInterval(fn: () => void, ms: number): { unref(): unknown };
  shutdown(): void;
}

/**
 * The supervisor closes our stdin when it stops us, and the OS closes it when
 * the supervisor dies without doing so. The poll covers whatever outlives
 * that: an orphan is re-parented, to launchd/init (pid 1) or to a Linux
 * subreaper, so a moved ppid means the parent is gone. It is armed even when
 * run by hand, because an embedder nobody will ever stop again still holds a
 * port and, once loaded, a model.
 *
 * Windows never re-parents, so there the explicit parent pid is what notices.
 * EPERM from it counts as dead too: our parent is ours to signal, so a pid we
 * may not signal has been reused by somebody else's process.
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
  const hasParentPid = Number.isInteger(parentPid) && parentPid > 0;
  const initialPpid = deps.ppid();

  const reparented = (): boolean => {
    const ppid = deps.ppid();
    // Pid 1 from the very start is an orphan as well — the parent died before
    // this ran — unless pid 1 is genuinely what spawned us (a container).
    return ppid !== initialPpid || (ppid === 1 && parentPid !== 1);
  };
  const parentGone = (): boolean => {
    if (!hasParentPid) return false;
    try {
      deps.kill(parentPid, 0);
      return false;
    } catch (err) {
      const code = (err as NodeJS.ErrnoException).code;
      return code === "ESRCH" || code === "EPERM";
    }
  };

  deps
    .setInterval(() => {
      if (reparented() || parentGone()) trigger();
    }, PARENT_POLL_MS)
    .unref();
}
