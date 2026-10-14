import { TOOLCHAIN_IDS, type ToolchainId, type ToolchainItem } from "../../ipc/types.js";

/**
 * What an install of `requested` will actually run, in order. Pure: the input
 * is a report's items, so the rules are tested on literal reports.
 *
 *  - Only tools that apply to this OS: an id with no item is dropped.
 *  - Each one's missing dependencies come with it, transitively.
 *  - Installed ones are dropped — requested or pulled in.
 *  - `manual` ones are dropped: nothing here can run them, and their item
 *    already says where to go. A tool that needs one is still planned; when
 *    its turn comes it is skipped with an error naming what it needs.
 *  - Dependencies first; otherwise the `TOOLCHAIN_IDS` order, so the same
 *    request always gives the same list.
 */

export type PlanItem = Pick<ToolchainItem, "id" | "status" | "installKind" | "dependsOn">;

const RANK = new Map<ToolchainId, number>(TOOLCHAIN_IDS.map((id, i) => [id, i]));
const rank = (id: ToolchainId): number => RANK.get(id) ?? Number.MAX_SAFE_INTEGER;

export class PlanCycleError extends Error {
  constructor(ids: readonly ToolchainId[]) {
    super(`The toolchain table has a dependency cycle among: ${ids.join(", ")}`);
    this.name = "PlanCycleError";
  }
}

export function planInstall(requested: readonly ToolchainId[], items: readonly PlanItem[]): ToolchainId[] {
  const byId = new Map(items.map((item) => [item.id, item]));
  const needed = (id: ToolchainId): boolean => byId.get(id)?.status !== "installed";

  const wanted = new Set<ToolchainId>();
  const visit = (id: ToolchainId): void => {
    const item = byId.get(id);
    if (!item || wanted.has(id)) return;
    wanted.add(id);
    for (const dep of item.dependsOn) if (needed(dep)) visit(dep);
  };
  for (const id of requested) visit(id);

  const selected = [...wanted].filter((id) => needed(id) && byId.get(id)?.installKind !== "manual");
  return topologicalOrder(selected, (id) => (byId.get(id)?.dependsOn ?? []).filter((dep) => selected.includes(dep)));
}

/** Kahn's algorithm, always taking the lowest-ranked ready id. */
export function topologicalOrder(ids: readonly ToolchainId[], depsOf: (id: ToolchainId) => readonly ToolchainId[]): ToolchainId[] {
  const pending = new Map(ids.map((id) => [id, new Set(depsOf(id))]));
  const out: ToolchainId[] = [];
  while (pending.size > 0) {
    const ready = [...pending.entries()].filter(([, deps]) => deps.size === 0).map(([id]) => id);
    if (ready.length === 0) throw new PlanCycleError([...pending.keys()]);
    ready.sort((a, b) => rank(a) - rank(b));
    const next = ready[0] as ToolchainId;
    out.push(next);
    pending.delete(next);
    for (const deps of pending.values()) deps.delete(next);
  }
  return out;
}
