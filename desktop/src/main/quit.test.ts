import { describe, expect, it } from "vitest";
import { HANDOFF_TIMEOUT_MS, quitSequence, type QuitSteps } from "./quit.js";

/**
 * The quit sequence, and specifically the claim auto-update rests on: an
 * update never gets applied over a running child.
 *
 * These are ordering tests, not coverage. The failure they exist to catch is
 * silent — the `.app` being replaced while a Claude Code session was halfway
 * through a task, whose only symptom is a task that stopped and a database
 * that saw a session time out rather than close. So the fake below records a
 * single ordered list of what happened, and the assertions are about that list.
 *
 * The ordering matters more than it looks, because on macOS the install is not
 * something this process performs: Squirrel's ShipIt waits for this process to
 * exit and swaps the bundle then. Everything here therefore has to be finished
 * before `exit`, not merely before `quitAndInstall`.
 */

function recorder(overrides: Partial<QuitSteps> = {}) {
  const order: string[] = [];
  const steps: QuitSteps = {
    stopUpdates: () => order.push("stop-updates"),
    stopNotifications: () => order.push("stop-notifications"),
    destroyTray: () => order.push("destroy-tray"),
    drain: async () => {
      order.push("drain:start");
      // A real drain is four sequential stops with health waits between them.
      // Yielding twice is what makes "install was called before this resolved"
      // an observable failure rather than a coincidence of microtask order.
      await Promise.resolve();
      await Promise.resolve();
      order.push("drain:done");
    },
    installUpdate: () => {
      order.push("install");
      return true;
    },
    exit: (code) => order.push(`exit:${code}`),
    schedule: () => order.push("schedule-fallback"),
    ...overrides,
  };
  return { order, quit: quitSequence(steps) };
}

describe("quitSequence", () => {
  it("drains before it exits, and never relaunches on a plain quit", async () => {
    const { order, quit } = recorder();
    await quit.run();

    expect(order).toEqual(["stop-updates", "stop-notifications", "destroy-tray", "drain:start", "drain:done", "exit:0"]);
    // Squirrel's ShipIt applies a staged update when this process exits, which
    // is what makes "applies on quit" true. What must NOT happen is
    // quitAndInstall(), because that is the call that brings the app back —
    // and somebody who pressed Quit did not ask for the app to return.
    expect(order).not.toContain("install");
  });

  it("drains to completion BEFORE handing off to Squirrel", async () => {
    const { order, quit } = recorder();
    await quit.run({ applyUpdate: true });

    expect(order).toEqual([
      "stop-updates",
      "stop-notifications",
      "destroy-tray",
      "drain:start",
      "drain:done",
      "install",
      "schedule-fallback",
    ]);
    // Said again as an index comparison, because this is the assertion the
    // whole file exists for and `toEqual` on a long array is easy to weaken by
    // accident.
    expect(order.indexOf("drain:done")).toBeLessThan(order.indexOf("install"));
  });

  it("does not exit immediately after a successful handoff — Squirrel replaces the process", async () => {
    const { order, quit } = recorder();
    await quit.run({ applyUpdate: true });
    // Calling exit here would kill the app out from under the installer.
    expect(order).not.toContain("exit:0");
  });

  it("still exits when Squirrel never takes over", async () => {
    const scheduled: (() => void)[] = [];
    const { order, quit } = recorder({
      schedule: (fn, ms) => {
        expect(ms).toBe(HANDOFF_TIMEOUT_MS);
        scheduled.push(fn);
      },
    });

    await quit.run({ applyUpdate: true });
    expect(order).not.toContain("exit:0");
    expect(scheduled).toHaveLength(1);

    // A refused signature is the realistic reason. Without this the user is
    // left with a window that has no tray, no children and no way to quit.
    scheduled[0]?.();
    expect(order).toContain("exit:0");
  });

  it("exits normally when the press arrives but nothing is actually staged", async () => {
    const { order, quit } = recorder({
      installUpdate: () => {
        order.push("install");
        return false;
      },
    });

    await quit.run({ applyUpdate: true });
    expect(order.at(-1)).toBe("exit:0");
    expect(order).not.toContain("schedule-fallback");
  });

  it("still exits when the drain fails", async () => {
    const { order, quit } = recorder({
      drain: () => {
        order.push("drain:start");
        return Promise.reject(new Error("the tunnel would not stop"));
      },
    });

    // A child that refuses to die must not turn Quit into a hang.
    await expect(quit.run()).resolves.toBeUndefined();
    expect(order).toEqual(["stop-updates", "stop-notifications", "destroy-tray", "drain:start", "exit:0"]);
  });

  it("does not drain twice when quit is pressed twice", async () => {
    const { order, quit } = recorder();
    const first = quit.run();
    // Cmd-Q during the drain, or `before-quit` firing after the tray's Quit.
    const second = quit.run();
    await Promise.all([first, second]);

    expect(order.filter((step) => step === "drain:start")).toHaveLength(1);
    expect(order.filter((step) => step.startsWith("exit:"))).toHaveLength(1);
  });

  it("reports that it started as soon as it is entered, so before-quit stops fighting it", async () => {
    const { quit } = recorder();
    expect(quit.started).toBe(false);
    const running = quit.run();
    // `app.on("before-quit")` calls preventDefault() unless this is true; if it
    // stayed false until the drain resolved, the quit the drain is about to
    // perform would be cancelled by our own handler.
    expect(quit.started).toBe(true);
    await running;
  });
});
