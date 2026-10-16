// Which developer tools a team's agents need on this computer, and the pure
// rules the first-run step and Settings → Development environment read them
// through. The job-state helpers the header also needs are in toolchainJob.ts,
// so this table stays out of the entry chunk.
//
// Needs are keyed by catalog slug. What applies on this OS is the shell's
// answer, not ours: an id missing from the report (no `xcode` on Windows) is
// simply never recommended.

import type { Agent } from "@/api";
import type { ToolchainId, ToolchainItem, ToolchainReport } from "@/lib/desktop-bridge";
import { CORE_AGENT_SLUGS, agentsUseCatalogSlug, findAgentBySlug } from "@/lib/teamTemplates";

export type ToolchainNeedLevel = "recommended" | "optional";

interface AgentToolNeeds {
  recommended: readonly ToolchainId[];
  optional: readonly ToolchainId[];
}

/** Every agent runs through git and Claude Code, whatever the team. */
export const BASE_TOOLCHAIN: readonly ToolchainId[] = ["git", "claude-code"];

export const AGENT_TOOLCHAIN: Readonly<Record<string, AgentToolNeeds>> = {
  "frontend-developer": { recommended: ["node", "chrome"], optional: [] },
  "ui-designer": { recommended: ["chrome", "node"], optional: [] },
  "backend-developer": { recommended: [], optional: ["go", "jdk", "node", "dotnet", "rust", "docker"] },
  "mobile-developer": {
    recommended: [
      "xcode-clt",
      "xcode",
      "cocoapods",
      "jdk",
      "android-sdk",
      "node",
      "appium",
      "appium-xcuitest",
      "appium-uiautomator2",
    ],
    optional: ["flutter"],
  },
  "qa-agent": { recommended: ["chrome"], optional: [] },
  "data-scientist": { recommended: ["python", "uv"], optional: ["duckdb", "docker"] },
  "game-developer": { recommended: [], optional: ["godot", "unity-hub"] },
  "security-agent": { recommended: [], optional: ["uv", "osv-scanner"] },
  "release-engineer": { recommended: [], optional: ["docker"] },
};

// QA drives the simulators and emulators only when there is a mobile app to
// test; on any other team the hub and the Android SDK are dead weight.
const QA_MOBILE_TOOLCHAIN: readonly ToolchainId[] = ["appium", "appium-xcuitest", "appium-uiautomator2", "android-sdk"];

export interface ToolchainNeed {
  level: ToolchainNeedLevel;
  /** Catalog slugs of the enabled agents that use it; empty for the base tools every agent relies on. */
  reasons: string[];
}

/** What the enabled agents need, whatever this computer already has or can run. */
export function toolchainNeeds(enabledSlugs: Iterable<string>): Map<ToolchainId, ToolchainNeed> {
  const slugs = new Set(enabledSlugs);
  const needs = new Map<ToolchainId, ToolchainNeed>();
  const add = (id: ToolchainId, level: ToolchainNeedLevel, slug: string | null) => {
    const need = needs.get(id) ?? { level, reasons: [] };
    if (level === "recommended") need.level = "recommended";
    if (slug && !need.reasons.includes(slug)) need.reasons.push(slug);
    needs.set(id, need);
  };

  for (const id of BASE_TOOLCHAIN) add(id, "recommended", null);
  for (const slug of slugs) {
    const own = AGENT_TOOLCHAIN[slug];
    if (!own) continue;
    for (const id of own.recommended) add(id, "recommended", slug);
    for (const id of own.optional) add(id, "optional", slug);
  }
  if (slugs.has("qa-agent") && slugs.has("mobile-developer")) {
    for (const id of QA_MOBILE_TOOLCHAIN) add(id, "recommended", "qa-agent");
  }
  // Base tools stay "every agent" even when an agent also lists them.
  for (const id of BASE_TOOLCHAIN) {
    const need = needs.get(id);
    if (need) need.reasons = [];
  }
  return needs;
}

/**
 * The missing `manual` tools an item waits on, through its dependencies that
 * are not installed yet. The shell never installs a manual tool, so the item
 * cannot be installed here until the person has installed those by hand (no
 * winget → Node.js is manual → Appium waits on it).
 */
export function manualBlockers(item: ToolchainItem, report: ToolchainReport): ToolchainItem[] {
  const byId = new Map(report.items.map((entry) => [entry.id, entry]));
  const seen = new Set<ToolchainId>([item.id]);
  const blockers: ToolchainItem[] = [];
  const visit = (ids: readonly ToolchainId[]) => {
    for (const id of ids) {
      if (seen.has(id)) continue;
      seen.add(id);
      const dep = byId.get(id);
      if (!dep || dep.status === "installed") continue;
      if (dep.installKind === "manual") blockers.push(dep);
      else visit(dep.dependsOn);
    }
  };
  visit(item.dependsOn);
  return blockers;
}

export interface ToolchainRecommendation extends ToolchainNeed {
  id: ToolchainId;
  item: ToolchainItem;
  /** Missing manual tools it waits on; see `manualBlockers`. */
  blockedBy: ToolchainItem[];
  /** Neither manual nor blocked: the shell can install it. */
  installable: boolean;
  /** Checked on first sight: recommended and installable. */
  preselected: boolean;
}

