import type { Node, NodeProps } from "@xyflow/react";
import type { WorkspaceMapRepository } from "@/api";
import { RepoShapeBadge } from "@/components/projects/model/RepoShapeBadge";
import { RoleBadge } from "@/components/projects/model/RoleBadge";

export interface WorkspaceRepoNodeData extends Record<string, unknown> {
  repository: WorkspaceMapRepository;
}

/** One repository row inside a `ProjectGroupNode`: its name, shape, and one
 * role chip per component. */
export function WorkspaceRepoNode({ data }: NodeProps<Node<WorkspaceRepoNodeData, "workspaceRepo">>) {
  const { repository } = data;
  return (
    <div className="flex h-full w-full flex-col justify-center gap-1 rounded-lg border border-border bg-card px-2.5 py-1.5">
      <div className="flex items-center gap-1.5">
        <span className="truncate text-caption font-medium">{repository.name}</span>
        <RepoShapeBadge shape={repository.shape} />
      </div>
      {repository.components.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {repository.components.map((c) => (
            <RoleBadge key={c.id} role={c.role} className="px-1.5 py-0 text-micro" />
          ))}
        </div>
      )}
    </div>
  );
}
