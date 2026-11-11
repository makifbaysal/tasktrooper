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
  v0_3_13: {
    title: "Your phone, your audience and your telemetry",
    items: {
      tunnel: "Log in to Cloudflare or ngrok from Settings → Remote access and reach this computer from the TaskTrooper mobile app through a tunnel you control.",
      pairPhone: "Pair a phone in one step: Pair phone opens a window with the QR code and the address.",
      audience: "Audience research: ask the PM agent for synthetic personas, run studies and read the report, or talk to a persona one to one.",
      telemetry: "New Telemetry screens in Operations show services, endpoints, traces, logs, errors, incidents, monitors, deploys and SLOs, and the release engineer handles the incidents it raises.",
      repoSetup: "Importing a repository now opens a \"Get this project ready\" checklist (scan, tools, index, README and agent guide, tests, CI, delivery) with a button for each step and one to run them all in order.",
      signOut: "Signing out of your account now clears it completely, so the \"working without account\" banner no longer lingers.",
    },
  },
};
