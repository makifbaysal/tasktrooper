import { describe, expect, it, vi } from "vitest";
import { TOOLCHAIN_IDS, type ToolchainId } from "../../ipc/types.js";
import { TOOLCHAIN_PLATFORMS, type HostFacts, type Installer, type ToolchainPlatform } from "./types.js";

vi.mock("electron", () => ({
  app: { getAppPath: () => "/app", getPath: () => "/tmp/tt-registry", isPackaged: false },
}));

const { TOOLCHAIN_DEFINITIONS, applicableIds, appliesOn } = await import("./registry.js");
const { installerFor } = await import("./recipes/index.js");
const { topologicalOrder } = await import("./plan.js");
const { isOpenablePage } = await import("./service.js");

/**
 * The table's invariants, on every OS — not only the one the suite runs on.
 * Each host below is a fake machine: `everything` has every channel a recipe
 * could use (Homebrew, winget, pkexec + apt, snap), `nothing` has none, so
 * both what runs and what it degrades to are checked.
 */

const PLATFORMS = TOOLCHAIN_PLATFORMS;

function host(platform: ToolchainPlatform, present: (name: string) => boolean, arch = "x64"): HostFacts {
  return {
    platform,
    arch,
    home: platform === "win32" ? "C:\\Users\\me" : "/home/me",
    env: platform === "win32" ? { LOCALAPPDATA: "C:\\Users\\me\\AppData\\Local" } : {},
    which: (name) => (present(name) ? `/fake/${name}` : null),
  };
}

const everything = (platform: ToolchainPlatform, arch = "x64"): HostFacts => host(platform, () => true, arch);
const nothing = (platform: ToolchainPlatform): HostFacts => host(platform, () => false);

function installers(h: HostFacts): [ToolchainId, Installer][] {
  return applicableIds(h.platform).map((id) => {
    const installer = installerFor(TOOLCHAIN_DEFINITIONS[id], h);
    if (!installer) throw new Error(`${id} applies on ${h.platform} and has no installer`);
    return [id, installer];
  });
}

