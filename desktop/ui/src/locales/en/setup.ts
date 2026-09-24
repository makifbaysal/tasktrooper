// English dictionary for the guided first-run sequence (`/setup`,
// pages/SetupPage.tsx and components/setup/*). Source of truth for SetupDict.
export const setup = {
  title: "Get TaskTrooper ready",
  description: "Four things, in order. Each one unlocks the next.",
  stepLabel: "Step {index} of {total}",
  later: "I'll do this later",
  state: {
    done: "Done",
    todo: "To do",
    unknown: "Couldn't check",
    locked: "Locked",
    desktopOnly: "Needs the Mac app",
  },
  unknownHint: "This couldn't be checked just now, so nothing here is claiming it isn't done.",
  desktopOnly: {
    title: "This step happens in the TaskTrooper Mac app",
    body: "It acts on your own Mac, checking what's installed and starting a headless Claude Code session there, and a browser tab can't reach any of that. Download the app, sign in with this same account, and this sequence carries on there.",
  },
  environment: {
    title: "Check this Mac",
    description:
      "What TaskTrooper needs on this machine: git, plus whichever agent CLIs you use. Anything red below says what's wrong and what to run.",
    readyTitle: "Everything required is in place",
    readyBody: "You can connect an agent runtime now.",
    notReadyTitle: "Something required is missing",
    notReadyBody: "Fix the items marked below, then press Check again. Connect stays closed until they're all green.",
    continue: "Continue",
  },
  agent: {
    title: "Connect an agent runtime",
    description:
      "Agents do their work through a coding CLI on this Mac. Connect any of the ones you use: one is enough, and you can add more later.",
    connect: "Connect",
    connecting: "Connecting…",
    disconnect: "Disconnect",
    connected: "Connected",
    connectedBody: "{binary}{version}: {agents} agents and {skills} skills installed.",
    notInstalled: "Not installed",
    installWith: "Install it with: {command}",
    notInstalledBody: "Not found on this Mac.",
    noneTitle: "Nothing connected yet",
    noneBody: "Connect at least one CLI, or add an API provider below, before agents can run.",
    apiKeyTitle: "Prefer an API key?",
    apiKeyBody:
      "Agents can also run through a model API instead of a CLI: OpenAI, Anthropic, Google Gemini, Groq, or any OpenAI-compatible endpoint with your own key and base URL.",
    apiKeyAction: "Add an API provider",
    failed: "Connecting failed",
  },
  github: {
    title: "Connect GitHub",
    description:
      "Agents clone, branch, push and open pull requests as this account. You'll go to GitHub's own permission screen; nothing is typed by hand here.",
    doneTitle: "GitHub is connected",
    doneBody: "You can import repositories now.",
    todoTitle: "GitHub isn't connected yet",
    todoBody: "Without it there's nothing to import and nowhere for an agent to push.",
    continue: "Continue",
  },
  project: {
    title: "Your first project",
    description:
      "A project groups the repositories that ship together. Create one, then import your first repository into it. You'll be asked what kind it is, how it deploys, and how it's built and tested.",
    createProject: "Create a project",
    needsRepositoryTitle: "Now import a repository",
    needsRepositoryBody: "Use the button below to add one. You can import more later.",
    doneTitle: "Your first repository is in",
    doneBody: "It's linked to the project and indexing in the background. Add more whenever you like.",
    loadFailed: "Couldn't load your projects",
  },
  nav: {
    finishSetup: "Finish setup",
  },
};
