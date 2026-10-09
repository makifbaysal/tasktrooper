// English dictionary — source of truth for UI strings.
// Shape here defines `Dict`; tr.ts must match it (missing/extra keys fail typecheck).
// Namespaced by page/component. Interpolation uses {name} placeholders.
import { activityArea } from "@/locales/en/activityArea";
import { addRepository } from "@/locales/en/addRepository";
import { agentArea } from "@/locales/en/agentArea";
import { analysisReview } from "@/locales/en/analysisReview";
import { boardArea } from "@/locales/en/boardArea";
import { chatArea } from "@/locales/en/chatArea";
import { cloud } from "@/locales/en/cloud";
import { content } from "@/locales/en/content";
import { designSystem } from "@/locales/en/designSystem";
import { frame } from "@/locales/en/frame";
import { lib } from "@/locales/en/lib";
import { operations } from "@/locales/en/operations";
import { projectAdmin } from "@/locales/en/projectAdmin";
import { projectModel } from "@/locales/en/projectModel";
import { projectsHub } from "@/locales/en/projectsHub";
import { release } from "@/locales/en/release";
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
        "A personal access token from GitHub → Settings → Developer settings. A classic token needs repo, workflow, admin:repo_hook and read:org; a fine-grained one needs Contents, Pull requests, Workflows (read and write) and Webhooks. Without workflow no agent can add or change a CI file. It is verified against GitHub before it is stored, encrypted, on this machine.",
      connectedToast: "GitHub connected",
      connectFailedToast: "GitHub connection failed",
      disconnectedToast: "GitHub connection removed",
      missingScopesTitle: "This token is missing: {scopes}",
      missingWorkflowScope: "Without the workflow scope GitHub refuses every push that adds or changes a file under .github/workflows, so no agent can set up or fix CI. Create a token with it, disconnect, and save the new one.",
      fineGrainedHint: "Fine-grained token: make sure it has Workflows: Read and write, or GitHub refuses pushes that touch CI files.",
      connectWithGitHub: "Connect with GitHub",
      connectWithGitHubHint: "Sign in on github.com with a one-time code. TaskTrooper renews the connection itself — no token to create or paste.",
      deviceCodeTitle: "Enter this code on GitHub",
      deviceCodeHint: "Open {url}, sign in, enter the code and approve TaskTrooper. This window continues by itself.",
      copyCode: "Copy code",
      codeCopied: "Code copied",
      openGitHub: "Open GitHub",
      waitingForApproval: "Waiting for your approval…",
      flowExpired: "The code expired before it was approved.",
      flowDenied: "The sign-in was declined on GitHub.",
      tryAgain: "Try again",
      cancel: "Cancel",
      useTokenInstead: "Use a personal access token instead",
      useAppInstead: "Connect with GitHub instead",
      modeApp: "via GitHub sign-in",
      modeToken: "via access token",
      needsInstallTitle: "Install the app on your repositories",
      needsInstallBody: "The connection works, but the TaskTrooper app is installed on no account yet, so it reaches no repository. Install it on your account or organization and choose All repositories, so repositories TaskTrooper creates later are covered too.",
      installApp: "Install on GitHub",
      expiredTitle: "The GitHub connection expired",
      expiredBody: "It was not used for six months or was revoked on GitHub. Connect again.",
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
      agentChatReplies: "Agent chat replies",
      loadFailed: "Failed to load settings",
      saveFailed: "Failed to save",
    },
    account: {
      title: "Account",
      localHelp: "TaskTrooper runs on this computer, with no account. Connect one to work with a team: the board lives in the account, and the work still runs here, with your own keys.",
      signedIn: "Signed in to {origin}. Tasks assigned to you run on this computer.",
      connect: "Connect an account",
      connecting: "Connecting…",
      signOut: "Sign out",
      signingOut: "Signing out…",
      runsTitle: "{count} local runs are in progress",
      runsBody: "Connecting stops the local server, and these runs with it. Local data stays as it is; signing out brings it back.",
      confirmTitle: "Stop the local server?",
      confirmBody: "Connecting stops the local server on this computer and opens the account's sign-in page in this window. Local data stays as it is; signing out brings it back.",
      confirm: "Continue",
      cancel: "Cancel",
      failed: "The switch didn't complete",
      unavailable: "Accounts are only available in the TaskTrooper desktop app.",
    },
    updates: {
      title: "Updates",
      help: "TaskTrooper checks for updates every few hours and downloads them in the background.",
      currentVersion: "You are running TaskTrooper {version}.",
      unavailable: "Updates are only available in the TaskTrooper desktop app, not in a browser.",
      unsupported: "This build can't update itself",
      upToDate: "You're on the latest version.",
      check: "Check for updates",
      checking: "Checking…",
      checkFailed: "Couldn't check for updates",
      lastChecked: "Last checked {when}",
      downloading: "Downloading an update… {percent}%",
      downloadingVersion: "Downloading TaskTrooper {version}… {percent}%",
      ready: "An update is ready to install.",
      readyVersion: "TaskTrooper {version} is ready to install.",
      restart: "Restart and install",
      restarting: "Restarting…",
      restartHelp: "Local processes are stopped first and the app reopens on the new version. Quitting the app also installs it, without reopening.",
      restartFailed: "Couldn't restart to update",
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
  analysisReview,
  boardArea,
  activityArea,
  chatArea,
  cloud,
  content,
  designSystem,
  frame,
  lib,
  operations,
  projectAdmin,
  projectModel,
  projectsHub,
  release,
  repositoryPage,
  settingsPages,
  setup,
};

// No `as const`: property strings widen to `string`, so tr.ts (typed `Dict`)
// must match en's keys/shape without matching exact literals.
export type Dict = typeof en;
