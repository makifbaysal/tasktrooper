// The release notes shown in the "What's new" pop-up on the first launch of a
// version. One record per released version, newest last; the wording of each
// lives in the locale catalogs under `whatsNew.v<major>_<minor>_<patch>`.
//
// Every tagged release needs a record here: either an entry (3-6 `items`, each
// the key of a string in `whatsNew.v….items`) or `{ version, skip: true }` for
// a release with nothing a person would notice. scripts/check-whats-new.mjs
// fails the release workflow when neither exists for the tag.
//
// Plain data and pure functions only: that script loads this file with Node's
// type stripping, so no imports and no TypeScript-only runtime syntax.

export interface WhatsNewEntry {
  version: string;
  items: readonly string[];
}

export interface WhatsNewSkip {
  version: string;
  skip: true;
}

export type WhatsNewRecord = WhatsNewEntry | WhatsNewSkip;

export const WHATS_NEW: readonly WhatsNewRecord[] = [
  {
    version: "0.3.16",
    items: ["pmAudienceTest", "studyRerun", "completeTasks", "chatThinking", "serialReleases", "mascot"],
  },
  {
    version: "0.3.15",
    items: ["agentsMd", "assignTasks", "dockerCommands"],
  },
  {
    version: "0.3.14",
    items: ["phoneAudience", "phoneStudies", "phoneReports"],
  },
  {
    version: "0.3.13",
    items: ["tunnel", "audience", "telemetry", "repoSetup", "runTrace", "reviewHistory"],
  },
];

export function normalizeVersion(version: string): string {
  return version.trim().replace(/^v/i, "");
}

export function whatsNewKey(version: string): string {
  return `v${normalizeVersion(version).replace(/\./g, "_")}`;
}

function parts(version: string): number[] {
  return normalizeVersion(version)
    .split("-")[0]!
    .split(".")
    .map((n) => Number.parseInt(n, 10) || 0);
}

export function compareVersions(a: string, b: string): number {
  const x = parts(a);
  const y = parts(b);
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] ?? 0) - (y[i] ?? 0);
    if (d !== 0) return d;
  }
  return 0;
}

function isEntry(record: WhatsNewRecord): record is WhatsNewEntry {
  return "items" in record && record.items.length > 0;
}

export function findWhatsNew(version: string, records: readonly WhatsNewRecord[] = WHATS_NEW): WhatsNewEntry | null {
  const wanted = normalizeVersion(version);
  const record = records.find((r) => normalizeVersion(r.version) === wanted);
  return record && isEntry(record) ? record : null;
}

export function latestWhatsNew(records: readonly WhatsNewRecord[] = WHATS_NEW): WhatsNewEntry | null {
  const entries = records.filter(isEntry);
  if (entries.length === 0) return null;
  return entries.reduce((best, e) => (compareVersions(e.version, best.version) > 0 ? e : best));
}

export type ReleaseNotesDecision = "entry" | "skip" | "missing";

export function releaseNotesDecision(version: string, records: readonly WhatsNewRecord[] = WHATS_NEW): ReleaseNotesDecision {
  const wanted = normalizeVersion(version);
  const record = records.find((r) => normalizeVersion(r.version) === wanted);
  if (!record) return "missing";
  return isEntry(record) ? "entry" : "skip" in record && record.skip ? "skip" : "missing";
}
