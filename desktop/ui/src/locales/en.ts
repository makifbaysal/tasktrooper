// English dictionary — source of truth for UI strings.
// Shape here defines `Dict`; tr.ts must match it (missing/extra keys fail typecheck).
// Namespaced by page/component. Interpolation uses {name} placeholders.
import { agentArea } from "@/locales/en/agentArea";
import { boardArea } from "@/locales/en/boardArea";
import { chatArea } from "@/locales/en/chatArea";
import { content } from "@/locales/en/content";
import { frame } from "@/locales/en/frame";
import { lib } from "@/locales/en/lib";
import { operations } from "@/locales/en/operations";
import { projectAdmin } from "@/locales/en/projectAdmin";
import { settingsPages } from "@/locales/en/settingsPages";
import { setup } from "@/locales/en/setup";

export const en = {
  common: {
    save: "Save",
    saving: "Saving...",
    cancel: "Cancel",
    edit: "Edit",
    refresh: "Refresh",
    resetDefault: "Reset to default",
    actionFailed: "Action failed",
    saved: "Saved",
    saveFailed: "Failed to save",
    errorBoundary: {
      title: "Something went wrong",
      body: "An unexpected error occurred and the screen couldn't render. Try reloading the page.",
      retry: "Try again",
      reload: "Reload page",
    },
    configError: {
      title: "Configuration error",
      body: "This build has no API key for the local server, so every request would be refused. Set VITE_API_KEY (it must match the server's SERVER_API_KEY) and rebuild.",
      missing: "Missing:",
    },
  },
  settings: {
    language: {
      label: "Language",
      help: "Used for assistant responses and system instructions.",
    },
    loadFailed: "Failed to load settings",
    savedToast: "Settings saved",
    github: {
      statusUnavailable: "Status unavailable.",
      connected: "✓ Connected: {login} — agents can create private repos, push, and open draft PRs.",
      disconnect: "Disconnect",
      connect: "Save token",
      tokenPlaceholder: "ghp_… or github_pat_…",
      tokenHelp:
        "A personal access token from GitHub → Settings → Developer settings. It needs repo, admin:repo_hook and read:org. It is verified against GitHub before it is stored, encrypted, on this machine.",
      connectedToast: "GitHub connected",
      connectFailedToast: "GitHub connection failed",
      disconnectedToast: "GitHub connection removed",
    },
    vercel: {
      statusUnavailable: "Status unavailable.",
      connected: "✓ Connected: {login}",
      disconnect: "Disconnect",
      connect: "Connect",
      tokenLabel: "Access token",
      tokenPlaceholder: "paste a Vercel access token",
      tokenHelp:
        "Create a token at vercel.com/account/tokens, scoped to the team your projects live in. It is verified once, stored encrypted on the server and never shown again.",
      team: "Scope",
      personalAccount: "Personal account",
      teamSaved: "Vercel scope saved",
      connectedToast: "Vercel connected",
      connectFailedToast: "Vercel connection failed",
      disconnectedToast: "Vercel connection removed",
      apps: {
        title: "App links",
        subtitle:
          "Pick an app. TaskTrooper reads its tree and your Vercel projects to work out where its frontend and backend ship; when it cannot tell, you choose.",
        selectApp: "Select an app",
        noApps: "No apps yet — add a repository first.",
        detecting: "Inspecting the repository and your Vercel projects…",
        detectFailed: "Detection failed",
        noAreas: "This app has no frontend or backend area to host — only backend and frontend projects are linked.",
        warnings: "Notes",
        area: { root: "Whole repository", frontend: "Frontend", backend: "Backend", mobile: "Mobile", worker: "Worker" },
        directory: "Folder",
        linked: "Linked to {name}",
        linkedProvider: "Recorded as {provider}",
        linkedBy: { detected: "detected", user: "chosen by you" },
        openProject: "Open",
        unlink: "Unlink",
        unlinked: "Link removed",
        hints: "What the tree says",
        detected: "Detected: {name}",
        detectedHelp: "Proven by {reason}.",
        confirm: "Link",
        ambiguous: "Several Vercel projects could be this one — pick the right one.",
        none: "Could not tell where this ships. Pick the Vercel project, or record where it lives instead.",
        pickProject: "Vercel project",
        pickProjectPlaceholder: "Choose a project",
        loadingProjects: "Loading projects…",
        noProjects: "No projects in this scope.",
        elsewhere: "It lives elsewhere",
        elsewherePlaceholder: "Choose a provider",
        record: "Record",
        recorded: "Recorded — this area will not be asked about again.",
        linkSaved: "Linked to {name}",
        notConnected: "Connect Vercel above to look projects up.",
        reasons: {
          project_json: ".vercel/project.json in the working copy",
          git_link_dir: "git-linked to this repository and built from this folder",
          git_link: "git-linked to this repository",
          name: "a matching project name",
        },
      },
    },
    boilerplate: {
      title: "Boilerplate Catalog",
      descPrefix: "Before writing code from scratch, agents look up",
      descMid: "in this repo; if a matching boilerplate exists they start by copying it.",
      descSuffix: "or a full URL is accepted.",
      loadFailed: "Failed to load settings",
    },
    notifications: {
      title: "Desktop notifications",
      help: "Native notifications for board events that need your attention. They keep working after you close the window.",
      unavailable: "Desktop notifications are only available in the TaskTrooper app, not in a browser.",
      enabled: "Enabled",
      analizReview: "Analysis ready for your review",
      humanUat: "Awaiting your UAT",
      humanNeeded: "An agent needs a human decision",
      agentComments: "New agent comments",
      loadFailed: "Failed to load settings",
      saveFailed: "Failed to save",
    },
  },
  agentArea,
  boardArea,
  chatArea,
  content,
  frame,
  lib,
  operations,
  projectAdmin,
  settingsPages,
  setup,
};

// No `as const`: property strings widen to `string`, so tr.ts (typed `Dict`)
// must match en's keys/shape without matching exact literals.
export type Dict = typeof en;
