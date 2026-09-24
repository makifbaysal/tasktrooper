import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import type { WorkspaceSharedResource } from "@/api";
import { ResourceKindIcon } from "@/components/projects/model/ResourceKindIcon";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";

export interface WorkspaceResourceNodeData extends Record<string, unknown> {
  shared: WorkspaceSharedResource;
}

/** A resource shared by two or more projects, drawn once and connected to
 * each project that links it — the workspace map's only resource nodes,
 * since a resource used by a single project is that project's own concern. */
export function WorkspaceResourceNode({ data }: NodeProps<Node<WorkspaceResourceNodeData, "workspaceResource">>) {
  const { t } = useI18n();
  const { shared } = data;
  return (
    <div className="flex w-full items-center gap-2 rounded-lg border border-dashed border-muted-foreground bg-card px-2.5 py-2">
      <Handle type="source" position={Position.Top} style={{ visibility: "hidden" }} />
      <ResourceKindIcon kind={shared.resource.kind} className="h-4 w-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1 truncate text-caption font-medium">{shared.resource.name}</span>
      <Badge variant="secondary" className="shrink-0 px-1.5 py-0 text-micro">
        {t("projectModel.map.sharedWith", { count: shared.project_ids.length })}
      </Badge>
    </div>
  );
}
