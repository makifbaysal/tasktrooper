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
import { findings } from "@/locales/en/findings";
import { frame } from "@/locales/en/frame";
import { issues } from "@/locales/en/issues";
import { lib } from "@/locales/en/lib";
import { operations } from "@/locales/en/operations";
import { projectAdmin } from "@/locales/en/projectAdmin";
import { projectModel } from "@/locales/en/projectModel";
import { projectsHub } from "@/locales/en/projectsHub";
import { release } from "@/locales/en/release";
import { repositoryPage } from "@/locales/en/repositoryPage";
import { settingsPages } from "@/locales/en/settingsPages";
import { setup } from "@/locales/en/setup";
import { toolchain } from "@/locales/en/toolchain";

export const en = {
  common: {
    close: "Close",
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
      help: "The language of the app, and of agents' replies and system instructions.",
    },
    loadFailed: "Failed to load settings",
    savedToast: "Settings saved",
    sections: {
      account: {
        title: "Account",
        description: "Where your board lives and where the work runs.",
      },
      preferences: {
        title: "Preferences",
        description: "Language and the notifications this app shows you.",
      },
      agents: {
        title: "Agents",
        description: "Limits and sources that apply to every agent run.",
      },
      app: {
        title: "Desktop app",
        description: "Updates and privacy for TaskTrooper on this computer.",
      },
      desktopOnly: "Updates, desktop notifications and anonymous usage statistics are set in the TaskTrooper desktop app.",
    },
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
      runner: {
        title: "Use this computer as a GitHub Actions runner",
        description: "Jobs in the repositories you pick run on this computer instead of GitHub's machines, without using Actions minutes. The runner works while TaskTrooper is open.",
        warning: "Code pushed to these repositories runs on this computer as you. Only private repositories you administer can be added.",
        repositories: "Repositories",
        noCandidates: "No private repository that you administer was found on GitHub.",
        diskNote: "Each repository gets its own runner install, about 450 MB.",
        snippetHint: "A workflow job runs here when its runs-on names these labels:",
        copySnippet: "Copy runs-on",
        snippetCopied: "Copied",
        enabledToast: "This computer now takes GitHub Actions jobs",
        disabledToast: "This computer no longer takes GitHub Actions jobs",
        unsupported: "Running GitHub Actions jobs on this computer is not available here: GitHub publishes no runner for it, or TaskTrooper is not running as the desktop app.",
        lastJob: "Last job: {job} · {result}",
        states: {
          stopped: "Off",
          installing: "Downloading the runner…",
          starting: "Connecting to GitHub…",
          idle: "Waiting for a job",
          busy: "Running {job}",
          error: "Stopped, retrying",
        },
        errors: {
          github_not_connected: "GitHub is not connected.",
          forbidden: "GitHub did not allow adding a runner. You need admin rights on the repository; a fine-grained token also needs Administration: Read and write, and a GitHub sign-in needs the app to have the Administration permission.",
          not_found: "This GitHub connection cannot see the repository.",
          public_repo: "{repo} is public. A runner there would run code from anyone's pull request on this computer.",
          needs_admin: "Adding a runner to {repo} needs admin rights on it.",
          invalid_repo: "{repo} is not an owner/name repository.",
          too_many_repos: "At most 5 repositories.",
          unsupported_platform: "GitHub publishes no runner for this computer.",
          download_failed: "The runner could not be downloaded.",
          start_failed: "The runner could not start.",
          exited: "The runner stopped before taking a job.",
          computer_away: "Your computer is not connected. Open the TaskTrooper desktop app on it and the runner starts by itself.",
          computer_not_ready: "Your computer is connected but not ready to run a GitHub runner yet: {{repo}}",
          computer_outdated: "Update the TaskTrooper desktop app on your computer to run a GitHub runner there.",
          github_error: "GitHub refused the request.",
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
      temporaryLocal: "Signed in to {origin}, but using TaskTrooper on this computer without it for now. Tasks assigned to you there wait until you go back, and the next launch tries the account again.",
      backToAccount: "Back to the account",
      bannerTitle: "Using TaskTrooper without your account for now",
      bannerBody: "You are still signed in to {origin}. Tasks assigned to you there wait until you go back.",
    },
    analytics: {
      label: "Usage statistics",
      help: "Sends which screens you open and which features you use, with the app version and system — never your tasks, chats, code, paths or email. Signed in to an account, a one-way hash of your account ID goes with it, so you count as one person across the desktop, web and mobile apps. Turning it off stops sending at once and deletes this install's random ID.",
      forcedOff: "Turned off by the TASKTROOPER_TELEMETRY or DO_NOT_TRACK environment variable.",
      saveFailed: "Couldn't save this setting",
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
  findings,
  frame,
  issues,
  lib,
  operations,
  projectAdmin,
  projectModel,
  projectsHub,
  release,
  repositoryPage,
  settingsPages,
  setup,
  toolchain,
};

// No `as const`: property strings widen to `string`, so tr.ts (typed `Dict`)
// must match en's keys/shape without matching exact literals.
export type Dict = typeof en;
