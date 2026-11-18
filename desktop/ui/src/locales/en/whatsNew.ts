// English dictionary for the "What's new" pop-up shown on the first launch of a
// new version. Source of truth for every other language's copy. Each release's
// notes live under `v<major>_<minor>_<patch>`, matching src/lib/whatsNew/entries.ts.
export const whatsNew = {
  dialogTitle: "What's new in TaskTrooper {version}",
  seeAll: "See all changes",
  gotIt: "Got it",
  card: {
    title: "What's new",
    help: "See what changed in the latest version of TaskTrooper.",
    open: "Show what's new",
  },
  v0_3_20: {
    title: "TaskTrooper Telemetry in Operations, a designer that draws in FigCraft, deploy markers",
    items: {
      operationsSource: "Connect TaskTrooper Telemetry in Settings and it becomes an Operations data source by itself: RED charts, logs, traces, error issues and endpoints for each component.",
      agentTools: "Agents get the telemetry tools their role needs: endpoint root-cause analysis, SLOs, monitors and load tests. They can only change or delete monitors and KPIs an agent created.",
      deployMarkers: "Every deploy that goes live is marked in the connected telemetry with its commit, so a regression lines up with the release that caused it.",
      figcraftDesigner: "The ui-designer draws in FigCraft: each design task gets its own editable file built on your design system, with pages you can preview, open in FigCraft or show in Finder from the task. The FigCraft, VectorCraft and PhotoCraft tools install from Settings → Development environment.",
      telemetryScreens: "New telemetry screens: overview, service map, infrastructure, apps, profiling, LLM usage and load tests.",
    },
  },
  v0_3_19: {
    title: "QA that tests and finishes, automation tests per repository, a preview per task",
    items: {
      qaManualRound: "QA now tests the running product against the task's criteria instead of compiling it: build and unit tests stay with code review.",
      qaFinishes: "A QA round cannot end with a test case left unrun or a criterion left unruled, and it cannot send a task back without a recorded failure.",
      automationTests: "Switch on Automation tests for a repository (Components → Review): QA adds each task's cases as Testcontainers + WireMock tests under qa-automation/, and a task leaves QA only with that suite green. Switching it on opens one task that writes the tests the repository is still missing.",
      taskPreviews: "Each task gets its own local preview on its own port, and moving a task stops only that task's preview.",
    },
  },
  v0_3_18: {
    title: "Talk to your agents, attach and tag anywhere, leaner task plans",
    items: {
      voiceChat: "Talk to agents out loud in Turkish or English: the reply is spoken as it streams, drawings appear on screen, and you can interrupt by talking. Pick the voice model and voice in Settings → Voice.",
      attachAndMention: "Paste or attach files and images, and @-tag agents, projects and repositories, on Home, in the header's quick ask and in task comments.",
      notifications: "The notification center shows who sent each notification with their avatar, in your language, and the sidebar badge counts what is really unread.",
      onePerFeature: "The PM opens one task per feature, repository and role instead of many small ones, so each feature is built, reviewed and released once.",
      releaseBatch: "The release engineer can propose a batch release for you to approve, and asks step by step how a component deploys.",
    },
  },
  v0_3_17: {
    title: "Hosted previews first, screenshots agents can see, no more QA ping-pong",
    items: {
      hostedPreview: "When a task's pull request has a ready hosted preview (Vercel), the preview panel opens it first instead of starting a local run.",
      chatImages: "Screenshots you attach in an agent chat now reach agents running on Claude Code, opencode, Cursor and Antigravity, so they can read what's on them.",
      qaBlocked: "When QA can't verify something because the environment lacks it (an integration, credential or device), the task waits for you instead of bouncing between QA and the developer; QA no longer re-tests code it already rejected unchanged.",
    },
  },
  v0_3_16: {
    title: "Audience tests before the work, re-runs that compare, complete tasks",
    items: {
      studyRerun: "Run a finished study again after you change the product and see what moved: the verdict score, objections that went away or appeared, and each person's change.",
      pmAudienceTest: "When a request depends on how people react (a price, a paid feature, copy, onboarding), the PM offers an audience test before opening the work, lets you revise and re-test, and opens the tasks when you say so.",
      serialReleases: "Releases ship one at a time per component, so two deploys of the same service never overlap.",
      completeTasks: "Tasks agents open always carry their description, acceptance criteria and an assignee; an empty or unassigned task is refused and fixed by the agent.",
      chatThinking: "While an agent works, its thinking streams into the thinking steps instead of appearing as a reply first; the system architect answers technical questions from the code.",
      mascot: "Trooper, the new mascot, shows up in empty states, setup and agent avatars.",
    },
  },
  v0_3_15: {
    title: "Agents get the right guide, tasks get an agent, Docker gets commands",
    items: {
      agentsMd: "The agent-guide task from project setup now writes a single AGENTS.md for every agent CLI and no longer asks for a README the project already has.",
      assignTasks: "Tasks opened by a scan or an agent are given a suitable agent when they are created, and the PM can set or correct a task's assignee in place instead of recreating it.",
      dockerCommands: "Containerised components get Docker commands (buildkit, image build, compose up, down and stop), registry, platforms and tag settings, a run-in-containers-only rule for agents, and every command can be removed.",
    },
  },
  v0_3_14: {
    title: "Audience research on your phone",
    items: {
      phoneAudience: "Manage audiences from the TaskTrooper mobile app: create one with the PM or a form, build it, and talk to any person in it one to one.",
      phoneStudies: "Run studies from your phone: pick the type, review and approve the interview script, follow the interviews and generate the report.",
      phoneReports: "Read study reports on your phone and share them as one HTML file.",
    },
  },
  v0_3_13: {
    title: "Your phone, your audience and your telemetry",
    items: {
      tunnel: "Log in to Cloudflare or ngrok from Settings → Remote access and reach this computer from the TaskTrooper mobile app through a tunnel you control. Pair a phone in one step: Pair phone opens a window with the QR code and the address.",
      audience: "Audience research: ask the PM agent for synthetic personas, or run a study yourself - interviews, a landing page test, message testing or an A/B test - read the report and export it as one HTML file.",
      telemetry: "New Telemetry screens in Operations show services, endpoints, traces, logs, errors, incidents, monitors, deploys and SLOs, and the release engineer handles the incidents it raises.",
      repoSetup: "Importing a repository now opens a \"Get this project ready\" checklist (scan, tools, index, README and agent guide, tests, CI, delivery) with a button for each step and one to run them all in order. Signing out of your account now clears it completely, so the \"working without account\" banner no longer lingers.",
      runTrace: "Every agent run now leaves a real trace in the task drawer: what it read, changed and ran, checked against git. One click, after a preview, undoes the run's work behind a backup branch.",
      reviewHistory: "Every task now carries its review history: each review and QA round with who reviewed, what they found and what became of it, the bounces between columns, and what each reviewer did and did not cover. A \"Needs your review\" card lists what reviewers and the server want a person to look at.",
    },
  },
};
