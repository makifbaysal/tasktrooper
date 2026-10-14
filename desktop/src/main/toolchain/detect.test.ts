import { describe, expect, it } from "vitest";
import { TOOLCHAIN_IDS, type ToolchainId } from "../../ipc/types.js";
import { ToolchainDetector } from "./detect.js";
import { jobForRemote, redactHome, reportForRemote } from "./remote.js";
import type { Installer, ProbeKit, ToolDefinition } from "./types.js";

/**
 * How report sweeps are shared: at most one running and one queued behind it,
 * however many callers force one. Each sweep is counted and held open until
 * the test lets it finish.
 */
function harness() {
  let sweeps = 0;
  const releases: (() => void)[] = [];
  const definitions = Object.fromEntries(
    TOOLCHAIN_IDS.map((id): [ToolchainId, ToolDefinition] => [
      id,
      {
        id,
        label: id,
        docsUrl: `https://docs.example/${id}`,
        affectsRuntime: false,
        recipes: id === "git" ? { linux: { install: { kind: "linux-pkg", apt: ["git"], dnf: ["git"] } } } : {},
        detect: () => {
          sweeps += 1;
          return new Promise((resolve) => releases.push(() => resolve({ status: "installed", path: "/home/ada/.local/bin/git" })));
        },
      },
    ]),
  ) as Record<ToolchainId, ToolDefinition>;
  const installer: Installer = { installKind: "auto", method: "apt (pkexec)", command: "pkexec apt-get install -y git", dependsOn: [], run: () => Promise.resolve() };
  const detector = new ToolchainDetector({
    definitions,
    platform: "linux",
    kit: () => ({}) as ProbeKit,
    installerFor: (id) => (id === "git" ? installer : null),
  });
  const tick = (): Promise<void> => new Promise((r) => setTimeout(r, 0));
  const finishAll = async (): Promise<void> => {
    while (releases.length > 0) {
      releases.shift()?.();
      await tick();
    }
  };
  return { detector, sweeps: () => sweeps, finishAll, tick };
}

describe("ToolchainDetector sweeps", () => {
  it("lets a forced caller join a sweep that began after the last change", async () => {
    const h = harness();
    const first = h.detector.report();
    const forced = h.detector.report(true);
    expect(forced).toBe(first);
    await h.finishAll();
    expect(h.sweeps()).toBe(1);
  });

  it("queues one follow-up behind a stale sweep, shared by every caller, forced or not", async () => {
    const h = harness();
    const stale = h.detector.report();
    h.detector.invalidate();
    const a = h.detector.report(true);
    const b = h.detector.report(true);
    const c = h.detector.report();
    expect(a).not.toBe(stale);
    expect(b).toBe(a);
    expect(c).toBe(a);
    await h.finishAll();
    await Promise.all([stale, a]);
    expect(h.sweeps()).toBe(2);
  });

  it("starts fresh when forced with nothing running, and reuses a fresh result otherwise", async () => {
    const h = harness();
    const first = h.detector.report();
    await h.finishAll();
    await first;
    const reused = h.detector.report();
    expect(await reused).toBe(await first);
    expect(h.sweeps()).toBe(1);
    const forced = h.detector.report(true);
    await h.finishAll();
    await forced;
    expect(h.sweeps()).toBe(2);
  });
});

describe("what the account's remote page is shown", () => {
  it("drops paths and writes the home directory as ~ everywhere else", async () => {
    const h = harness();
    const pending = h.detector.report();
    await h.finishAll();
    const report = await pending;
    const remote = reportForRemote(
      { ...report, items: report.items.map((i) => ({ ...i, detail: "found in /home/ada/.local/bin", command: "copy to /home/ada/.local/bin/git" })) },
      "/home/ada",
      "linux",
    );
    expect(remote.items[0]?.path).toBeUndefined();
    expect(JSON.stringify(remote)).not.toContain("/home/ada");
    expect(remote.items[0]?.detail).toBe("found in ~/.local/bin");
  });

  it("redacts a job's details, errors and log lines, Windows spellings included", () => {
    const state = jobForRemote(
      {
        phase: "done",
        percent: 100,
        steps: [
          {
            id: "git",
            label: "Git",
            state: "failed",
            installKind: "auto",
            error: "Could not write C:\\Users\\Ada\\AppData\\Local\\x",
            log: ["unpacking into c:/users/ada/AppData/Local/Temp"],
          },
        ],
      },
      "C:\\Users\\Ada",
      "win32",
    );
    expect(state.steps[0]?.error).toBe("Could not write ~\\AppData\\Local\\x");
    expect(state.steps[0]?.log).toEqual(["unpacking into ~/AppData/Local/Temp"]);
    expect(redactHome("nothing here", "/home/ada", "linux")).toBe("nothing here");
  });
});
