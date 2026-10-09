/* global process, console */
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, rmSync } from "node:fs";
import path from "node:path";

/**
 * Builds the Go runner the desktop app spawns in account mode.
 *
 * Default: one binary for this machine's own OS and architecture, into `bin/`,
 * which is where `services/detect.ts`'s `probeRunnerBinary` looks when the
 * app is not packaged — the same split `build-server.mjs` uses for the OSS
 * backend. It is `runner.exe` for a Windows target, the name detect.ts's
 * `RUNNER_BINARY` looks for there.
 *
 * `RUNNER_GOOS` / `RUNNER_GOARCH` pick another target, for building a
 * Windows or Linux runner from a Mac (or the reverse) in CI.
 *
 * `--universal`: arm64 and amd64, joined with `lipo`, so one .app runs on
 * both Apple Silicon and Intel. `npm run package` uses this. A macOS target
 * only, built on macOS: lipo exists nowhere else.
 *
 * `CGO_ENABLED=0`, unlike `build-server.mjs`. The runner links no C code —
 * it has no tree-sitter grammars, no embedded Postgres — so cgo would only
 * add a dependency on the local C toolchain for no benefit, and a pure-Go
 * static binary cross-compiles cleanly to any target in one pass.
 */
const root = path.resolve(import.meta.dirname, "..");
const runnerDir = path.resolve(root, "runner");
const outDir = process.env.RUNNER_OUT_DIR ?? path.join(root, "bin");
const universal = process.argv.includes("--universal");

if (!existsSync(path.join(runnerDir, "go.mod"))) {
  console.error(
    `build-runner: no Go module at ${runnerDir}.\n` +
      "The desktop app bundles the runner from `desktop/runner/`; check out the whole repository.",
  );
  process.exit(1);
}

mkdirSync(outDir, { recursive: true });

function go(args, env) {
  try {
    execFileSync("go", args, { cwd: runnerDir, stdio: "inherit", env: { ...process.env, ...env } });
  } catch (err) {
    console.error(`\nbuild-runner: \`go ${args.join(" ")}\` failed in ${runnerDir}.`);
    console.error("Fix the runner build there first — the desktop app cannot ship a runner that does not compile.");
    process.exit(typeof err.status === "number" ? err.status : 1);
  }
}

const GOOS = ["darwin", "linux", "windows"];
const GOARCH = ["amd64", "arm64"];
const goos = process.env.RUNNER_GOOS || { darwin: "darwin", linux: "linux", win32: "windows" }[process.platform];
const goarch = process.env.RUNNER_GOARCH || ({ x64: "amd64", arm64: "arm64" }[process.arch] ?? "amd64");
if (!GOOS.includes(goos)) {
  console.error(`build-runner: no runner target for ${goos ?? process.platform}; set RUNNER_GOOS to one of ${GOOS.join(", ")}.`);
  process.exit(1);
}
if (!GOARCH.includes(goarch)) {
  console.error(`build-runner: no runner target for ${goarch}; set RUNNER_GOARCH to one of ${GOARCH.join(", ")}.`);
  process.exit(1);
}
const exe = goos === "windows" ? ".exe" : "";
const buildEnv = { CGO_ENABLED: "0", GOOS: goos };
// -s -w strips the symbol table and DWARF. Nothing debugs this binary in the
// field, and the app is smaller for it — same rule build-server.mjs follows.
const ldflags = "-s -w";
const pkg = "./";

/**
 * `go build -o` refuses to overwrite a file it does not recognise as its own
 * output, which is exactly what a universal binary already built once looks
 * like — see `build-server.mjs`'s identical note.
 */
if (!universal) {
  const out = path.join(outDir, `runner${exe}`);
  rmSync(out, { force: true });
  go(["build", "-trimpath", "-ldflags", ldflags, "-o", out, pkg], { ...buildEnv, GOARCH: goarch });
  console.log(`runner (${goos}/${goarch}) -> ${out}`);
} else {
  if (goos !== "darwin" || process.platform !== "darwin") {
    console.error("build-runner: --universal is a macOS binary joined with lipo, which only macOS has; build other targets without it.");
    process.exit(1);
  }
  const arm = path.join(outDir, "runner-arm64");
  const amd = path.join(outDir, "runner-amd64");
  go(["build", "-trimpath", "-ldflags", ldflags, "-o", arm, pkg], { ...buildEnv, GOARCH: "arm64" });
  go(["build", "-trimpath", "-ldflags", ldflags, "-o", amd, pkg], { ...buildEnv, GOARCH: "amd64" });
  const out = path.join(outDir, "runner");
  rmSync(out, { force: true });
  execFileSync("lipo", ["-create", "-output", out, arm, amd], { stdio: "inherit" });
  rmSync(arm, { force: true });
  rmSync(amd, { force: true });
  console.log(`runner (universal) -> ${out}`);
}
