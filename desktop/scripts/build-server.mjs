/* global process, console */
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, rmSync } from "node:fs";
import path from "node:path";

/**
 * Builds the Go backend this app runs.
 *
 * Dev (default): one binary for this Mac's own architecture, into `bin/`, which
 * is where `services/detect.ts` looks when the app is not packaged.
 *
 * `--universal`: arm64 and amd64, joined with `lipo`, so one .app runs on both
 * Apple Silicon and Intel. Packaging uses this — a universal app that ships a
 * single-arch helper dies on half the Macs it installs on, and it dies at
 * launch rather than at build time.
 *
 * `--executor`: the same module's headless executor (`cmd/executor`) into
 * `bin/executor` instead — the process the runner starts in account mode.
 * Same cgo rule, because it links the same tree-sitter grammars.
 */
const root = path.resolve(import.meta.dirname, "..");
const serverDir = path.resolve(root, "..", "server");
const outDir = process.env.SERVER_OUT_DIR ?? path.join(root, "bin");
const universal = process.argv.includes("--universal");
const target = process.argv.includes("--executor") ? "executor" : "agent-server";

if (!existsSync(path.join(serverDir, "go.mod"))) {
  console.error(
    `build-server: no Go module at ${serverDir}.\n` +
      "The desktop app bundles the backend from the sibling `server/` directory; check out the whole repository.",
  );
  process.exit(1);
}

if (!existsSync(path.join(serverDir, "cmd", target))) {
  console.error(`build-server: ${serverDir} has no cmd/${target}.`);
  process.exit(1);
}

mkdirSync(outDir, { recursive: true });

function go(args, env) {
  try {
    execFileSync("go", args, { cwd: serverDir, stdio: "inherit", env: { ...process.env, ...env } });
  } catch (err) {
    // `go build` has already printed the compiler errors; what it has not said
    // is which tree failed, and this script is usually run from the desktop
    // package where that is the first question.
    console.error(`\nbuild-server: \`go ${args.join(" ")}\` failed in ${serverDir}.`);
    console.error("Fix the server build there first — the desktop app cannot ship a backend that does not compile.");
    process.exit(typeof err.status === "number" ? err.status : 1);
  }
}

// CGO ON, and not by preference: the backend links tree-sitter, whose grammars
// are C. With CGO_ENABLED=0 every one of them fails with "build constraints
// exclude all Go files", which reads like a toolchain problem and is not one.
//
// The universal path below still works because this only ever cross-compiles
// darwin → darwin: clang takes -arch from Go, so an amd64 build on an Apple
// Silicon Mac needs nothing but the Xcode command line tools.
const goos = { darwin: "darwin", linux: "linux", win32: "windows" }[process.platform];
if (!goos) {
  console.error(`build-server: no Go target for ${process.platform}.`);
  process.exit(1);
}
const goarch = { x64: "amd64", arm64: "arm64" }[process.arch] ?? "amd64";
const exe = goos === "windows" ? ".exe" : "";
// cgo: go-tree-sitter's grammars are C, so the backend is built on the platform
// it runs on, never cross-compiled from another one.
const buildEnv = { CGO_ENABLED: "1", GOOS: goos };
// -s -w strips the symbol table and DWARF. Nothing debugs this binary in the
// field, and the app is smaller for it.
const ldflags = "-s -w";
const pkg = `./cmd/${target}`;

/**
 * `go build -o` refuses to overwrite a file it does not recognise as its own
 * output, and a universal binary is exactly that:
 *
 *   build output "…/bin/agent-server" already exists and is not an object file
 *
 * So a plain `npm run build:server` fails on any machine that has packaged
 * once. Removing the target first is the whole fix; it is a build artefact
 * either way.
 */
if (!universal) {
  const out = path.join(outDir, `${target}${exe}`);
  rmSync(out, { force: true });
  go(["build", "-trimpath", "-ldflags", ldflags, "-o", out, pkg], { ...buildEnv, GOARCH: goarch });
  console.log(`${target} -> ${out}`);
} else {
  if (goos !== "darwin") {
    console.error("build-server: --universal is a macOS build; build on each platform without it.");
    process.exit(1);
  }
  const arm = path.join(outDir, `${target}-arm64`);
  const amd = path.join(outDir, `${target}-amd64`);
  go(["build", "-trimpath", "-ldflags", ldflags, "-o", arm, pkg], { ...buildEnv, GOARCH: "arm64" });
  go(["build", "-trimpath", "-ldflags", ldflags, "-o", amd, pkg], { ...buildEnv, GOARCH: "amd64" });
  const out = path.join(outDir, target);
  // lipo has the same objection to an existing target, for the same reason.
  rmSync(out, { force: true });
  execFileSync("lipo", ["-create", "-output", out, arm, amd], { stdio: "inherit" });
  rmSync(arm, { force: true });
  rmSync(amd, { force: true });
  console.log(`${target} (universal) -> ${out}`);
}
