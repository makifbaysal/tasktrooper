// Fails when a release has no decision about its "What's new" pop-up.
//
//   node --experimental-strip-types scripts/check-whats-new.mjs v0.3.13
//
// A version passes with an entry in src/lib/whatsNew/entries.ts, or with an
// explicit `{ version, skip: true }` when nothing in it is worth announcing.
// Pre-release tags (v1.2.3-beta) are not announced and always pass.
import path from "node:path";
import process from "node:process";
import { pathToFileURL } from "node:url";

const root = path.resolve(import.meta.dirname, "..");
const raw = process.argv[2] ?? "";
const version = raw.replace(/^v/i, "");
if (!/^\d+\.\d+\.\d+/.test(version)) {
  console.error(`usage: check-whats-new.mjs <vX.Y.Z>  (got "${raw}")`);
  process.exit(2);
}
if (version.includes("-")) {
  console.log(`${version} is a pre-release; no What's new needed.`);
  process.exit(0);
}

const { releaseNotesDecision } = await import(pathToFileURL(path.join(root, "src/lib/whatsNew/entries.ts")).href);
const decision = releaseNotesDecision(version);
if (decision === "missing") {
  console.error(
    `No What's new record for ${version}. Add an entry (or { version: "${version}", skip: true }) to desktop/ui/src/lib/whatsNew/entries.ts, with its strings in all 7 locales.`,
  );
  process.exit(1);
}
console.log(`What's new for ${version}: ${decision}.`);
