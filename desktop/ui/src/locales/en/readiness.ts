// English dictionary for the repository setup checklist ("Get this project
// ready") on the repository page. Step ids and the detail reasons match
// server readiness (domain.ReadinessStepID and each step's `reason`); every
// other language mirrors these keys exactly.
export const readiness = {
  title: "Get this project ready",
  intro: "What this project is missing before agents can work on it well, in the order that makes sense. Skip anything you do not need.",
  progress: "{done} of {total} done",
  skippedCount: "{count} skipped",
  complete: "This project is ready.",
  show: "Show checklist",
  hide: "Hide",
  expand: "Expand checklist",
  collapse: "Collapse checklist",
  skip: "Skip",
  unskip: "Undo skip",
  doIt: "Do it",
  open: "Open",
  install: "Install",
  viewTask: "View {key}",
  taskOpen: "Task {key} is in progress.",
  skippedNote: "Skipped.",
  waitingFor: "Waiting for: {step}.",
  started: "Started.",
  taskOpened: "Task {key} opened on the board.",
  allStarted: "Started {runs} and opened {tasks} tasks.",
  actionFailed: "Could not do that",
  runAll: "Run all in order",
  runAllTitle: "Run all in order?",
  runAllTasks: "{count} tasks will be opened on the board, one waiting for the one before it.",
  runAllRuns: "{count} will start right away, without a task.",
  runAllCost: "Each task uses your agents' time.",
  runAllConfirm: "Run all",
  status: {
    done: "Done",
    todo: "To do",
    running: "In progress",
    blocked: "Waiting for an earlier step",
    skipped: "Skipped",
    unknown: "Not checked",
  },
  missing: {
    readme: "README",
    agent_guide: "agent guide",
  },
  steps: {
    scan: {
      title: "Scan the project",
      why: "The scan finds the components, commands and CI that every other step reads from.",
    },
    toolchain: {
      title: "Install the tools for this stack",
      why: "Agents build and test on this computer, so it needs the compilers they call.",
    },
    index: {
      title: "Index the code",
      why: "The index lets agents find the right files instead of reading everything.",
    },
    guide: {
      title: "Add a README and an agent guide",
      why: "Agents start every task from these files, so they save the most time and mistakes.",
    },
    tests: {
      title: "Make the tests runnable",
      why: "A passing test run is how you and the agents know a change did not break anything.",
    },
    ci: {
      title: "Run the checks in CI",
      why: "CI repeats the build and the tests on every change before it reaches your main branch.",
    },
    delivery: {
      title: "Choose how changes ship",
      why: "A delivery target tells TaskTrooper where a finished change goes and how to check it.",
    },
    observability: {
      title: "Watch it in production",
      why: "Metrics, logs and traces tell you when a release breaks something.",
    },
    findings: {
      title: "Review the code-health findings",
      why: "The scan flags debug settings, secrets and missing logging that are cheap to fix early.",
    },
    design: {
      title: "Set up the design system",
      why: "A design system keeps new screens consistent with the ones you already have.",
    },
    store: {
      title: "Link the store app",
      why: "Linking the App Store or Google Play app lets TaskTrooper ship builds and watch reviews.",
    },
  },
  detail: {
    scan: {
      no_scan: "Not scanned yet.",
      scan_failed: "The last scan failed.",
      no_components: "The scan found no components.",
      running: "Scanning…",
      done: "{cmp} components found.",
    },
    toolchain: {
      unknown: "Needs: {languages}.",
      no_bridge: "Only the desktop app can check this computer's tools.",
      missing: "Missing on this computer: {tools}.",
      installed: "Everything this stack needs is installed.",
    },
    index: {
      not_indexed: "Not indexed yet.",
      failed: "The last index pass failed.",
      unavailable: "The code index is not available.",
      running: "{processed} of {total} files.",
      done: "{files} files indexed.",
    },
    guide: {
      missing: "Missing: {missing}.",
      unreadable: "The working copy cannot be read.",
      done: "README and agent guide found.",
    },
    tests: {
      gaps: "No test command for: {components}.",
      done: "Every component has a test command.",
    },
    ci: {
      no_ci: "No CI workflow found.",
      done: "{checks} CI checks found.",
    },
    delivery: {
      gaps: "No confirmed delivery for: {components}.",
      done: "Every deployable component has a delivery target.",
    },
    observability: {
      gaps: "Gaps in: {components}.",
      done: "No gaps found.",
    },
    findings: {
      open: "{count} open ({high} high severity).",
      done: "No open findings.",
    },
    design: {
      none: "No design system yet.",
      unavailable: "The design system could not be read.",
      done: "A design system is in place.",
    },
    store: {
      no_app: "No store app linked.",
      unavailable: "The store apps could not be read.",
      done: "A store app is linked.",
    },
  },
};
