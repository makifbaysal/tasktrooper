import type { ToolchainJobState, ToolchainReport } from "../../ipc/types.js";

/**
 * What the account's web app — a page on another origin — sees of this
 * computer's toolchain. Paths are dropped from the report and the home
 * directory is written `~` wherever it appears in a sentence, a command or a
 * log line: it carries the person's account name, and the remote page needs
 * what is installed, not where.
 */

function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** `home` in either slash direction, and case-insensitively on Windows, becomes `~`. */
export function redactHome(text: string, home: string, platform: NodeJS.Platform = process.platform): string {
  if (home === "" || home === "/") return text;
  const spellings = new Set([home, home.replace(/\\/g, "/"), home.replace(/\//g, "\\")]);
  let out = text;
  for (const spelling of spellings) {
    out = out.replace(new RegExp(escapeRegExp(spelling), platform === "win32" ? "gi" : "g"), "~");
  }
  return out;
}

export function reportForRemote(report: ToolchainReport, home: string, platform?: NodeJS.Platform): ToolchainReport {
  const r = (text: string): string => redactHome(text, home, platform);
  return {
    ...report,
    items: report.items.map(({ path: _path, ...item }) => ({
      ...item,
      ...(item.detail !== undefined ? { detail: r(item.detail) } : {}),
      ...(item.command !== undefined ? { command: r(item.command) } : {}),
    })),
  };
}

export function jobForRemote(state: ToolchainJobState, home: string, platform?: NodeJS.Platform): ToolchainJobState {
  const r = (text: string): string => redactHome(text, home, platform);
  return {
    ...state,
    steps: state.steps.map((step) => ({
      ...step,
      ...(step.detail !== undefined ? { detail: r(step.detail) } : {}),
      ...(step.error !== undefined ? { error: r(step.error) } : {}),
      ...(step.log !== undefined ? { log: step.log.map(r) } : {}),
    })),
  };
}
