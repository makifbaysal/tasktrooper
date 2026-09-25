import type {
  Component,
  ComponentCheck,
  ComponentLink,
  ComponentRole,
  ComponentStack,
  Confidence,
  Fact,
  LinkProtocol,
  LinkStatus,
  LocalCommand,
  ProjectScan,
  ProjectType,
  RepositoryModel,
  RepoShape,
  ResourceKind,
  ReviewItem,
  ScanStage,
  SourceEvidence,
  StackItem,
  SystemResource,
} from "@/api";

// The server's fixed scan pipeline (project_model.go's ScanStage* consts, in
// the order application/discovery runs them). Kept here, not in api.ts,
// because it is presentation order rather than part of the wire shape.
const SCAN_STAGES: ScanStage[] = [
  "clone",
  "inventory",
  "shape",
  "components",
  "stack",
  "checks",
  "links",
  "deploy",
  "match",
];

export function factValue<T>(fact: Fact<T> | undefined | null): T | undefined {
  return fact ? (fact.override ?? fact.detected) : undefined;
}

export function isOverridden<T>(fact: Fact<T> | undefined | null): boolean {
  return fact?.override !== undefined;
}

export function effectiveRole(component: Component): ComponentRole | undefined {
  return factValue(component.role);
}

/**
 * Mirrors the server's Component.DisplayName, except the root component
 * ("." or "") reads as the repository's own name here instead of the literal
 * "root" — this is what a human sees, not what a monorepo path resolver reads.
 */
export function componentLabel(component: Component, repositoryName: string): string {
  const name = factValue(component.name)?.trim();
  if (name) return name;
  if (component.path === "." || component.path === "") return repositoryName;
  const segments = component.path.split("/").filter(Boolean);
  return segments[segments.length - 1] || component.path;
}

/**
 * Mirrors the server's ComponentStack.Summary(limit): frameworks, then
 * languages, then libraries, each rendered "name version". limit <= 0 keeps
 * every item.
 */
export function stackSummary(stack: ComponentStack | undefined | null, limit = 0): string {
  if (!stack) return "";
  const parts: string[] = [];
  const add = (items: StackItem[] | undefined) => {
    for (const item of items ?? []) {
      parts.push(item.version ? `${item.name} ${item.version}` : item.name);
    }
  };
  add(stack.frameworks);
  add(stack.languages);
  add(stack.libraries);
  return (limit > 0 ? parts.slice(0, limit) : parts).join(" · ");
}

/** Mirrors the server's ComponentCheck.Required(): active, not missing, and
 * gated required. `componentId` narrows to one component; omitted counts them all. */
export function requiredChecks(checks: ComponentCheck[], componentId?: string): number {
  return checks.filter((check) => {
    if (componentId && check.component_id !== componentId) return false;
    return check.status === "active" && !check.missing && factValue(check.gate) === "required";
  }).length;
}

function repoSlashPath(repoName: string, path: string): string {
  return path === "." || path === "" ? repoName : `${repoName}/${path}`;
}

/**
 * A link's target, for display: a component resolves to "repo/path" — looked
 * up in the model's own components first, then linked_components (a
 * component that lives in another repository but is the other end of one of
 * this payload's links); a resource resolves to its name; an unresolved
 * suggestion falls back to the scan's hint.
 */
export function linkTargetLabel(link: ComponentLink, model: RepositoryModel): string {
  if (link.to_component_id) {
    const own = model.components.find((c) => c.id === link.to_component_id);
    if (own) return repoSlashPath(model.repository.name, own.path);
    const linked = model.linked_components.find((c) => c.id === link.to_component_id);
    if (linked) return repoSlashPath(linked.repository_name, linked.path);
    return link.hint ?? "";
  }
  if (link.to_resource_id) {
    const resource = model.resources.find((r) => r.id === link.to_resource_id);
    return resource?.name ?? link.hint ?? "";
  }
  return link.hint ?? "";
}

/**
 * Splits a repository model's full link set to the edges touching one
 * component: outgoing (it is the source, from `model.links`) and incoming
 * (it is the target, from `model.incoming_links`).
 */
export function groupLinks(
  model: Pick<RepositoryModel, "links" | "incoming_links">,
  componentId: string,
): { outgoing: ComponentLink[]; incoming: ComponentLink[] } {
  return {
    outgoing: model.links.filter((link) => link.from_component_id === componentId),
    incoming: model.incoming_links.filter((link) => link.to_component_id === componentId),
  };
}

export interface LinkGroup {
  key: string;
  links: ComponentLink[];
}

/**
 * Groups links that share a source component AND a resolved target
 * (to_resource_id, or to_component_id) into one row — a merge can leave
 * several links pointing at the same place. An unresolved link (neither
 * id set) never groups with anything else, since there is nothing to key
 * it on. Order is first appearance.
 */
export function groupLinksByTarget(links: ComponentLink[]): LinkGroup[] {
  const groups: LinkGroup[] = [];
  const byKey = new Map<string, LinkGroup>();
  for (const link of links) {
    const target = link.to_resource_id
      ? `r:${link.to_resource_id}`
      : link.to_component_id
        ? `c:${link.to_component_id}`
        : undefined;
    if (!target) {
      groups.push({ key: `u:${link.id}`, links: [link] });
      continue;
    }
    const key = `${link.from_component_id}:${target}`;
    const existing = byKey.get(key);
    if (existing) {
      existing.links.push(link);
    } else {
      const group: LinkGroup = { key, links: [link] };
      byKey.set(key, group);
      groups.push(group);
    }
  }
  return groups;
}

const CONFIDENCE_ORDER: Confidence[] = ["exact", "high", "medium", "low"];

