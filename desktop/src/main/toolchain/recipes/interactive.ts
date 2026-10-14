import { chmod, writeFile } from "node:fs/promises";
import type { InstallSpec, StepContext } from "../types.js";
import { InstallError } from "../types.js";
import { shellQuote, workFile, type InstallerFactory } from "./shared.js";

/**
 * The installs only the person can finish: macOS's own command line tools
 * dialog, and Homebrew's installer, which asks for a password in a terminal
 * and refuses to run as root — so it is opened in Terminal for them rather
 * than run here with elevated rights. The step then waits, re-checking until
 * the tool appears.
 *
 * Homebrew's installer is downloaded first, like every other script here, and
 * the Terminal runs that file: what the person watches run is what was
 * fetched, and nothing is piped from the network into a shell.
 */

export const INTERACTIVE_POLL_MS = 5_000;
export const INTERACTIVE_TIMEOUT_MS = 30 * 60_000;

const DONE = "Homebrew is installed. You can close this window and go back to TaskTrooper.";

async function waitForTool(ctx: StepContext, detail: string): Promise<void> {
  ctx.progress({ waiting: true, detail });
  for (let waited = 0; waited < INTERACTIVE_TIMEOUT_MS; waited += INTERACTIVE_POLL_MS) {
    await ctx.sleep(INTERACTIVE_POLL_MS);
    if ((await ctx.detect()).status === "installed") return;
  }
  throw new InstallError(`Gave up after ${INTERACTIVE_TIMEOUT_MS / 60_000} minutes waiting for ${ctx.label} to finish installing.`);
}

type Interactive = Extract<InstallSpec, { kind: "interactive" }>;

/** The `.command` Terminal opens: run the downloaded installer, and say so plainly if it is not there. */
export function terminalScript(installer: string): string {
  const file = shellQuote(installer);
  return [
    "#!/bin/bash",
    `if [ ! -f ${file} ]; then`,
    `  echo "The installer TaskTrooper downloaded is missing: "${file} >&2`,
    "  echo 'Go back to TaskTrooper and start the install again.' >&2",
    "  exit 1",
    "fi",
    `/bin/bash ${file}`,
    "status=$?",
    "echo",
    `[ "$status" -eq 0 ] && echo ${shellQuote(DONE)}`,
    "exit $status",
    "",
  ].join("\n");
}

export const interactive: InstallerFactory<"interactive"> = (spec: Interactive, tool) => {
  if (spec.via === "xcode-select") {
    return {
      installKind: "interactive",
      method: "macOS installer",
      command: "xcode-select --install",
      url: tool.docsUrl,
      dependsOn: [],
      run: async (ctx) => {
        // Exits at once: 0 once the dialog is up, 1 when macOS thinks they are
        // already installed — detection, not this code, is the answer.
        await ctx.exec("/usr/bin/xcode-select", ["--install"]);
        await waitForTool(ctx, "Finish the installer macOS opened; this continues once the tools are in place.");
      },
    };
  }
  return {
    installKind: "interactive",
    method: "Terminal",
    command: `/bin/bash install.sh in Terminal (downloaded from ${spec.scriptUrl})`,
    url: tool.docsUrl,
    dependsOn: [],
    run: async (ctx) => {
      const installer = workFile(ctx, "install.sh");
      ctx.progress({ detail: `Downloading ${spec.scriptUrl}…` });
      await ctx.download(spec.scriptUrl, installer);
      const command = workFile(ctx, "install.command");
      await writeFile(command, terminalScript(installer), { mode: 0o755 });
      await chmod(command, 0o755);
      const code = await ctx.exec("/usr/bin/open", ["-a", "Terminal", command]);
      if (code !== 0) throw new InstallError("Terminal could not be opened for the installer.");
      await waitForTool(ctx, "Finish the installer in the Terminal window (it asks for your password); this continues once it is done.");
    },
  };
};
