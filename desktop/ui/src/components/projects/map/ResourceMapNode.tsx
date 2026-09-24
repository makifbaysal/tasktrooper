import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import { ResourceKindIcon } from "@/components/projects/model/ResourceKindIcon";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { MAP_NODE_WIDTH, type MapNodeData } from "@/lib/architectureMapLayout";
import { cn } from "@/lib/utils";

/** One system resource on the architecture map — a database, queue, or
 * external API — with a "shared with N projects" chip when other projects
 * link the same node (SystemResource.IdentityKey dedupes it server-side). */
export function ResourceMapNode({ data, selected }: NodeProps<Node<MapNodeData, "resource">>) {
  const { t } = useI18n();
  const { node, dimmed } = data;
  const sharedCount = node.shared_with?.length ?? 0;

  return (
    <div
      className={cn(
        "flex items-center gap-2 rounded-lg border border-border bg-card px-2.5 py-2 text-card-foreground transition-opacity",
        selected && "ring-2 ring-ring",
        dimmed && "opacity-30",
      )}
      style={{ width: MAP_NODE_WIDTH }}
    >
      <Handle type="target" position={Position.Left} style={{ visibility: "hidden" }} />
      <ResourceKindIcon kind={node.resource_kind ?? "other"} className="h-4 w-4 shrink-0 text-muted-foreground" />
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-caption font-semibold" title={node.label}>
          {node.label}
        </span>
        {node.vendor && <span className="truncate text-micro text-muted-foreground">{node.vendor}</span>}
      </div>
      {sharedCount > 0 && (
        <Badge variant="secondary" className="shrink-0 px-1.5 py-0 text-micro">
          {t("projectModel.map.sharedWith", { count: sharedCount })}
        </Badge>
      )}
      <Handle type="source" position={Position.Right} style={{ visibility: "hidden" }} />
    </div>
  );
}