function teamEntries(enabledSlugs: Iterable<string>, report: ToolchainReport): ToolchainRecommendation[] {
  const needs = toolchainNeeds(enabledSlugs);
  const entries: ToolchainRecommendation[] = [];
  for (const item of report.items) {
    const need = needs.get(item.id);
    if (!need) continue;
    const blockedBy = item.status === "installed" ? [] : manualBlockers(item, report);
    const installable = item.installKind !== "manual" && blockedBy.length === 0;
    entries.push({
      id: item.id,
      item,
      level: need.level,
      reasons: need.reasons,
      blockedBy,
      installable,
      preselected: need.level === "recommended" && installable,
    });
  }
  return entries;
}

/** The team's tools this computer lacks (missing or not usable), in report order. */
export function recommendFor(enabledSlugs: Iterable<string>, report: ToolchainReport): ToolchainRecommendation[] {
  return teamEntries(enabledSlugs, report).filter((entry) => entry.item.status !== "installed");
}

/** The team's tools this computer already has. */
export function installedFor(enabledSlugs: Iterable<string>, report: ToolchainReport): ToolchainRecommendation[] {
  return teamEntries(enabledSlugs, report).filter((entry) => entry.item.status === "installed");
}

export function preselectedIds(recommendations: readonly ToolchainRecommendation[]): ToolchainId[] {
  return recommendations.filter((entry) => entry.preselected).map((entry) => entry.id);
}

export type ToolchainGroupId = "basics" | "web" | "backend" | "mobile" | "data" | "games" | "infra";

export const TOOLCHAIN_GROUPS: readonly { id: ToolchainGroupId; ids: readonly ToolchainId[] }[] = [
  { id: "basics", ids: ["homebrew", "git", "claude-code", "node", "python", "uv"] },
  { id: "web", ids: ["chrome"] },
  // Node.js is under basics: the frontend and the tooling need it as much as a TypeScript service does.
  { id: "backend", ids: ["go", "jdk", "dotnet", "rust"] },
  {
    id: "mobile",
    ids: [
      "xcode",
      "xcode-clt",
      "cocoapods",
      "android-sdk",
      "appium",
      "appium-xcuitest",
      "appium-uiautomator2",
      "flutter",
    ],
  },
  { id: "data", ids: ["duckdb"] },
  { id: "games", ids: ["godot", "unity-hub"] },
  { id: "infra", ids: ["docker", "osv-scanner"] },
];

export interface ToolchainGroup {
  id: ToolchainGroupId;
  items: ToolchainItem[];
}

/** The report's items under their group, in group order; a group with nothing on this OS is left out. */
export function groupReport(report: ToolchainReport): ToolchainGroup[] {
  const byId = new Map(report.items.map((item) => [item.id, item]));
  return TOOLCHAIN_GROUPS.map((group) => ({
    id: group.id,
    items: group.ids.map((id) => byId.get(id)).filter((item): item is ToolchainItem => item !== undefined),
  })).filter((group) => group.items.length > 0);
}

/**
 * The dependencies `plan` added in front of a selection, as labels — the
 * "installed first" preview. Ids missing from the report fall back to the id.
 */
export function planExtras(
  planned: readonly ToolchainId[],
  selected: Iterable<ToolchainId>,
  report: ToolchainReport | null,
): string[] {
  const chosen = new Set(selected);
  const labels = new Map(report?.items.map((item) => [item.id, item.label]) ?? []);
  return planned.filter((id) => !chosen.has(id)).map((id) => labels.get(id) ?? id);
}

export interface TeamMember {
  slug: string;
  name: string;
}

/**
 * The enabled agents as catalog slugs with their display names. A server old
 * enough to omit `catalog_slug` is matched by name against the slugs this
 * module knows, the same fallback `findAgentBySlug` uses.
 */
export function enabledTeam(agents: readonly Agent[]): TeamMember[] {
  const enabled = agents.filter((agent) => agent.enabled);
  if (agentsUseCatalogSlug(agents)) {
    return enabled
      .filter((agent) => Boolean(agent.catalog_slug))
      .map((agent) => ({ slug: agent.catalog_slug as string, name: agent.name }));
  }
  const known = new Set<string>([...CORE_AGENT_SLUGS, ...Object.keys(AGENT_TOOLCHAIN)]);
  const members: TeamMember[] = [];
  for (const slug of known) {
    const agent = findAgentBySlug(enabled, slug);
    if (agent) members.push({ slug, name: agent.name });
  }
  return members;
}

type Translate = (key: string, params?: Record<string, string | number>) => string;

/** "For Mobile Developer, QA Agent", or "Every agent uses it" for a base tool. */
export function toolchainReason(
  t: Translate,
  reasons: readonly string[],
  names: Readonly<Record<string, string>>,
): string {
  if (reasons.length === 0) return t("toolchain.reasons.everyone");
  return t("toolchain.reasons.agents", { agents: reasons.map((slug) => names[slug] ?? slug).join(", ") });
}

/** "Via Homebrew · Opens an installer window" — how this OS installs it. */
export function toolchainMethodLine(t: Translate, item: Pick<ToolchainItem, "method" | "installKind">): string {
  const parts: string[] = [];
  if (item.method) parts.push(t("toolchain.method", { method: item.method }));
  if (item.installKind !== "auto") parts.push(t(`toolchain.kind.${item.installKind}`));
  return parts.join(" · ");
}
