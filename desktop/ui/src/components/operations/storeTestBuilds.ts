import type {
  MobileStorePlatform,
  SimulatorRunStatus,
  StoreTestBuild,
  StoreTestBuildStatus,
  StoreTestGroup,
} from "@/api";
import type { BadgeProps } from "@/components/ui/badge";

/**
 * The test-build vocabulary (TestFlight / Play internal app sharing), the
 * sibling of the release-channel one in `StoreReleaseControls`. The task
 * drawer's build panel and the Operations app drawer both read their status
 * labels, colours and build names from here — neither grows its own switch.
 */

export const STORE_PLATFORMS: MobileStorePlatform[] = ["ios", "android"];

export const storePlatformLabelKey = (platform: MobileStorePlatform) => `operations.storeTest.platforms.${platform}`;

/** Still moving on its own. action_required is not: it waits on a person, so nothing polls for it. */
export const ACTIVE_TEST_BUILD_STATUSES: StoreTestBuildStatus[] = ["queued", "building", "processing"];

export function isTestBuildActive(status: string): boolean {
  return (ACTIVE_TEST_BUILD_STATUSES as string[]).includes(status);
}

const KNOWN_TEST_BUILD_STATUSES = new Set<string>([
  "queued",
  "building",
  "processing",
  "action_required",
  "ready",
  "failed",
  "dispatched",
]);

/** A status this build has never heard of reads "Unknown", never the raw key. */
export const testBuildStatusLabelKey = (status: string) =>
  KNOWN_TEST_BUILD_STATUSES.has(status)
    ? `operations.storeTest.status.${status}`
    : "operations.storeTest.status.unknown";

export function testBuildStatusBadgeVariant(status: string): BadgeProps["variant"] {
  switch (status) {
    case "ready":
      return "success";
    case "failed":
      return "destructive";
    case "action_required":
      return "warning";
    case "queued":
    case "building":
    case "processing":
    case "dispatched":
      return "info";
    default:
      return "outline";
  }
}

/** A dispatched build lives on in its GitHub Actions run; nothing here acts on it. */
export function isTestBuildActionable(status: string): boolean {
  return status !== "dispatched";
}

export function shortSha(sha?: string): string {
  return (sha ?? "").trim().slice(0, 7);
}

/** "T-54 · #2 · a1b2c3d" — mirrors the server's StoreTestBuild.Label(). */
export function testBuildLabel(build: Pick<StoreTestBuild, "task_key" | "attempt" | "commit_sha">): string {
  const parts: string[] = [];
  if (build.task_key) parts.push(build.task_key);
  if (build.attempt > 0) parts.push(`#${build.attempt}`);
  const sha = shortSha(build.commit_sha);
  if (sha) parts.push(sha);
  return parts.join(" · ");
}

export function testBuildGroups(build: Pick<StoreTestBuild, "groups">): string[] {
  return build.groups ?? [];
}

/** True when the build waits on the developer's export compliance answer. */
export function needsExportCompliance(build: Pick<StoreTestBuild, "status" | "failure">): boolean {
  return build.status === "action_required" && (build.failure === "export_compliance" || !build.failure);
}

export interface PlatformBuilds {
  latest: StoreTestBuild | null;
  older: StoreTestBuild[];
}

/** Splits a newest-first list into each platform's latest build and its older attempts. */
export function buildsByPlatform(builds: StoreTestBuild[]): Record<MobileStorePlatform, PlatformBuilds> {
  const out: Record<MobileStorePlatform, PlatformBuilds> = {
    ios: { latest: null, older: [] },
    android: { latest: null, older: [] },
  };
  for (const build of builds) {
    const slot = out[build.platform];
    if (!slot) continue;
    if (slot.latest) slot.older.push(build);
    else slot.latest = build;
  }
  return out;
}

/**
 * Folds builds the server just answered with into a newest-first list: a known
 * id is replaced in place, a new one goes on top.
 */
export function mergeTestBuilds(list: StoreTestBuild[], incoming: StoreTestBuild[]): StoreTestBuild[] {
  const byId = new Map(incoming.map((b) => [b.id, b]));
  const fresh = incoming.filter((b) => !list.some((existing) => existing.id === b.id));
  return [...fresh, ...list.map((b) => byId.get(b.id) ?? b)];
}

const KNOWN_GROUP_KINDS = new Set<string>(["internal", "external", "closed", "open"]);

export const testGroupKindLabelKey = (kind: string) =>
  KNOWN_GROUP_KINDS.has(kind) ? `operations.storeTest.groupKinds.${kind}` : "operations.storeTest.groupKinds.unknown";

/** An external TestFlight group only gets a build after Apple's Beta App Review. */
export function needsBetaReview(group: Pick<StoreTestGroup, "platform" | "kind">): boolean {
  return group.platform === "ios" && group.kind === "external";
}

/** iOS group ids are opaque; Play track ids are their names, so an unresolved id still reads. */
export function testGroupName(id: string, groups: StoreTestGroup[] | null): string {
  return groups?.find((g) => g.id === id)?.name ?? id;
}

/**
 * The set PUT .../test-groups/auto takes after flipping one group. all_builds
 * groups get every build regardless, so they are never part of the set.
 */
export function autoGroupSet(groups: StoreTestGroup[], toggledId: string, on: boolean): string[] {
  return groups
    .filter((g) => !g.all_builds && (g.id === toggledId ? on : g.auto_distribute))
    .map((g) => g.id);
}

export const ACTIVE_SIMULATOR_RUN_STATUSES: SimulatorRunStatus[] = ["preparing", "building", "installing", "launching"];

export function isSimulatorRunActive(status: string): boolean {
  return (ACTIVE_SIMULATOR_RUN_STATUSES as string[]).includes(status);
}

const KNOWN_SIMULATOR_RUN_STATUSES = new Set<string>([...ACTIVE_SIMULATOR_RUN_STATUSES, "running", "failed"]);

export const simulatorRunStatusLabelKey = (status: string) =>
  KNOWN_SIMULATOR_RUN_STATUSES.has(status)
    ? `operations.storeTest.simulator.status.${status}`
    : "operations.storeTest.simulator.status.unknown";

export function simulatorRunStatusBadgeVariant(status: string): BadgeProps["variant"] {
  if (status === "running") return "success";
  if (status === "failed") return "destructive";
  return isSimulatorRunActive(status) ? "info" : "outline";
}

export function tailLines(text: string | undefined, max: number): string {
  if (!text) return "";
  const lines = text.replace(/\s+$/, "").split("\n");
  return lines.slice(-max).join("\n");
}
