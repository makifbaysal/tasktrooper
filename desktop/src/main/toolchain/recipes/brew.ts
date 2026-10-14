import { chmod, writeFile } from "node:fs/promises";
import type { HostFacts, StepContext } from "../types.js";
import { InstallError } from "../types.js";
import { execOrFail, workFile, type InstallerFactory } from "./shared.js";

/**
 * Homebrew formulae and casks, unattended.
 *
 * `NONINTERACTIVE` stops brew from waiting on a keypress nobody can give it.
 * A cask may have to write outside the Homebrew prefix (Docker's CLI links in
 * /usr/local/bin), and sudo asks for a password it cannot read without a
 * terminal — so for casks only, `SUDO_ASKPASS` points it at a native macOS
 * password dialog naming the cask. The password goes from that dialog to
 * sudo over a pipe: never argv, never this process. Cancel exits non-zero,
 * which sudo takes as no password and stops; a mistyped one is asked again by
 * sudo itself, up to its own limit.
 */

const BREW_ENV = {
  NONINTERACTIVE: "1",
  HOMEBREW_NO_ENV_HINTS: "1",
  HOMEBREW_NO_INSTALL_CLEANUP: "1",
};

/** Apple Silicon's prefix first, Intel's second: the order Homebrew itself prefers. */
export const BREW_CANDIDATES = ["/opt/homebrew/bin/brew", "/usr/local/bin/brew"];

export function brewBinary(host: HostFacts): string | null {
  for (const candidate of BREW_CANDIDATES) {
    const found = host.which(candidate);
    if (found) return found;
  }
  return host.which("brew");
}

/** Only what may sit inside both an AppleScript string and a shell single-quoted one. */
const plain = (text: string): string => text.replace(/[^A-Za-z0-9 ._+/-]/g, "");

export function askpassScript(label: string, cask: string): string {
  const prompt = `TaskTrooper is installing ${plain(label)} (Homebrew cask ${plain(cask)}). macOS needs your password to finish.`;
  return [
    "#!/bin/sh",
    `answer=$(/usr/bin/osascript -e 'text returned of (display dialog "${prompt}" default answer "" with hidden answer with title "TaskTrooper" with icon caution)') || exit 1`,
    `printf '%s\\n' "$answer"`,
    "",
  ].join("\n");
}

async function askpass(ctx: StepContext, label: string, cask: string): Promise<string> {
  const file = workFile(ctx, "askpass.sh");
  await writeFile(file, askpassScript(label, cask), { mode: 0o700 });
  await chmod(file, 0o700);
  return file;
}

function requireBrew(ctx: StepContext): string {
  const brew = brewBinary(ctx.host);
  if (!brew) throw new InstallError("Homebrew was not found; it has to be installed first.");
  return brew;
}

export const brewFormula: InstallerFactory<"brew"> = (spec, tool) => ({
  installKind: "auto",
  method: "Homebrew",
  command: `brew install ${spec.formula}`,
  url: tool.docsUrl,
  dependsOn: ["homebrew"],
  run: async (ctx) => {
    const brew = requireBrew(ctx);
    ctx.progress({ detail: `Running brew install ${spec.formula}…` });
    await execOrFail(ctx, brew, ["install", spec.formula], `brew install ${spec.formula}`, { env: BREW_ENV });
  },
});

export const brewCask: InstallerFactory<"brew-cask"> = (spec, tool) => ({
  installKind: "auto",
  method: "Homebrew",
  command: `brew install --cask ${spec.cask}`,
  url: tool.docsUrl,
  dependsOn: ["homebrew"],
  run: async (ctx) => {
    const brew = requireBrew(ctx);
    ctx.progress({
      detail: `Running brew install --cask ${spec.cask}… macOS may ask for your password, and again if it is mistyped.`,
    });
    await execOrFail(ctx, brew, ["install", "--cask", spec.cask], `brew install --cask ${spec.cask}`, {
      env: { ...BREW_ENV, SUDO_ASKPASS: await askpass(ctx, tool.label, spec.cask) },
    });
  },
});
