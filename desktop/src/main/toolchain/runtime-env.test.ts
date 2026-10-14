import { describe, expect, it } from "vitest";
import type { ToolchainReport } from "../../ipc/types.js";
import { installedRuntime, mergeRuntime, runtimeFromReport, withRuntime, type Disk } from "./runtime-env.js";

/**
 * What a tool reported Installed needs to be reachable by the agents: its
 * directory on PATH and, for the JDK, JAVA_HOME. From a report, and from the
 * install locations on a fake disk, per OS.
 */

const item = (id: ToolchainReport["items"][number]["id"], path?: string, status: "installed" | "missing" = "installed") => ({
  id,
  label: id,
  status,
  ...(path ? { path } : {}),
  installKind: "auto" as const,
  dependsOn: [],
});

describe("runtimeFromReport", () => {
  it("takes the directory of each installed tool the agents run, and JAVA_HOME from the JDK", () => {
    const report: ToolchainReport = {
      generatedAt: 1,
      platform: "darwin",
      items: [
        item("node", "/opt/homebrew/bin/node"),
        item("jdk", "/opt/homebrew/opt/openjdk@17/bin/java"),
        item("osv-scanner", "/Users/me/.local/bin/osv-scanner"),
        item("docker", "/Applications/Docker.app"),
        item("go", undefined, "missing"),
        item("python", "/opt/homebrew/bin/python3"),
      ],
    };
    const exposure = (id: string) => (id === "jdk" ? ("java-home" as const) : id === "python" ? undefined : ("bin-dir" as const));
    expect(runtimeFromReport(report, exposure, (java) => `${java}-home`)).toEqual({
      pathDirs: ["/opt/homebrew/bin", "/opt/homebrew/opt/openjdk@17/bin", "/Users/me/.local/bin"],
      javaHome: "/opt/homebrew/opt/openjdk@17/bin/java-home",
    });
    expect(runtimeFromReport(null, exposure, (j) => j)).toEqual({ pathDirs: [] });
  });
});

function disk(dirs: string[], listings: Record<string, string[]> = {}): Disk {
  return { isDir: (d) => dirs.includes(d), list: (d) => listings[d] ?? [] };
}

describe("installedRuntime", () => {
  it("finds the keg-only openjdk@17 and its JAVA_HOME on a Mac", () => {
    const keg = "/opt/homebrew/opt/openjdk@17";
    const runtime = installedRuntime(
      { platform: "darwin", home: "/Users/me", env: {} },
      disk([keg, `${keg}/libexec/openjdk.jdk/Contents/Home`, "/Users/me/.local/bin", "/Users/me/.duckdb/cli/latest"]),
    );
    expect(runtime).toEqual({
      pathDirs: ["/Users/me/.local/bin", "/Users/me/.duckdb/cli/latest", `${keg}/bin`],
      javaHome: `${keg}/libexec/openjdk.jdk/Contents/Home`,
    });
  });

  it("finds OSV-Scanner's own folder, winget's links and a Temurin 17 on Windows", () => {
    const local = "C:\\Users\\me\\AppData\\Local";
    const adoptium = "C:\\Program Files\\Eclipse Adoptium";
    const runtime = installedRuntime(
      { platform: "win32", home: "C:\\Users\\me", env: { LOCALAPPDATA: local, ProgramFiles: "C:\\Program Files" } },
      disk([`${local}\\Programs\\osv-scanner`, `${local}\\Microsoft\\WinGet\\Links`], { [adoptium]: ["jdk-11.0.2-hotspot", "jdk-17.0.12.7-hotspot"] }),
    );
    expect(runtime.pathDirs).toEqual([
      `${local}\\Programs\\osv-scanner`,
      `${local}\\Microsoft\\WinGet\\Links`,
      `${adoptium}\\jdk-17.0.12.7-hotspot\\bin`,
    ]);
    expect(runtime.javaHome).toBe(`${adoptium}\\jdk-17.0.12.7-hotspot`);
  });

  it("finds a distribution's JDK 17 on Linux, and nothing that is not there", () => {
    const runtime = installedRuntime(
      { platform: "linux", home: "/home/me", env: {} },
      disk([], { "/usr/lib/jvm": ["java-11-openjdk-amd64", "java-17-openjdk-amd64", "default-java"] }),
    );
    expect(runtime).toEqual({ pathDirs: ["/usr/lib/jvm/java-17-openjdk-amd64/bin"], javaHome: "/usr/lib/jvm/java-17-openjdk-amd64" });
  });
});

describe("withRuntime", () => {
  it("appends without duplicating and prefers the report's JAVA_HOME when merged", () => {
    const merged = mergeRuntime({ pathDirs: ["/a"], javaHome: "/jdk-report" }, { pathDirs: ["/a", "/b"], javaHome: "/jdk-disk" });
    expect(merged).toEqual({ pathDirs: ["/a", "/b"], javaHome: "/jdk-report" });
    expect(withRuntime({ PATH: "/usr/bin:/a" }, merged, "linux")).toEqual({ PATH: "/usr/bin:/a:/b", JAVA_HOME: "/jdk-report" });
    expect(withRuntime({ PATH: "/usr/bin", JAVA_HOME: "/mine" }, merged, "linux").JAVA_HOME).toBe("/mine");
  });
});
