/* global process */
import { build } from "esbuild";
import { copyFileSync, existsSync, rmSync } from "node:fs";
import path from "node:path";

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
 */
const root = path.resolve(import.meta.dirname, "..");
const outdir = path.join(root, "dist", "embedder");

rmSync(outdir, { recursive: true, force: true });

// Two bundles: `index.cjs` is the process (HTTP server, download), and
// `worker.cjs` is the worker thread that holds the model, started from it by
// path. Only the worker pulls in onnxruntime-web; see worker-engine.ts.
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

process.stdout.write(`build-embedder: wrote ${outdir}\n`);
