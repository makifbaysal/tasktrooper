import { describe, expect, it } from "vitest";
import type { Release } from "@/api";
import { openReleaseByTask, verifyMinutesLeft } from "@/lib/release-board";

function makeRelease(overrides: Partial<Release> = {}): Release {
  return {
    id: "r1",
    repository_id: "repo-1",
    version: "b53ee526cc18",
    mode: "on_merge",
    status: "verifying",
    profile: { mode: "on_merge" } as Release["profile"],
    checks: {} as Release["checks"],
    tasks: [{ id: "t1" }],
    created_at: "2026-10-01T11:09:24Z",
    updated_at: "2026-10-01T11:10:32Z",
    ...overrides,
  };
}

describe("openReleaseByTask", () => {
  it("keeps the newest release of a task that two releases carry", () => {
    const newer = makeRelease({ id: "new", tasks: [{ id: "t1" }, { id: "t2" }] });
    const older = makeRelease({ id: "old", status: "awaiting_verdict", tasks: [{ id: "t1" }] });
    const byTask = openReleaseByTask([newer, older]);
    expect(byTask.get("t1")?.id).toBe("new");
    expect(byTask.get("t2")?.id).toBe("new");
    expect(byTask.has("t3")).toBe(false);
  });
});

describe("verifyMinutesLeft", () => {
  const now = new Date("2026-10-01T11:13:00Z").getTime();

  it("rounds the time left in the verify window up to whole minutes", () => {
    expect(verifyMinutesLeft(makeRelease({ verify_until: "2026-10-01T11:20:32Z" }), now)).toBe(8);
  });

  it("never goes below zero once the window has closed", () => {
    expect(verifyMinutesLeft(makeRelease({ verify_until: "2026-10-01T11:10:00Z" }), now)).toBe(0);
  });

  it("has nothing to say outside verifying", () => {
    expect(verifyMinutesLeft(makeRelease({ status: "deploying", verify_until: "2026-10-01T11:20:32Z" }), now)).toBeUndefined();
  });
});
