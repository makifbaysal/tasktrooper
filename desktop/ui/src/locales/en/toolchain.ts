// English dictionary for the developer-tool installer: the shared pieces in
// components/toolchain/*, the header chip's popover and Settings →
// Development environment. Source of truth for every other language's copy.
export const toolchain = {
  stepState: {
    queued: "Queued",
    running: "Installing",
    waiting: "Waiting for you",
    done: "Installed",
    failed: "Failed",
    skipped: "Skipped",
    cancelled: "Cancelled",
  },
  itemStatus: {
    installed: "Installed",
    installedVersion: "Installed · {version}",
    missing: "Missing",
    unusable: "Not usable",
  },
  badge: {
    running: "Setting up · {percent}%",
    done: "Setup complete",
    failed: "Setup: {count} failed",
    stopped: "Setup stopped",
    incomplete: "Setup incomplete",
  },
  waitingHint: "An installer or Terminal window is open on this computer. Finish it there and this carries on by itself.",
  method: "Via {method}",
  needsManual: "Needs {label}. Install it from its page first.",
  queuedAfterCurrent: "Queued after the current steps",
  kind: {
    interactive: "Opens an installer window",
    manual: "Installed from its own page",
  },
  reasons: {
    everyone: "Every agent uses it",
    agents: "For {agents}",
  },
  progress: {
    percent: "{percent}%",
    summary: "{done} of {total} finished",
    failed: "{count} failed",
  },
  actions: {
    install: "Install",
    openPage: "Open page",
    openPageFor: "Open {label} page",
    stop: "Stop",
    dismiss: "Dismiss",
    details: "Details",
    restartAgents: "Restart agents",
    checkAgain: "Check again",
    installRecommended: "Install recommended",
  },
  restart: {
    hint: "Some of what was installed reaches the agents only after they restart.",
    title: "Restart the agents?",
    description:
      "TaskTrooper restarts the service your agents run in so they pick up the new tools. Anything an agent is doing right now is interrupted, and this window reloads.",
    confirm: "Restart",
    failed: "Couldn't restart the agents",
  },
  errors: {
    installFailed: "Couldn't start the installation",
    declined: "Nothing was installed",
    stopFailed: "Couldn't stop the installation",
    dismissFailed: "Couldn't clear the finished setup",
    openPageFailed: "Couldn't open the page",
    reportFailed: "Couldn't check this computer",
  },
  job: {
    title: {
      running: "Setting up this computer",
      complete: "Setup finished",
      failed: "Setup finished with errors",
      stopped: "Setup stopped",
      incomplete: "Setup finished, but not everything was installed",
    },
  },
  running: {
    title: "Developer tools are still installing",
    body: "git or Claude Code may still be on their way. Once they're in, press Check again.",
  },
  settings: {
    desktopOnlyTitle: "This page needs the TaskTrooper desktop app",
    desktopOnlyBody: "It checks and installs developer tools on your own computer, which a browser tab can't reach.",
    recommendedTitle: "Recommended for your team",
    recommendedDescription: "What your enabled agents work best with and this computer doesn't have yet.",
    recommendedNone: "Everything your team needs is on this computer.",
    recommendedManual: "Tools installed from their own page aren't part of the one-click install; use Open page below.",
    allTitle: "All developer tools",
    allDescription: "What TaskTrooper can check for and install on this computer.",
    lastChecked: "Checked {when}.",
    checking: "Checking this computer…",
    groups: {
      basics: "Basics",
      web: "Web",
      mobile: "Mobile",
      data: "Data",
      games: "Games",
      infra: "Infra & security",
    },
  },
};
