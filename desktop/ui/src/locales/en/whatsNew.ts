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
