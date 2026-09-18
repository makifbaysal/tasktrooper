import { describe, expect, it } from "vitest";
import type { SupervisorSnapshot } from "@ipc/types.js";
import { supervisorLabel, unreachableCopy } from "./copy";

describe("unreachableCopy", () => {
  it("says menu bar and this machine on macOS", () => {
    expect(unreachableCopy(true)).toContain("menu bar");
    expect(unreachableCopy(true)).toContain("this machine");
    expect(unreachableCopy(true)).not.toContain("this Mac");
  });

  it("says system tray on Windows/Linux", () => {
    expect(unreachableCopy(false)).toContain("system tray");
    expect(unreachableCopy(false)).not.toContain("menu bar");
  });
});

describe("supervisorLabel", () => {
  const running = { state: "running" } as SupervisorSnapshot;

  it("names this Mac when running on macOS", () => {
    expect(supervisorLabel(running, true)).toBe("running on this Mac");
  });

  it("says plain running on Windows/Linux", () => {
    expect(supervisorLabel(running, false)).toBe("running");
  });
});
