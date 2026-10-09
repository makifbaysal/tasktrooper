package main

// runnerSupervisorDir is where the desktop app's runner supervisor lives in
// this monorepo. The runner used to sit inside the old Electron app at
// desktop/runner, next to ../src/main/supervisor; the contract tests read that
// TypeScript to keep the two sides in step.
const runnerSupervisorDir = "../src/main/runner"
