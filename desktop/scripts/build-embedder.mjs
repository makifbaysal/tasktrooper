/* global process, Buffer, fetch */
import { build } from "esbuild";
import { createHash } from "node:crypto";
import { copyFileSync, createReadStream, createWriteStream, existsSync, mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { Readable, Transform } from "node:stream";
import { pipeline } from "node:stream/promises";

/**
 * Bundles the embedder — the third supervised child, alongside the runner
 * and Appium — into one CommonJS file. A separate esbuild invocation from
 * `build-main.mjs` on purpose: this ships and is spawned independently
 * (`process.execPath`, `ELECTRON_RUN_AS_NODE=1`), not loaded by Electron's
 * own module system, so it has no reason to share a build step with main.
 *
 * `.cjs`, CommonJS, `platform: "node"`: the same reasons as `build-main.mjs`
 * — this package says `"type": "module"`, so a bundled `.js` here would be
 * parsed as ESM, and `onnxruntime-web`'s own Node build (`dist/ort.node.min.js`,
 * what its root import resolves to under Node's `require`) is CJS itself.
 *
 * After the bundle, the exact pair of WASM runtime files that build actually
 * loads is copied alongside it: `ort-wasm-simd-threaded.mjs` and its
 * `.wasm`, confirmed by grepping the built `onnxruntime-web` for the literal
 * filename it references (not the `.jsep`/`.jspi`/`.asyncify` variants nothing
 * in that path touches). `ort.env.wasm.wasmPaths` is pointed at this same
 * directory at runtime (`engine.ts`) rather than relying on relative
 * auto-resolution, because a packaged app's `extraResources` stages
 * `dist/embedder` as a flat directory that does not mirror `node_modules` —
 * proven necessary by the go/no-go prototype's packaged-layout simulation.
 *
 * Last, the voices (EMA and tiny-voca): the files pinned in `embedder/src/tts/files.ts`,
 * staged into `voice/` beside the bundle, because the app ships them rather
 * than downloading them at run time. They are fetched once into
 * `.cache/voice/` (gitignored) and every copy is SHA-256-checked on every
 * build, cached or not; a mismatch fails the build. `TASKTROOPER_VOICE_MODELS_DIR`
 * points at a directory holding the same files instead (offline builds, a new
 * export being tried), and those are held to the same hashes.
 */
const root = path.resolve(import.meta.dirname, "..");
const outdir = path.join(root, "dist", "embedder");

rmSync(outdir, { recursive: true, force: true });

// Three bundles: `index.cjs` is the process (HTTP server, download),
// `worker.cjs` is the worker thread that holds the embedding model, and
// `tts-worker.cjs` the one that holds the voice, both started from it by
// path. Only the workers pull in onnxruntime-web; see worker-engine.ts.
const common = {
  bundle: true,
  platform: "node",
  format: "cjs",
  target: "node22",
  external: ["electron"],
  logLevel: "info",
  // Same reasoning as build-main.mjs: this process reads the runner token's
  // own tenant back out of stdin, and a sourcemap is one more artefact that
  // could carry a literal into a file someone can open.
  sourcemap: false,
  minify: false,
};

await build({ ...common, entryPoints: [path.join(root, "embedder/src/index.ts")], outfile: path.join(outdir, "index.cjs") });
await build({ ...common, entryPoints: [path.join(root, "embedder/src/worker.ts")], outfile: path.join(outdir, "worker.cjs") });

const wasmSrcDir = path.join(root, "node_modules", "onnxruntime-web", "dist");
const WASM_RUNTIME_FILES = ["ort-wasm-simd-threaded.mjs", "ort-wasm-simd-threaded.wasm"];

for (const name of WASM_RUNTIME_FILES) {
  const src = path.join(wasmSrcDir, name);
  if (!existsSync(src)) {
    throw new Error(`build-embedder: expected ${src} to exist — has onnxruntime-web's dist/ layout changed?`);
  }
  copyFileSync(src, path.join(outdir, name));
}

await build({ ...common, entryPoints: [path.join(root, "embedder/src/tts/worker.ts")], outfile: path.join(outdir, "tts-worker.cjs") });

await stageVoice();

process.stdout.write(`build-embedder: wrote ${outdir}\n`);

async function stageVoice() {
  const { VOICE_DIR_NAME, VOICE_FILES, VOICE_NOTICE, VOICE_LICENSES } = await loadVoiceFiles();
  const override = process.env.TASKTROOPER_VOICE_MODELS_DIR;
  const cacheDir = path.join(root, ".cache", "voice");
  const voiceOut = path.join(outdir, VOICE_DIR_NAME);
  mkdirSync(voiceOut, { recursive: true });
  for (const file of VOICE_FILES) {
    let src;
    if (override) {
      src = path.join(override, file.name);
      if (!existsSync(src)) throw new Error(`build-embedder: ${src} is missing (TASKTROOPER_VOICE_MODELS_DIR)`);
      await verify(src, file);
    } else {
      src = path.join(cacheDir, file.name);
      if (!existsSync(src) || !(await matches(src, file))) await download(file, src);
    }
    copyFileSync(src, path.join(voiceOut, file.name));
  }
  const licenses = VOICE_LICENSES.map((name) => [name, readFileSync(path.join(voiceOut, name), "utf8")]);
  writeFileSync(path.join(voiceOut, "NOTICE.txt"), voiceNotice(VOICE_NOTICE, licenses));
  process.stdout.write(`build-embedder: staged ${VOICE_FILES.length} voice files${override ? ` from ${override}` : ""}\n`);
}

/** The attribution, then each license text in full. */
function voiceNotice(notice, licenses) {
  return [notice, ...licenses.map(([name, text]) => `==== ${name} ====\n\n${text}`)].join("\n\n");
}

/**
 * files.ts is TypeScript and stays the one copy of the list, so it is bundled
 * in memory and imported from that, rather than duplicated here or left to a
 * Node version that strips types by itself.
 */
async function loadVoiceFiles() {
  const out = await build({
    entryPoints: [path.join(root, "embedder/src/tts/files.ts")],
    bundle: true,
    format: "esm",
    platform: "node",
    write: false,
    logLevel: "silent",
  });
  const code = out.outputFiles[0].text;
  return import(`data:text/javascript;base64,${Buffer.from(code).toString("base64")}`);
}

async function sha256(file) {
  const hash = createHash("sha256");
  await pipeline(createReadStream(file), hash);
  return hash.digest("hex");
}

async function matches(file, pinned) {
  return (await sha256(file)) === pinned.sha256;
}

async function verify(file, pinned) {
  const actual = await sha256(file);
  if (actual !== pinned.sha256) {
    throw new Error(`build-embedder: ${file} has SHA-256 ${actual}, but ${pinned.name} is pinned to ${pinned.sha256}`);
  }
}

/** Into a temporary name, hashed as it streams, renamed into place only once it matches. */
async function download(pinned, dest) {
  mkdirSync(path.dirname(dest), { recursive: true });
  process.stdout.write(`build-embedder: downloading ${pinned.url}\n`);
  const res = await fetch(pinned.url);
  if (!res.ok || !res.body) throw new Error(`build-embedder: GET ${pinned.url} answered ${res.status}`);
  const tmp = `${dest}.partial`;
  const hash = createHash("sha256");
  const tee = new Transform({
    transform(chunk, _encoding, callback) {
      hash.update(chunk);
      callback(null, chunk);
    },
  });
  try {
    await pipeline(Readable.fromWeb(res.body), tee, createWriteStream(tmp));
    const actual = hash.digest("hex");
    if (actual !== pinned.sha256) {
      throw new Error(`build-embedder: ${pinned.url} has SHA-256 ${actual}, but it is pinned to ${pinned.sha256}`);
    }
    renameSync(tmp, dest);
  } finally {
    rmSync(tmp, { force: true });
  }
}
