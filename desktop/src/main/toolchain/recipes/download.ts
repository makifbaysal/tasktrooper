import { createReadStream, createWriteStream } from "node:fs";
import { chmod, copyFile, mkdir, readdir, rename, rm } from "node:fs/promises";
import path from "node:path";
import { pipeline } from "node:stream/promises";
import { createGunzip } from "node:zlib";
import type { HostArch, HostFacts, InstallSpec, StepContext } from "../types.js";
import { InstallError } from "../types.js";
import { userBinDir } from "../locations.js";
import { execOrFail, manualInstaller, workFile, type InstallerFactory } from "./shared.js";

/**
 * The two recipes that start from a file on a vendor's server.
 *
 * `script`: the vendor's own installer script, from a fixed https URL in the
 * table, saved to a file and then run by an interpreter given that file. Never
 * `curl … | sh`: the script that runs is the script that was saved, whole,
 * and it is still on disk afterwards for anybody who wants to read it.
 *
 * `binary-download`: one pinned release asset, checked against the digest the
 * release publishes, placed in a per-user directory — no administrator rights
 * and nothing outside the person's own profile. An archive is unpacked in
 * `userData/toolchain` first and only the named programs are copied out.
 */

const SCRIPT_EXT = { bash: "sh", sh: "sh", powershell: "ps1" } as const;

export const script: InstallerFactory<"script"> = (spec, tool) => {
  const shown =
    spec.interpreter === "powershell"
      ? `powershell -File install.ps1 (downloaded from ${spec.url})`
      : `${spec.interpreter} install.sh (downloaded from ${spec.url})`;
  return {
    installKind: "auto",
    method: "Official installer",
    command: shown,
    url: tool.docsUrl,
    dependsOn: [],
    run: async (ctx) => {
      const file = workFile(ctx, `install.${SCRIPT_EXT[spec.interpreter]}`);
      ctx.progress({ detail: `Downloading ${spec.url}…` });
      await ctx.download(spec.url, file);
      ctx.progress({ detail: "Running the official installer…" });
      if (spec.interpreter === "powershell") {
        const powershell = ctx.host.which("powershell") ?? "powershell.exe";
        await execOrFail(ctx, powershell, ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", file], "The installer");
        return;
      }
      await execOrFail(ctx, spec.interpreter === "bash" ? "/bin/bash" : "/bin/sh", [file], "The installer");
    },
  };
};

export function hostArch(host: HostFacts): HostArch | null {
  return host.arch === "x64" || host.arch === "arm64" ? host.arch : null;
}

type BinarySpec = Extract<InstallSpec, { kind: "binary-download" }>;

async function findIn(dir: string, name: string): Promise<string | null> {
  const entries = await readdir(dir, { recursive: true });
  const hit = entries.find((entry) => path.basename(entry) === name);
  return hit === undefined ? null : path.join(dir, hit);
}

async function gunzipTo(archive: string, dest: string): Promise<void> {
  const part = `${dest}.part`;
  try {
    await pipeline(createReadStream(archive), createGunzip(), createWriteStream(part));
    await rename(part, dest);
  } catch (err) {
    await rm(part, { force: true });
    throw new InstallError(`Unpacking ${path.basename(archive)} failed: ${err instanceof Error ? err.message : String(err)}`);
  }
}

async function unpack(ctx: StepContext, spec: BinarySpec, archive: string, dir: string, exe: (name: string) => string): Promise<void> {
  if (spec.archive === "gz") {
    await gunzipTo(archive, path.join(dir, exe(spec.name)));
    return;
  }
  const scratch = workFile(ctx, "extract");
  await rm(scratch, { recursive: true, force: true });
  await mkdir(scratch, { recursive: true });
  try {
    await execOrFail(ctx, ctx.host.which("tar") ?? "tar", ["-xzf", archive, "-C", scratch], `Unpacking ${path.basename(archive)}`);
    for (const name of spec.files ?? [spec.name]) {
      const found = await findIn(scratch, exe(name));
      if (!found) throw new InstallError(`${exe(name)} was not in ${path.basename(archive)}.`);
      await copyFile(found, path.join(dir, exe(name)));
    }
  } finally {
    await rm(scratch, { recursive: true, force: true });
  }
}

export const binaryDownload: InstallerFactory<"binary-download"> = (spec, tool, host) => {
  const arch = hostArch(host);
  const asset = arch ? spec.assets[arch] : undefined;
  if (!asset) return manualInstaller(tool.docsUrl, "Download");
  const win = host.platform === "win32";
  const exe = (name: string): string => (win ? `${name}.exe` : name);
  const join = win ? path.win32.join : path.posix.join;
  const dir = userBinDir(host, spec.name);
  const programs = (spec.archive === "tar.gz" ? (spec.files ?? [spec.name]) : [spec.name]).map(exe);
  return {
    installKind: "auto",
    method: "GitHub release",
    command: `download ${asset.url} → ${programs.map((p) => join(dir, p)).join(", ")}`,
    url: tool.docsUrl,
    dependsOn: [],
    run: async (ctx) => {
      await mkdir(dir, { recursive: true });
      ctx.progress({ detail: `Downloading ${spec.name}…`, percent: 0 });
      const target = spec.archive ? workFile(ctx, path.posix.basename(new URL(asset.url).pathname)) : join(dir, exe(spec.name));
      await ctx.download(asset.url, target, {
        ...(asset.sha256 ? { sha256: asset.sha256 } : {}),
        ...(asset.sha1 ? { sha1: asset.sha1 } : {}),
        onPercent: (p) => ctx.progress({ percent: spec.archive ? Math.round(p * 0.9) : p }),
      });
      if (spec.archive) {
        ctx.progress({ detail: `Unpacking ${spec.name}…`, percent: 90 });
        try {
          await unpack(ctx, spec, target, dir, exe);
        } finally {
          await rm(target, { force: true });
        }
      }
      if (!win) for (const program of programs) await chmod(join(dir, program), 0o755);
    },
  };
};
