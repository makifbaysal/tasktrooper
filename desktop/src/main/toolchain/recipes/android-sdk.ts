import { existsSync, statSync } from "node:fs";
import { chmod, cp, mkdir, readdir, rename, rm } from "node:fs/promises";
import path from "node:path";
import { androidRootFrom, androidSdkDefaultRoot, probeAndroidSdk } from "../../services/detect.js";
import { javaHomeOf } from "../detectors.js";
import type { StepContext, ToolchainPlatform } from "../types.js";
import { InstallError } from "../types.js";
import { hostArch } from "./download.js";
import { execOrFail, manualInstaller, workFile, type InstallerFactory } from "./shared.js";

/**
 * The Android SDK without Android Studio and without administrator rights:
 * Google's command-line tools into `<sdk>/cmdline-tools/latest`, then
 * `sdkmanager` for the packages an emulator run needs.
 *
 * An SDK that is already here (Android Studio's, or one `ANDROID_HOME` names)
 * is completed in place rather than shadowed by a second one: two SDKs on one
 * machine is how adb talks to one emulator's server and the agent to another.
 */

function isDir(dir: string): boolean {
  try {
    return statSync(dir).isDirectory();
  } catch {
    return false;
  }
}

export function sdkRoot(env: Readonly<NodeJS.ProcessEnv> = process.env): string {
  const found = androidRootFrom(probeAndroidSdk().path);
  if (found !== "") return found;
  for (const key of ["ANDROID_HOME", "ANDROID_SDK_ROOT"]) {
    const value = (env[key] ?? "").trim();
    if (value !== "" && isDir(value)) return value;
  }
  return androidSdkDefaultRoot();
}

function sdkmanagerPath(root: string, platform: ToolchainPlatform): string {
  return path.join(root, "cmdline-tools", "latest", "bin", platform === "win32" ? "sdkmanager.bat" : "sdkmanager");
}

async function moveDir(from: string, to: string): Promise<void> {
  try {
    await rename(from, to);
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code !== "EXDEV") throw err;
    await cp(from, to, { recursive: true });
    await rm(from, { recursive: true, force: true });
  }
}

/**
 * `unzip` keeps the execute bits; a JDK's `jar` (the Linux fallback when
 * `unzip` is not installed) does not, so the launchers are made executable
 * again afterwards.
 */
async function extract(ctx: StepContext, zip: string, into: string, javaHome: string): Promise<void> {
  if (ctx.host.platform === "win32") {
    const tar = path.win32.join(ctx.host.env.SystemRoot ?? "C:\\Windows", "System32", "tar.exe");
    await execOrFail(ctx, tar, ["-xf", zip, "-C", into], "Extracting the command-line tools");
    return;
  }
  const unzip = ctx.host.which("unzip");
  if (unzip) {
    await execOrFail(ctx, unzip, ["-q", "-o", zip, "-d", into], "Extracting the command-line tools");
    return;
  }
  await execOrFail(ctx, path.join(javaHome, "bin", "jar"), ["xf", zip], "Extracting the command-line tools", { cwd: into });
  const bin = path.join(into, "cmdline-tools", "bin");
  for (const name of await readdir(bin)) await chmod(path.join(bin, name), 0o755);
}

/** sdkmanager draws one 0–100% bar per package; this folds them into one rising number. */
export function packageProgress(packages: number, from: number, to: number): (line: string) => number | null {
  let index = 0;
  let lastPct = 0;
  let best = from;
  return (line) => {
    const m = /\]\s*(\d{1,3})%/.exec(line);
    if (!m) return null;
    const pct = Math.min(100, Number.parseInt(m[1] ?? "0", 10));
    if (pct + 50 < lastPct) index = Math.min(index + 1, Math.max(packages - 1, 0));
    lastPct = pct;
    const value = Math.round(from + ((index + pct / 100) / Math.max(packages, 1)) * (to - from));
    best = Math.max(best, Math.min(value, to));
    return best;
  };
}

export const androidSdk: InstallerFactory<"android-sdk"> = (spec, tool, host) => {
  const arch = hostArch(host);
  const archive = arch ? spec.archives[arch] : undefined;
  if (!archive) return manualInstaller(tool.docsUrl, "Android Studio");
  return {
    installKind: "auto",
    method: "Android sdkmanager",
    command: `sdkmanager ${spec.packages.map((p) => (p.includes(";") ? `"${p}"` : p)).join(" ")}`,
    url: tool.docsUrl,
    dependsOn: ["jdk"],
    run: async (ctx) => {
      const java = await ctx.detect("jdk");
      if (java.status !== "installed" || !java.path) throw new InstallError("Java 17 or newer is needed for sdkmanager and was not found.");
      const javaHome = javaHomeOf(java.path);
      const root = sdkRoot(ctx.host.env);
      const sdkmanager = sdkmanagerPath(root, ctx.host.platform);

      if (!existsSync(sdkmanager)) {
        const zip = workFile(ctx, "commandlinetools.zip");
        ctx.progress({ detail: "Downloading the Android command-line tools…", percent: 0 });
        await ctx.download(archive.url, zip, {
          ...(archive.sha1 ? { sha1: archive.sha1 } : {}),
          ...(archive.sha256 ? { sha256: archive.sha256 } : {}),
          onPercent: (p) => ctx.progress({ percent: Math.round(p * 0.4) }),
        });
        ctx.progress({ detail: "Extracting the command-line tools…", percent: 40 });
        const scratch = workFile(ctx, "extract");
        await rm(scratch, { recursive: true, force: true });
        await mkdir(scratch, { recursive: true });
        await extract(ctx, zip, scratch, javaHome);
        const latest = path.dirname(path.dirname(sdkmanager));
        await mkdir(path.dirname(latest), { recursive: true });
        await rm(latest, { recursive: true, force: true });
        await moveDir(path.join(scratch, "cmdline-tools"), latest);
        await rm(scratch, { recursive: true, force: true });
        await rm(zip, { force: true });
      }

      const env = { JAVA_HOME: javaHome, ANDROID_HOME: root, ANDROID_SDK_ROOT: root };
      ctx.progress({ detail: "Accepting the Android SDK licenses…", percent: 42 });
      await execOrFail(ctx, sdkmanager, [`--sdk_root=${root}`, "--licenses"], "sdkmanager --licenses", {
        env,
        input: "y\n".repeat(64),
      });
      ctx.progress({ detail: `Installing ${spec.packages.join(", ")}…`, percent: 45 });
      const progress = packageProgress(spec.packages.length, 45, 99);
      await execOrFail(ctx, sdkmanager, [`--sdk_root=${root}`, ...spec.packages], "sdkmanager", {
        env,
        onLine: (line) => {
          const percent = progress(line);
          if (percent !== null) ctx.progress({ percent });
        },
      });
    },
  };
};
