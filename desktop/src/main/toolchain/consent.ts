import { dialog, type BrowserWindow, type MessageBoxOptions } from "electron";
import type { PlannedTool } from "./service.js";

/**
 * The person's own yes, in a native dialog, before the account's remote page
 * installs anything on this computer. A page on another origin can ask; only
 * this dialog — drawn by this app, not by the page — can agree.
 */

export const INSTALL_BUTTON = 0;
export const CANCEL_BUTTON = 1;

export function consentDialog(tools: readonly PlannedTool[]): MessageBoxOptions {
  const lines = tools.map((tool) => `• ${tool.label}${tool.method === "" ? "" : ` — ${tool.method}`}`);
  return {
    type: "question",
    title: "TaskTrooper",
    message: "Install developer tools?",
    detail: [
      "Your TaskTrooper account asked to install these on this computer for your agents:",
      "",
      ...lines,
      "",
      "Each comes from its official source. Your computer may ask for your password.",
    ].join("\n"),
    buttons: ["Install", "Cancel"],
    // Return and Escape both decline: a keystroke meant for something else
    // must not install software the remote page asked for.
    defaultId: CANCEL_BUTTON,
    cancelId: CANCEL_BUTTON,
    noLink: true,
  };
}

export async function confirmInstall(window: BrowserWindow | null, tools: readonly PlannedTool[]): Promise<boolean> {
  const options = consentDialog(tools);
  const result =
    window && !window.isDestroyed() ? await dialog.showMessageBox(window, options) : await dialog.showMessageBox(options);
  return result.response === INSTALL_BUTTON;
}
