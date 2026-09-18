import type { DependencyTargetKind, Repository } from "@/api";

/**
 * Repositories selectable as a dependency target. A `repo` target excludes
 * the current repository (no self-dependency). A `sub_repo` target keeps it
 * IN the list when it is itself a monorepo, so its own sibling sub-projects
 * become reachable — this repo's root_path is the monorepo checkout the
 * sub-project actually lives under.
 */
export function dependencyTargetRepositories(
  repositories: Repository[],
  repositoryId: string,
  targetKind: DependencyTargetKind,
): Repository[] {
  if (targetKind === "sub_repo") {
    const current = repositories.find((r) => r.id === repositoryId);
    if (current?.kind === "monorepo") {
      return repositories;
    }
  }
  return repositories.filter((r) => r.id !== repositoryId);
}