/** suggested if any link is suggested, else confirmed if any is confirmed, else dismissed. */
export function groupStatus(links: ComponentLink[]): LinkStatus {
  if (links.some((l) => l.status === "suggested")) return "suggested";
  if (links.some((l) => l.status === "confirmed")) return "confirmed";
  return "dismissed";
}

export function groupProtocols(links: ComponentLink[]): LinkProtocol[] {
  const seen = new Set<LinkProtocol>();
  const out: LinkProtocol[] = [];
  for (const link of links) {
    if (!seen.has(link.protocol)) {
      seen.add(link.protocol);
      out.push(link.protocol);
    }
  }
  return out;
}

export function groupEnvVars(links: ComponentLink[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const link of links) {
    for (const v of link.env_vars ?? []) {
      if (!seen.has(v)) {
        seen.add(v);
        out.push(v);
      }
    }
  }
  return out;
}

export function groupIsAuto(links: ComponentLink[]): boolean {
  return links.length > 0 && links.every((l) => l.auto_confirmed);
}

export function groupIsMissing(links: ComponentLink[]): boolean {
  return links.length > 0 && links.every((l) => l.missing);
}

export function groupHighestConfidence(links: ComponentLink[]): Confidence {
  let best: Confidence = "low";
  let bestRank = CONFIDENCE_ORDER.length;
  for (const link of links) {
    const rank = CONFIDENCE_ORDER.indexOf(link.confidence);
    if (rank !== -1 && rank < bestRank) {
      bestRank = rank;
      best = link.confidence;
    }
  }
  return best;
}

export function groupFirstReason(links: ComponentLink[]): string | undefined {
  return links.find((l) => l.reason?.trim())?.reason;
}

export function groupEvidence(links: ComponentLink[]): SourceEvidence[] {
  return links.flatMap((l) => l.evidence ?? []);
}

// Infra kinds a scan is likely to split into more than one SystemResource for
// the same physical thing (e.g. "Database"/"PostgreSQL"); api/auth/email/... are
// left out since two different links to those are usually genuinely different.
const DUPLICATE_HINT_KINDS: ResourceKind[] = ["database", "cache", "queue", "storage", "search"];

export interface DuplicateResourceHint {
  componentId: string;
  kind: ResourceKind;
  resourceIds: string[];
}

/**
 * Per component, among the infra kinds above, flags a kind that a
 * component's non-dismissed outgoing links resolve to ≥2 different
 * resources — the classic "Database" + "PostgreSQL" split from a scan.
 * componentIds narrows to those components; omitted checks every component.
 */
export function findDuplicateResourceHints(
  links: ComponentLink[],
  resources: SystemResource[],
  componentIds?: string[],
): DuplicateResourceHint[] {
  const resourceById = new Map(resources.map((r) => [r.id, r]));
  const seenByGroup = new Map<string, Set<string>>();
  const order: string[] = [];
  for (const link of links) {
    if (link.status === "dismissed") continue;
    if (!link.to_resource_id) continue;
    if (componentIds && !componentIds.includes(link.from_component_id)) continue;
    const resource = resourceById.get(link.to_resource_id);
    if (!resource || !DUPLICATE_HINT_KINDS.includes(resource.kind)) continue;
    const groupKey = `${link.from_component_id}:${resource.kind}`;
    let set = seenByGroup.get(groupKey);
    if (!set) {
      set = new Set();
      seenByGroup.set(groupKey, set);
      order.push(groupKey);
    }
    set.add(resource.id);
  }
  const hints: DuplicateResourceHint[] = [];
  for (const groupKey of order) {
    const ids = seenByGroup.get(groupKey)!;
    if (ids.size < 2) continue;
    const separatorIndex = groupKey.indexOf(":");
    hints.push({
      componentId: groupKey.slice(0, separatorIndex),
      kind: groupKey.slice(separatorIndex + 1) as ResourceKind,
      resourceIds: [...ids],
    });
  }
  return hints;
}

/** Mirrors the server's ComputeProjectType. */
export function projectTypeOf(shapes: RepoShape[]): ProjectType {
  if (shapes.length === 0) return "empty";
  if (shapes.length === 1) return shapes[0] === "monorepo" ? "monorepo" : "single_repo";
  return "multi_repo";
}

export function reviewCount(items: ReviewItem[] | undefined | null): number {
  return items?.length ?? 0;
}

/** The scan stages' fixed presentation order. */
export function scanStagesInOrder(): ScanStage[] {
  return SCAN_STAGES;
}

export function isScanFinished(scan: Pick<ProjectScan, "status"> | null | undefined): boolean {
  return scan?.status === "succeeded" || scan?.status === "failed";
}

/** Mirrors the server's LocalCommand.Display(): "cd <dir> && <argv…>", or
 * just the argv when dir is empty/the component root. */
/** Commands sharing a directory run under one `cd`; groups are separated by "; " since every dir is repo-relative. */
export function localCommandsDisplay(commands: LocalCommand[]): string {
  const groups: { dir: string; cmds: string[] }[] = [];
  for (const c of commands) {
    const dir = c.dir || ".";
    const last = groups[groups.length - 1];
    if (last && last.dir === dir) last.cmds.push(c.argv.join(" "));
    else groups.push({ dir, cmds: [c.argv.join(" ")] });
  }
  return groups
    .map((g) => (g.dir === "." ? g.cmds.join(" && ") : `cd ${g.dir} && ${g.cmds.join(" && ")}`))
    .join("; ");
}

export function localCommandDisplay(command: LocalCommand): string {
  const cmd = command.argv.join(" ");
  if (!command.dir || command.dir === ".") return cmd;
  return `cd ${command.dir} && ${cmd}`;
}
