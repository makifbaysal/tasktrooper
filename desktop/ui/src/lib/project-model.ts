import type {
  Component,
  ComponentCheck,
  ComponentLink,
  ComponentRole,
  ComponentStack,
  Fact,
  LocalCommand,
  ProjectScan,
  ProjectType,
  RepositoryModel,
  RepoShape,
  ReviewItem,
  ScanStage,
  StackItem,
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
  "notes",
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
