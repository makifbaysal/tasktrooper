import { build } from "esbuild";
import { rmSync } from "node:fs";
import path from "node:path";

/**
 * Bundles the main process and the three preloads.
 *
 * CommonJS, and `.cjs` extensions, because package.json says `"type":
 * "module"`: Electron's main process and preload scripts load more reliably as
 * CJS, and a `.js` file in a `type: module` package would be parsed as ESM.
 *
 * Bundled rather than transpiled file-by-file so a packaged app has three
 * files to sign instead of a node_modules tree to prune. `electron` is
 * external because it is provided by the runtime, not by npm, at run time.
 */
const root = path.resolve(import.meta.dirname, "..");
const outdir = path.join(root, "dist");

rmSync(path.join(outdir, "main"), { recursive: true, force: true });
rmSync(path.join(outdir, "preload"), { recursive: true, force: true });

const common = {
  bundle: true,
  platform: "node",
  format: "cjs",
  target: "node22",
  outExtension: { ".js": ".cjs" },
  external: ["electron"],
  logLevel: "info",
  // The main process handles secrets. A sourcemap is one more artefact that
  // could carry a literal out of that code into a file someone can open, and
  // nothing about a packaged desktop app needs one shipped.
  sourcemap: false,
  minify: false,
};

await build({ ...common, entryPoints: [path.join(root, "src/main/index.ts")], outfile: path.join(outdir, "main/index.cjs") });
await build({
  ...common,
  entryPoints: [path.join(root, "src/preload/index.ts")],
  outfile: path.join(outdir, "preload/index.cjs"),
});
await build({
  ...common,
  entryPoints: [path.join(root, "src/preload/cloud.ts")],
  outfile: path.join(outdir, "preload/cloud.cjs"),
});
await build({
  ...common,
  entryPoints: [path.join(root, "src/preload/keys.ts")],
  outfile: path.join(outdir, "preload/keys.cjs"),
});
