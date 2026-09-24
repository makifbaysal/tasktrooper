import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import { AlertCircle } from "lucide-react";
import type { ComponentRole } from "@/api";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { useI18n } from "@/hooks/useI18n";
import { MAP_NODE_WIDTH, type MapNodeData } from "@/lib/architectureMapLayout";
import { cn } from "@/lib/utils";

// Solid-token counterparts of RoleBadge's tinted badge colors, for a 3px
// identity stripe rather than a pill background.
const ROLE_STRIPE_CLASS: Record<ComponentRole, string> = {
  frontend: "bg-info",
  backend: "bg-primary",
  mobile: "bg-success",
  desktop: "bg-accent-warm",
  worker: "bg-warning",
  library: "bg-secondary-foreground",
  infra: "bg-accent-foreground",
  cli: "bg-muted-foreground",
  other: "bg-destructive",
};

const HEALTH_DOT_CLASS: Record<string, string> = {
  healthy: "bg-success",
  deploying: "bg-info",
  degraded: "bg-warning",
  failed: "bg-destructive",
  unknown: "bg-muted-foreground",
};

/** One component on the architecture map: a role-colored stripe, its label,
 * repo/path caption, and — only when a production environment is bound —
 * a provider mark, health dot and error badge. A foreign node (another
 * project's component, drawn only because a link reaches it) gets a dashed
 * border instead of the role stripe's solid identity. */
export function ComponentMapNode({ data, selected }: NodeProps<Node<MapNodeData, "component">>) {
  const { t } = useI18n();
  const { node, dimmed } = data;

  return (
    <div
      className={cn(
        "flex items-stretch overflow-hidden rounded-lg border bg-card text-card-foreground transition-opacity",
        node.foreign ? "border-dashed border-muted-foreground bg-card/70" : "border-border",
        selected && "ring-2 ring-ring",
        dimmed && "opacity-30",
      )}
      style={{ width: MAP_NODE_WIDTH }}
    >
      <Handle type="target" position={Position.Left} style={{ visibility: "hidden" }} />
      {!node.foreign && (
        <span className={cn("w-1 shrink-0", node.role ? ROLE_STRIPE_CLASS[node.role] : "bg-muted")} aria-hidden />
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-0.5 px-2.5 py-2 text-left">
        <span className="truncate text-caption font-semibold" title={node.label}>
          {node.label}
        </span>
        <span className="truncate text-micro text-muted-foreground">
          {node.repository_name}
          {node.path && node.path !== "." ? `/${node.path}` : ""}
        </span>
        {node.foreign && <span className="text-micro text-muted-foreground">{t("projectModel.map.foreign")}</span>}
        {!node.foreign && node.stack_summary && (
          <span className="truncate text-micro text-muted-foreground">{node.stack_summary}</span>
        )}
        {(node.provider || node.health || (node.error_count_24h ?? 0) > 0) && (
          <div className="mt-0.5 flex items-center gap-1.5">
            {node.provider && <ProviderIcon provider={node.provider} className="h-3 w-3 text-muted-foreground" />}
            {node.health && (
              <span
                className={cn("h-1.5 w-1.5 shrink-0 rounded-full", HEALTH_DOT_CLASS[node.health])}
                title={t(`cloud.health.${node.health}`)}
                aria-hidden
              />
            )}
            {(node.error_count_24h ?? 0) > 0 && (
              <span className="inline-flex items-center gap-0.5 text-micro text-destructive">
                <AlertCircle className="h-3 w-3" aria-hidden />
                {node.error_count_24h}
              </span>
            )}
          </div>
        )}
      </div>
      <Handle type="source" position={Position.Right} style={{ visibility: "hidden" }} />
    </div>
  );
}
