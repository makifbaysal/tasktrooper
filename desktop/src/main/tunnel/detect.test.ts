import { describe, expect, it } from "vitest";
import { detectBinary, extraDirs, type DetectDeps } from "./detect.js";

const deps = (found: string[], answers: Record<string, { code: number; out: string }>, platform: NodeJS.Platform = "darwin"): DetectDeps => ({
  platform,
  find: () => found,
  version: (p) => Promise.resolve(answers[p] ?? { code: 1, out: "nope" }),
});

describe("detectBinary", () => {
  it("reports the first copy that answers --version", async () => {
    const result = await detectBinary(
      "cloudflare",
      deps(["/shim/cloudflared", "/opt/homebrew/bin/cloudflared"], {
        "/shim/cloudflared": { code: 1, out: "No version is set" },
        "/opt/homebrew/bin/cloudflared": { code: 0, out: "cloudflared version 2025.10.1 (built 2025-10-01)" },
      }),
    );
    expect(result).toEqual({ provider: "cloudflare", installed: true, path: "/opt/homebrew/bin/cloudflared", version: "2025.10.1" });
  });

  it("says how to install when nothing is found, per OS, and never installs anything itself", async () => {
    expect(await detectBinary("cloudflare", deps([], {}, "darwin"))).toMatchObject({ installed: false, installCommand: "brew install cloudflared" });
    expect(await detectBinary("ngrok", deps([], {}, "darwin"))).toMatchObject({ installCommand: "brew install ngrok/ngrok/ngrok" });
    expect(await detectBinary("ngrok", deps([], {}, "win32"))).toMatchObject({ installCommand: expect.stringContaining("winget install") });
    expect((await detectBinary("cloudflare", deps([], {}, "linux"))).installCommand).toMatch(/Download cloudflared/);
  });

  it("calls a program that will not run unusable, with its own words", async () => {
    const result = await detectBinary("ngrok", deps(["/usr/local/bin/ngrok"], { "/usr/local/bin/ngrok": { code: 1, out: "dyld: bad CPU type" } }));
    expect(result).toMatchObject({ installed: false });
    expect(result.detail).toContain("dyld: bad CPU type");
  });
});

describe("extraDirs", () => {
  it("covers the usual install places", () => {
    expect(extraDirs("darwin", "/Users/a", {})).toEqual(expect.arrayContaining(["/opt/homebrew/bin", "/usr/local/bin", "/Users/a/.cloudflared", "/Users/a/bin"]));
    expect(extraDirs("win32", "C:\\Users\\a", { ProgramFiles: "C:\\Program Files" })).toEqual(
      expect.arrayContaining([expect.stringContaining("cloudflared"), expect.stringContaining("WinGet")]),
    );
  });
});
