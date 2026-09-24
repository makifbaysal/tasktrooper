import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import type { WorkspaceMapProject } from "@/api";
import { ProjectTypeBadge } from "@/components/projects/model/ProjectTypeBadge";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export interface ProjectGroupNodeData extends Record<string, unknown> {
  project: WorkspaceMapProject;
  independent: boolean;
}

/**
 * The workspace map's project box: a header (name, type, repo/component
 * counts) with its repositories rendered as child `WorkspaceRepoNode`s
 * inside it. An independent project (no cross-project edge, no shared
 * resource) gets a caption instead of ending abruptly.
 */
export function ProjectGroupNode({ data, selected }: NodeProps<Node<ProjectGroupNodeData, "projectGroup">>) {
  const { t } = useI18n();
  const { project, independent } = data;
  const componentCount = project.repositories.reduce((sum, r) => sum + r.components.length, 0);

  return (
    <div
      className={cn(
        "flex h-full w-full flex-col rounded-xl border-2 border-border bg-muted/30",
        selected && "ring-2 ring-ring",
      )}
    >
      <Handle type="target" position={Position.Left} style={{ visibility: "hidden" }} />
      <Handle type="source" position={Position.Right} style={{ visibility: "hidden" }} />
      <div className="flex items-start justify-between gap-2 border-b border-border px-3 py-2">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="truncate font-semibold">{project.name}</span>
            <ProjectTypeBadge type={project.type} />
          </div>
          <span className="text-micro text-muted-foreground">
            {t("projectsHub.card.counts", { repos: project.repositories.length, components: componentCount })}
          </span>
        </div>
        {independent && (
          <Badge variant="outline" className="shrink-0 text-micro">
            {t("projectsHub.map.independent")}
          </Badge>
        )}
      </div>
    </div>
  );
}
