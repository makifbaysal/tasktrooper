import { dialog, type BrowserWindow, type MessageBoxOptions } from "electron";
import type { McpServerConsent } from "./keys.js";

/**
 * The person's own yes, in a native dialog, before the account's remote page
 * adds or changes an MCP server on this computer — a command the runner will
 * start with this user's permissions. The page can ask; only this dialog,
 * drawn by this app, can agree.
 */

export const ALLOW_BUTTON = 0;
export const CANCEL_BUTTON = 1;

// Control characters and the invisible marks that reorder or hide text: a
// value the page sent must read in the dialog exactly as it will run.
// eslint-disable-next-line no-control-regex -- matching control characters is the point.
const HIDDEN = /[\u0000-\u001f\u007f-\u009f\u00ad\u061c\u180e\u200b-\u200f\u2028-\u202e\u2060-\u206f\ufeff]/g;
const PLAIN_ARG = /^[A-Za-z0-9@%+=:,./_-]+$/;

function hiddenWrittenOut(s: string): string {
  return s.replace(HIDDEN, (c) => `\\u{${c.codePointAt(0)?.toString(16) ?? ""}}`);
}

/**
 * `s` with every hidden or control character written out as `\u{…}`. A
 * backslash is doubled first, so a value that spells `\u{a}` reads apart
 * from one that holds a newline.
 */
export function visible(s: string): string {
  return hiddenWrittenOut(s.replace(/\\/g, "\\\\"));
}

/** One argument as the person should read it: quoted when it has a space, is empty or needs escaping. */
export function shownArg(arg: string): string {
  return PLAIN_ARG.test(arg) ? arg : `"${hiddenWrittenOut(arg.replace(/[\\"]/g, "\\$&"))}"`;
}

export function mcpConsentDialog(consent: McpServerConsent): MessageBoxOptions {
  const runs =
    consent.url !== undefined ? visible(consent.url) : [consent.command ?? "", ...(consent.args ?? [])].map(shownArg).join(" ");
  const values =
    consent.values.length > 0 ? ["", "With:", ...consent.values.map(([name, value]) => `• ${name}=${visible(value)}`)] : [];
  const kept =
    consent.kept.length > 0
      ? ["", `Your saved values go with it: ${consent.kept.join(", ")}. Allow it only if you trust where they now go.`]
      : [];
  return {
    type: "question",
    title: "TaskTrooper",
    message: consent.added
      ? `Add the MCP server "${consent.name}" to this computer?`
      : `Change the MCP server "${consent.name}" on this computer?`,
    detail: [
      consent.added
        ? "Your TaskTrooper account asked to add this for your agents. It runs on this computer, as you:"
        : "Your TaskTrooper account asked to change it. It will run on this computer, as you:",
      "",
      runs,
      ...values,
      ...kept,
      "",
      "Allow it only if you trust this command.",
    ].join("\n"),
    buttons: [consent.added ? "Add" : "Change", "Cancel"],
    // Return and Escape both decline: a keystroke meant for something else
    // must not start a program the remote page asked for.
    defaultId: CANCEL_BUTTON,
    cancelId: CANCEL_BUTTON,
    noLink: true,
  };
}

export async function confirmMcpServer(window: BrowserWindow | null, consent: McpServerConsent): Promise<boolean> {
  const options = mcpConsentDialog(consent);
  const result =
    window && !window.isDestroyed() ? await dialog.showMessageBox(window, options) : await dialog.showMessageBox(options);
  return result.response === ALLOW_BUTTON;
}
