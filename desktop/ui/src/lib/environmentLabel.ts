import type { DeployEnvironment } from "@/api";

const PRODUCTION_WORDS = new Set(["prod", "prd", "production", "live"]);
// "non-prod", "not-live", "pre-prod": a production word with one of these
// before it is not production.
const NEGATING_WORDS = new Set(["pre", "non", "not"]);
const STAGING_WORDS = new Set(["stage", "staging", "stg", "preprod", "uat", "qa", "test"]);
const PREVIEW_WORDS = new Set(["preview"]);
const DEVELOPMENT_WORDS = new Set(["dev", "development", "local"]);

function words(label: string): string[] {
  return label.split(/[^a-z0-9]+/).filter(Boolean);
}

// Alert senders and the deploy watch write their own shorthand ("prod",
// "prd", "eu-prod", "stage", "preprod", "uat"). Only an empty label and a
// production word (alone or with a region around it) are production: a
// pre-production label read as production would paint the production row
// red, so anything unrecognised matches no environment and callers show it
// on its own.
export function environmentFromLabel(label: string | undefined | null): DeployEnvironment | undefined {
  const e = (label ?? "").trim().toLowerCase();
  if (e === "") return "production";
  if (e.startsWith("stag") || e === "stg" || e.startsWith("preprod") || e.startsWith("pre-prod") || e === "uat" || e === "qa" || e === "test") {
    return "staging";
  }
  if (e === "live" || e.startsWith("prod")) return "production";
  const parts = words(e);
  const negated = parts.some((w) => NEGATING_WORDS.has(w));
  if (!negated && parts.some((w) => PRODUCTION_WORDS.has(w))) return "production";
  if (/(^|[^a-z])pre-?prod/.test(e) || parts.some((w) => STAGING_WORDS.has(w))) return "staging";
  if (e.startsWith("prev") || parts.some((w) => PREVIEW_WORDS.has(w))) return "preview";
  if (e.startsWith("dev") || parts.some((w) => DEVELOPMENT_WORDS.has(w))) return "development";
  return undefined;
}
