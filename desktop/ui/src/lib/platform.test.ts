import { afterEach, describe, expect, it, vi } from "vitest";
import { isMacPlatform, shortcutModifier } from "@/lib/platform";

const userAgents = {
  mac: "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/537.36 (KHTML, like Gecko) TaskTrooper/0.2.16 Electron/38.0.0",
  windows: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) TaskTrooper/0.2.16 Electron/38.0.0",
  linux: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) TaskTrooper/0.2.16 Electron/38.0.0",
};

describe("platform", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("shows ⌘ on a Mac", () => {
    vi.stubGlobal("navigator", { userAgent: userAgents.mac });
    expect(isMacPlatform()).toBe(true);
    expect(shortcutModifier()).toBe("⌘");
  });

  it.each([["windows"], ["linux"]] as const)("shows Ctrl+ on %s", (os) => {
    vi.stubGlobal("navigator", { userAgent: userAgents[os] });
    expect(isMacPlatform()).toBe(false);
    expect(shortcutModifier()).toBe("Ctrl+");
  });
});
