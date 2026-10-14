import path from "node:path";
import type { ToolchainId } from "../../../ipc/types.js";
import {
  InstallError,
  type ExecOptions,
  type HostFacts,
  type InstallSpec,
  type Installer,
  type StepContext,
  type ToolDefinition,
} from "../types.js";

/** Turns one table entry into an installer for this host. Pure: no process, no file. */
export type InstallerFactory<K extends InstallSpec["kind"]> = (
  spec: Extract<InstallSpec, { kind: K }>,
  tool: ToolDefinition,
  host: HostFacts,
) => Installer;

/**
 * Nothing this app can run; the person installs it from `url`. Also what a
 * recipe degrades to when its channel is not on this machine (no winget, no
 * pkexec), so the item still says where to go instead of failing to start.
 */
export function manualInstaller(url: string, method: string, command?: string, dependsOn: readonly ToolchainId[] = []): Installer {
  return {
    installKind: "manual",
    method,
    ...(command !== undefined ? { command } : {}),
    url,
    dependsOn,
    run: () => Promise.reject(new InstallError(`This one is installed by hand: ${url}`)),
  };
}

/**
 * Exit codes are the installer's own; the sentence is the last thing it
 * printed, which is nearly always the reason.
 */
export async function execOrFail(
  ctx: StepContext,
  command: string,
  args: readonly string[],
  what: string,
  opts: ExecOptions = {},
): Promise<void> {
  let last = "";
  const code = await ctx.exec(command, args, {
    ...opts,
    onLine: (line) => {
      if (line.trim() !== "") last = line.trim();
      opts.onLine?.(line);
    },
  });
  if (code !== 0) throw new InstallError(`${what} exited with code ${code}${last === "" ? "." : `: ${last}`}`);
}

/** A command an earlier step was supposed to provide, looked up when this step runs. */
export function requireCommand(ctx: StepContext, name: string, providedBy: string): string {
  const found = ctx.host.which(name);
  if (!found) throw new InstallError(`${name} was not found; ${providedBy} has to be installed first.`);
  return found;
}

/** `userData/toolchain/<id>-<name>`: one file per tool, so two tools never share a download. */
export function workFile(ctx: StepContext, name: string): string {
  return path.join(ctx.workDir, `${ctx.id}-${name}`);
}

export const shellQuote = (value: string): string => `'${value.replaceAll("'", `'\\''`)}'`;