describe("the toolchain table", () => {
  it("defines every id, under its own id", () => {
    expect(Object.keys(TOOLCHAIN_DEFINITIONS).sort()).toEqual([...TOOLCHAIN_IDS].sort());
    for (const id of TOOLCHAIN_IDS) {
      expect(TOOLCHAIN_DEFINITIONS[id].id).toBe(id);
      expect(TOOLCHAIN_DEFINITIONS[id].label.trim()).not.toBe("");
      expect(isOpenablePage(TOOLCHAIN_DEFINITIONS[id].docsUrl), id).toBe(true);
    }
  });

  it("applies every id somewhere, and the Apple-only ones on macOS only", () => {
    for (const id of TOOLCHAIN_IDS) expect(PLATFORMS.some((p) => appliesOn(id, p)), id).toBe(true);
    for (const id of ["homebrew", "xcode", "xcode-clt", "cocoapods", "appium-xcuitest"] as const) {
      expect(PLATFORMS.filter((p) => appliesOn(id, p))).toEqual(["darwin"]);
    }
  });

  for (const platform of PLATFORMS) {
    describe(platform, () => {
      it("gives every applicable id an installer of a known kind", () => {
        for (const [id, installer] of installers(everything(platform))) {
          expect(["auto", "interactive", "manual"], id).toContain(installer.installKind);
          expect(installer.method.trim(), id).not.toBe("");
        }
        for (const id of TOOLCHAIN_IDS.filter((i) => !appliesOn(i, platform))) {
          expect(installerFor(TOOLCHAIN_DEFINITIONS[id], everything(platform)), id).toBeNull();
        }
      });

      it("shows a command for everything it would run", () => {
        for (const [id, installer] of installers(everything(platform))) {
          if (installer.installKind !== "manual") expect(installer.command?.trim() ?? "", id).not.toBe("");
        }
      });

      it("gives every manual one a page this app may open, with or without the tools a recipe uses", () => {
        for (const h of [everything(platform), nothing(platform), everything(platform, "arm64")]) {
          for (const [id, installer] of installers(h)) {
            expect(installer.url, id).toBeDefined();
            expect(isOpenablePage(installer.url ?? ""), `${id}: ${installer.url}`).toBe(true);
          }
        }
      });

      it("depends only on ids that exist and apply here, without a cycle", () => {
        const all = installers(everything(platform));
        const deps = new Map(all.map(([id, i]) => [id, i.dependsOn]));
        for (const [id, installer] of all) {
          for (const dep of installer.dependsOn) {
            expect(TOOLCHAIN_IDS, `${id} → ${dep}`).toContain(dep);
            expect(appliesOn(dep, platform), `${id} → ${dep} on ${platform}`).toBe(true);
            expect(dep, id).not.toBe(id);
          }
        }
        expect(() => topologicalOrder([...deps.keys()], (id) => deps.get(id) ?? [])).not.toThrow();
      });

      it("pins every download it fetches to https", () => {
        for (const id of applicableIds(platform)) {
          const spec = TOOLCHAIN_DEFINITIONS[id].recipes[platform]?.install;
          const urls: string[] = [];
          if (spec?.kind === "script") urls.push(spec.url);
          if (spec?.kind === "interactive" && spec.via === "terminal") urls.push(spec.scriptUrl);
          if (spec?.kind === "binary-download") urls.push(...Object.values(spec.assets).map((a) => a.url));
          if (spec?.kind === "android-sdk") urls.push(...Object.values(spec.archives).map((a) => a.url));
          for (const url of urls) expect(new URL(url).protocol, `${id}: ${url}`).toBe("https:");
          if (spec?.kind === "binary-download" || spec?.kind === "android-sdk") {
            const pinned = spec.kind === "android-sdk" ? Object.values(spec.archives) : Object.values(spec.assets);
            for (const asset of pinned) expect(asset.sha1 ?? asset.sha256, `${id}: ${asset.url}`).toMatch(/^[0-9a-f]{40,64}$/);
          }
        }
      });
    });
  }

  it("installs most tools unattended on every OS, not mostly by hand", () => {
    for (const platform of PLATFORMS) {
      const all = installers(everything(platform));
      const manual = all.filter(([, i]) => i.installKind === "manual").map(([id]) => id);
      expect(manual.length, `${platform}: ${manual.join(", ")}`).toBeLessThanOrEqual(3);
    }
  });

  it("falls back to manual, with a page, when winget or pkexec is not on the machine", () => {
    const windows = new Map(installers(nothing("win32")));
    expect(windows.get("git")?.installKind).toBe("manual");
    expect(windows.get("git")?.url).toBe("https://git-scm.com/downloads");

    const linuxNoPkexec = new Map(installers(host("linux", (name) => name === "apt-get")));
    expect(linuxNoPkexec.get("git")).toMatchObject({ installKind: "manual", command: "sudo apt-get install -y git" });

    const linuxDnf = new Map(installers(host("linux", (name) => name !== "apt-get")));
    expect(linuxDnf.get("jdk")?.command).toBe("pkexec dnf install -y java-17-openjdk-devel");
    expect(linuxDnf.get("docker")?.command).toBe("pkexec dnf install -y moby-engine");
  });

  it("keeps the arch-specific downloads to the arch they were built for", () => {
    expect(new Map(installers(everything("linux", "arm64"))).get("android-sdk")?.installKind).toBe("manual");
    expect(new Map(installers(everything("linux", "arm64"))).get("uv")?.installKind).toBe("auto");
    expect(new Map(installers(everything("linux", "arm64"))).get("osv-scanner")?.installKind).toBe("auto");
    expect(new Map(installers(everything("darwin", "arm64"))).get("android-sdk")?.installKind).toBe("auto");
  });

  it("names the channel a person recognises", () => {
    const mac = new Map(installers(everything("darwin")));
    expect(mac.get("node")).toMatchObject({ method: "Homebrew", command: "brew install node", dependsOn: ["homebrew"] });
    expect(mac.get("docker")?.command).toBe("brew install --cask docker-desktop");
    expect(mac.get("xcode")).toMatchObject({ installKind: "manual", method: "App Store" });
    expect(mac.get("homebrew")?.installKind).toBe("interactive");
    expect(mac.get("appium")).toMatchObject({ method: "npm", dependsOn: ["node"] });
    expect(mac.get("android-sdk")?.dependsOn).toEqual(["jdk"]);
    const win = new Map(installers(everything("win32")));
    expect(win.get("jdk")?.command).toBe("winget install --id EclipseAdoptium.Temurin.17.JDK -e");
    const linux = new Map(installers(everything("linux")));
    expect(linux.get("git")?.command).toBe("pkexec apt-get install -y git");
    expect(linux.get("flutter")?.command).toBe("pkexec snap install flutter --classic");
    expect(linux.get("chrome")?.command).toBe("pkexec snap install chromium");
    expect(linux.get("uv")?.method).toBe("GitHub release");
    expect(linux.get("duckdb")?.method).toBe("GitHub release");
  });

  it("takes git on a Mac from Apple's command line tools, with nothing of its own to run", () => {
    const git = new Map(installers(everything("darwin"))).get("git");
    expect(git).toMatchObject({ installKind: "interactive", method: "Xcode Command Line Tools", dependsOn: ["xcode-clt"] });
    expect(git?.dependsOn).not.toContain("homebrew");
  });

  it("never installs a downloaded package file as administrator", () => {
    for (const platform of PLATFORMS) {
      for (const id of applicableIds(platform)) {
        const spec = TOOLCHAIN_DEFINITIONS[id].recipes[platform]?.install;
        if (spec?.kind === "linux-pkg") {
          for (const pkg of [...spec.apt, ...spec.dnf]) expect(pkg, id).toMatch(/^[a-z0-9][a-z0-9.+-]*$/);
        }
      }
    }
  });

  it("says how every tool the runtime reads reaches the agents' processes", () => {
    const elsewhere = new Set(["chrome", "android-sdk", "appium-xcuitest", "appium-uiautomator2"]);
    for (const id of TOOLCHAIN_IDS) {
      const def = TOOLCHAIN_DEFINITIONS[id];
      if (def.affectsRuntime && !elsewhere.has(id)) expect(def.runtime, id).toBeDefined();
      if (def.runtime) expect(def.affectsRuntime, id).toBe(true);
    }
    expect(TOOLCHAIN_DEFINITIONS.jdk.runtime).toBe("java-home");
  });
});
