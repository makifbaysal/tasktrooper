import { refreshWindowsPath } from "../winpath.js";
import { execOrFail, manualInstaller, type InstallerFactory } from "./shared.js";

/**
 * winget, unattended. `--silent` and the two `--accept-*` flags are what make
 * it run without a window; an installer that needs elevation still raises
 * Windows' own UAC prompt, which is the person's to answer.
 *
 * A non-zero exit is not the last word: winget reports a pending reboot as a
 * failure for a package that did install, and the job's detection afterwards
 * is what decides (`job.ts`). The PATH is re-read either way.
 */

const ARGS = ["--silent", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"];

export const winget: InstallerFactory<"winget"> = (spec, tool, host) => {
  if (!host.which("winget")) return manualInstaller(tool.docsUrl, "Download");
  return {
    installKind: "auto",
    method: "winget",
    command: `winget install --id ${spec.id} -e`,
    url: tool.docsUrl,
    dependsOn: [],
    run: async (ctx) => {
      const bin = ctx.host.which("winget") ?? "winget";
      ctx.progress({ detail: `Running winget install ${spec.id}…` });
      try {
        await execOrFail(ctx, bin, ["install", "--id", spec.id, "-e", ...ARGS], `winget install ${spec.id}`);
      } finally {
        await refreshWindowsPath();
      }
    },
  };
};
