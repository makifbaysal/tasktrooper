/**
 * Quitting, and the one variation of it that installs an update.
 *
 * This is a separate file for one reason: it is the sequence in which a
 * long-running child is stopped, and auto-update gave it a second ending.
 * Getting the order wrong does not fail loudly — it leaves an `.app` being
 * replaced underneath a `claude` process that is halfway through a task, or a
 * Postgres cluster that was never shut down. Neither shows up as an error
 * anybody sees, so the order is asserted in `quit.test.ts` rather than left to
 * be read.
 *
 * The rules it encodes:
 *
 *  - **The drain always runs, and always first.** Both endings wait for it.
 *    There is no path here that stops the process before the children are down
 *    — and the drain is what makes the backend cancel the Claude Code sessions
 *    it is supervising rather than orphaning them.
 *  - **A drain that fails still ends the app.** A child that will not die must
 *    not turn Quit into a hang; `SupervisedChild.stop` escalates to SIGKILL on
 *    its own, and whatever is left is not a reason to keep a menu-bar item the
 *    user has already dismissed.
 *  - **`applyUpdate` controls the relaunch, not the install.** Once Squirrel has
 *    staged an update, its ShipIt process installs it when this one exits —
 *    both endings below therefore apply it, and both do so only after the drain.
 *    What `applyUpdate` adds is `quitAndInstall()`, which asks ShipIt to bring
 *    the app back afterwards. That is what "Restart to update" promises and it
 *    is the only thing in this app that restarts it. A plain Quit applies the
 *    update and stays quit, which is the behaviour somebody pressing Quit meant.
 *  - **The handoff is not trusted to complete.** Squirrel refuses an update
 *    whose signature does not match, and a refusal must not leave a window with
 *    no tray, no child and no way out.
 */

export interface QuitSteps {
  /** Stop the update timers, so nothing starts a download mid-drain. */
  stopUpdates: () => void;
  /** Stop the notification poll, so it does not fire against a draining backend. */
  stopNotifications: () => void;
  destroyTray: () => void;
  /** The supervisor's drain: SIGTERM to every child, and time to use it. */
  drain: () => Promise<unknown>;
  /**
   * Hand off to Squirrel. Returns false when there is nothing staged, in which
   * case this is an ordinary quit after all.
   */
  installUpdate: () => boolean;
  exit: (code: number) => void;
  /** Injected so the test does not wait 30 real seconds. */
  schedule?: (fn: () => void, ms: number) => void;
}

/** How long to wait for Squirrel to replace this process before giving up. */
export const HANDOFF_TIMEOUT_MS = 30_000;

export interface QuitSequence {
  /** True once `run` has been entered. `before-quit` reads this. */
  readonly started: boolean;
  run(options?: { applyUpdate?: boolean }): Promise<void>;
}

export function quitSequence(steps: QuitSteps): QuitSequence {
  let started = false;
  const schedule = steps.schedule ?? ((fn, ms) => setTimeout(fn, ms).unref?.());

  return {
    get started() {
      return started;
    },

    async run(options: { applyUpdate?: boolean } = {}): Promise<void> {
      // Cmd-Q while the drain is already running, or the tray's Quit pressed
      // twice, must not start a second teardown of the same processes.
      if (started) return;
      started = true;

      steps.stopUpdates();
      steps.stopNotifications();
      steps.destroyTray();

      try {
        await steps.drain();
      } catch {
        // Deliberately swallowed: see the header. The app still exits.
      }

      if (options.applyUpdate && steps.installUpdate()) {
        schedule(() => steps.exit(0), HANDOFF_TIMEOUT_MS);
        return;
      }

      steps.exit(0);
    },
  };
}
