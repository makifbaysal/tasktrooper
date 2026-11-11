import os from "node:os";
import path from "node:path";
import type { RemoteBinaryDetection, RemoteProvider } from "../../ipc/types.js";
import { parseVersion, runProbe, searchDirs, whichAll } from "../services/detect.js";

/**
 * Finding `cloudflared` / `ngrok` on this computer, and what to tell a person who
 * has neither. Nothing is downloaded or installed here: the install line is
 * shown, the download page opens in the real browser, and the person runs it.
 */

interface ProviderFacts {
  binary: string;
  page: string;
  /** What to run, per OS, display only. */
  install: Partial<Record<NodeJS.Platform, string>>;
  fallbackInstall: string;
}

export const PROVIDER_FACTS: Record<RemoteProvider, ProviderFacts> = {
  cloudflare: {
    binary: "cloudflared",
    page: "https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/",
    install: {
      darwin: "brew install cloudflared",
      win32: "winget install --id Cloudflare.cloudflared",
    },
    fallbackInstall: "Download cloudflared for your system from the Cloudflare page.",
  },
  ngrok: {
    binary: "ngrok",
    page: "https://ngrok.com/download",
    install: {
      darwin: "brew install ngrok/ngrok/ngrok",
      win32: "winget install --id Ngrok.Ngrok",
      linux: "snap install ngrok",
    },
    fallbackInstall: "Download ngrok for your system from the ngrok page.",
  },
};

export function installPage(provider: RemoteProvider): string {
  return PROVIDER_FACTS[provider].page;
}

/** Where these programs land when the GUI's PATH does not include them. */
export function extraDirs(platform: NodeJS.Platform, home: string, env: NodeJS.ProcessEnv): string[] {
  if (platform === "win32") {
    const programFiles = env.ProgramFiles ?? "C:\\Program Files";
    const programFilesX86 = env["ProgramFiles(x86)"] ?? "C:\\Program Files (x86)";
    const localAppData = env.LOCALAPPDATA ?? path.join(home, "AppData", "Local");
    return [
      path.join(programFiles, "cloudflared"),
      path.join(programFilesX86, "cloudflared"),
      path.join(programFiles, "ngrok"),
      path.join(localAppData, "Microsoft", "WinGet", "Links"),
      path.join(localAppData, "ngrok"),
      path.join(home, "bin"),
    ];
  }
  return [
    "/opt/homebrew/bin",
    "/usr/local/bin",
    "/snap/bin",
    "/usr/bin",
    path.join(home, ".cloudflared"),
    path.join(home, "bin"),
    path.join(home, ".local", "bin"),
  ];
}

export interface DetectDeps {
  find(binary: string): string[];
  version(binaryPath: string): Promise<{ code: number; out: string }>;
  platform: NodeJS.Platform;
}

export function realDetectDeps(): DetectDeps {
  return {
    platform: process.platform,
    find: (binary) => {
      const dirs = [
        ...searchDirs(),
        ...extraDirs(process.platform, os.homedir(), process.env).map((dir) => ({ dir, source: "home" as const })),
      ];
      return whichAll(binary, dirs).map((found) => found.path);
    },
    version: async (binaryPath) => {
      const answer = await runProbe(binaryPath, ["--version"]);
      return { code: answer.code, out: answer.out };
    },
  };
}

export async function detectBinary(provider: RemoteProvider, deps: DetectDeps = realDetectDeps()): Promise<RemoteBinaryDetection> {
  const facts = PROVIDER_FACTS[provider];
  const installCommand = facts.install[deps.platform] ?? facts.fallbackInstall;
  const candidates = deps.find(facts.binary);
  if (candidates.length === 0) {
    return { provider, installed: false, installCommand, detail: `${facts.binary} was not found on this computer.` };
  }
  let firstFailure: string | undefined;
  for (const found of candidates.slice(0, 4)) {
    const answer = await deps.version(found);
    if (answer.code === 0) {
      const version = parseVersion(answer.out) ?? undefined;
      return { provider, installed: true, path: found, ...(version ? { version } : {}) };
    }
    firstFailure ??= `${path.basename(found)} --version failed: ${answer.out.split("\n")[0]?.trim() ?? ""}`.trim();
  }
  return { provider, installed: false, installCommand, detail: firstFailure ?? `${facts.binary} did not answer.` };
}
