import { describe, expect, it } from "vitest";
import { unreachableCopy } from "./copy";

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
