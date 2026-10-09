/* global process */
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { createServer } from "vite";

/**
 * Dev: Vite for the shell's own chrome with HMR, esbuild in watch mode for main
 * and the preloads, the Go backend rebuilt, and Electron pointed at the dev
 * server.
 *
 * The main process is NOT hot-reloaded. It supervises long-lived child
 * processes — a Postgres cluster among them — and reloading it out from under
 * them would orphan the lot on every save. Restarting the app is the honest way
 * to pick up a main-process change.
 *
 * The SPA is NOT served by Vite here. The window loads `app://tasktrooper` from
 * `ui/dist`, because that is the origin the bridge's sender checks are written
 * against; a dev server on http://localhost would be a different origin and a
 * different set of powers. So `ui/dist` is built once if it is missing, and
 * `npm --prefix ui run build -- --watch` in a second terminal is how to iterate
 * on it.
 */
const root = path.resolve(import.meta.dirname, "..");

// The `electron` package's main export is the path to its binary, which is
// what node_modules/.bin/electron is a POSIX shell wrapper around — and on
// Windows that wrapper is a .cmd, which spawn() will not run without a shell.
const electronBinary = createRequire(import.meta.url)("electron");

/**
 * npm, as the npm running this script: `npm_execpath` is its JS entry, run with
 * this same node, so no `npm.cmd` (and no shell) is involved on Windows. Run
 * outside npm, it falls back to the platform's own launcher.
 */
function npm(args) {
  const cli = process.env.npm_execpath;
  if (cli && /\.(c|m)?js$/.test(cli)) return run(process.execPath, [cli, ...args]);
  return process.platform === "win32"
    ? run("npm.cmd", args, { shell: true })
    : run("npm", args);
}

function run(command, args, options = {}) {
  const child = spawn(command, args, { stdio: "inherit", cwd: root, ...options });
  return new Promise((resolve, reject) => {
    child.on("exit", (code) => (code === 0 ? resolve() : reject(new Error(`${command} exited ${code}`))));
  });
}

const server = await createServer({ configFile: path.join(root, "vite.config.ts") });
await server.listen();
const url = server.resolvedUrls?.local?.[0];
if (!url) throw new Error("vite did not report a dev server URL");
server.printUrls();

// The backend too, and it is not optional. It is a separate Go binary that
// nothing else in this flow rebuilds, so a dev run would otherwise cheerfully
// start whatever was left in bin/ from the last time somebody packaged.
await run(process.execPath, [path.join(root, "scripts/build-server.mjs")]);

// The embedder is a separate esbuild bundle for the same reason; without it the
// child fails to spawn and embeddings are silently unavailable all session.
await run(process.execPath, [path.join(root, "scripts/build-embedder.mjs")]);

// The runner, which account mode starts; without it signing in leaves a
// runner that cannot start.
await run(process.execPath, [path.join(root, "scripts/build-runner.mjs")]);

// The SPA, only when there is nothing to serve: it is slow, and most sessions
// are not changing it.
if (!existsSync(path.join(root, "ui", "dist", "index.html"))) {
  await npm(["--prefix", "ui", "run", "build"]);
}

await run(process.execPath, [path.join(root, "scripts/build-main.mjs")]);

// Electron's single-instance lock keys off the app name, and this build's is
// the same "TaskTrooper" as the packaged app's — so on a machine that already
// has TaskTrooper.app running (the common case: it's this project's own daily
// driver), a plain dev launch just focuses that window and exits immediately.
// `npm run dev:isolated` (`--isolated`) gives it a profile of its own; an
// explicit TASKTROOPER_DEV_USER_DATA_DIR still wins. /tmp on macOS and Linux,
// where earlier isolated runs left their profile, and the temp dir on Windows.
const isolatedDefault = path.join(process.platform === "win32" ? os.tmpdir() : "/tmp", "tasktrooper-dev-profile");
const userDataDir =
  process.env.TASKTROOPER_DEV_USER_DATA_DIR || (process.argv.includes("--isolated") ? isolatedDefault : undefined);
const electronArgs = userDataDir ? [".", `--user-data-dir=${userDataDir}`] : ["."];

const electron = spawn(electronBinary, electronArgs, {
  cwd: root,
  stdio: "inherit",
  env: { ...process.env, VITE_DEV_SERVER_URL: url },
});

electron.on("exit", async (code) => {
  await server.close();
  process.exit(code ?? 0);
});
