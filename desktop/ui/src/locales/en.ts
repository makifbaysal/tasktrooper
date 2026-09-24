// English dictionary — source of truth for UI strings.
// Shape here defines `Dict`; tr.ts must match it (missing/extra keys fail typecheck).
// Namespaced by page/component. Interpolation uses {name} placeholders.
import { addRepository } from "@/locales/en/addRepository";
import { agentArea } from "@/locales/en/agentArea";
import { boardArea } from "@/locales/en/boardArea";
import { chatArea } from "@/locales/en/chatArea";
import { cloud } from "@/locales/en/cloud";
import { content } from "@/locales/en/content";
import { frame } from "@/locales/en/frame";
import { lib } from "@/locales/en/lib";
import { operations } from "@/locales/en/operations";
import { projectAdmin } from "@/locales/en/projectAdmin";
import { projectModel } from "@/locales/en/projectModel";
import { projectsHub } from "@/locales/en/projectsHub";
import { repositoryPage } from "@/locales/en/repositoryPage";
import { settingsPages } from "@/locales/en/settingsPages";
import { setup } from "@/locales/en/setup";

export const en = {
  common: {
    save: "Save",
    saving: "Saving...",
    cancel: "Cancel",
    refresh: "Refresh",
    resetDefault: "Reset to default",
    actionFailed: "Action failed",
    saved: "Saved",
    saveFailed: "Failed to save",
    comingSoon: "Coming soon",
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
    concurrency: {
      title: "Concurrency limits",
      agentsLabel: "Max concurrent agents",
      tasksLabel: "Max concurrent tasks",
      agentsHelp: "How many agent runs can execute at the same time from the board.",
      tasksHelp: "How many different tasks can hold a running agent at the same time.",
      zeroHint: "0 means unlimited",
      reset: "Unlimited",
      resetting: "Resetting…",
      loadFailed: "Failed to load settings",
      saveFailed: "Failed to save",
    },
  },
  addRepository,
  agentArea,
  boardArea,
  chatArea,
  cloud,
  content,
  frame,
  lib,
  operations,
  projectAdmin,
  projectModel,
  projectsHub,
  repositoryPage,
  settingsPages,
  setup,
};

// No `as const`: property strings widen to `string`, so tr.ts (typed `Dict`)
// must match en's keys/shape without matching exact literals.
export type Dict = typeof en;
