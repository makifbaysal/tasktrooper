import { X } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type MapEdge, type MapNode, type ProjectRef } from "@/api";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { ResourceKindIcon } from "@/components/projects/model/ResourceKindIcon";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export type MapSelection = { type: "node"; id: string } | { type: "edge"; id: string };

interface MapSidePanelProps {
  currentProjectId: string;
  crossProjects: ProjectRef[];
  edges: MapEdge[];
  nodesById: Map<string, MapNode>;
  selection: MapSelection;
  onSelectEdge: (edgeId: string) => void;
  onSelectNode: (nodeId: string) => void;
  onChanged: () => void;
  onClose: () => void;
  className?: string;
}

/** Detail for the architecture map's current selection: a node's identity,
 * outgoing/incoming edges (each reselects that edge), and — for a foreign
 * node — links out to the repository/project it actually lives in; or, for
 * an edge, its two ends plus Confirm/Dismiss when it is still a suggestion. */
export function MapSidePanel({
  currentProjectId,
  crossProjects,
  edges,
  nodesById,
  selection,
  onSelectEdge,
  onSelectNode,
  onChanged,
  onClose,
  className,
}: MapSidePanelProps) {
  if (selection.type === "edge") {
    const edge = edges.find((e) => e.id === selection.id);
    if (!edge) return null;
    return (
      <EdgeDetail edge={edge} nodesById={nodesById} onChanged={onChanged} onClose={onClose} onSelectNode={onSelectNode} className={className} />
    );
  }

  const node = nodesById.get(selection.id);
  if (!node) return null;
  return (
    <NodeDetail
      node={node}
      currentProjectId={currentProjectId}
      crossProjects={crossProjects}
      edges={edges}
      nodesById={nodesById}
      onSelectEdge={onSelectEdge}
      onClose={onClose}
      className={className}
    />
  );
}

function CloseButton({ onClose }: { onClose: () => void }) {
  const { t } = useI18n();
  return (
    <Button variant="ghost" size="icon" className="h-6 w-6 shrink-0" onClick={onClose} aria-label={t("common.cancel")}>
      <X className="h-3.5 w-3.5" />
    </Button>
  );
}

interface NodeDetailProps {
  node: MapNode;
  currentProjectId: string;
  crossProjects: ProjectRef[];
  edges: MapEdge[];
  nodesById: Map<string, MapNode>;
  onSelectEdge: (edgeId: string) => void;
  onClose: () => void;
  className?: string;
}

function NodeDetail({ node, currentProjectId, crossProjects, edges, nodesById, onSelectEdge, onClose, className }: NodeDetailProps) {
  const { t } = useI18n();
  const outgoing = edges.filter((e) => e.from === node.id);
  const incoming = edges.filter((e) => e.to === node.id);
  const otherProjectId = node.project_ids?.find((id) => id !== currentProjectId);
  const otherProject = otherProjectId ? crossProjects.find((p) => p.id === otherProjectId) : undefined;

  return (
    <Card data-testid="map-side-panel" className={cn("flex flex-col gap-3 overflow-y-auto p-4", className)}>
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          {node.kind === "component" ? (
            node.role && <RoleBadge role={node.role} />
          ) : (
            <ResourceKindIcon kind={node.resource_kind ?? "other"} className="h-4 w-4 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate font-semibold">{node.label}</span>
        </div>
        <CloseButton onClose={onClose} />
      </div>

      <dl className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-1 text-caption">
        {node.kind === "component" && (
          <>
            <dt className="text-muted-foreground">{t("projectModel.map.panel.repository")}</dt>
            <dd className="truncate">
              {node.repository_name}
              {node.path && node.path !== "." ? `/${node.path}` : ""}
            </dd>
          </>
        )}
        {node.kind === "resource" && node.vendor && (
          <>
            <dt className="text-muted-foreground">{t("projectModel.map.panel.vendor")}</dt>
            <dd className="truncate">{node.vendor}</dd>
          </>
        )}
        {node.stack_summary && (
          <>
            <dt className="text-muted-foreground">{t("projectModel.map.panel.stack")}</dt>
            <dd className="truncate">{node.stack_summary}</dd>
          </>
        )}
        {node.provider && (
          <>
            <dt className="text-muted-foreground">{t("projectModel.map.panel.provider")}</dt>
            <dd className="flex items-center gap-1.5">
              <ProviderIcon provider={node.provider} className="h-3.5 w-3.5" />
              {t(`cloud.providers.${node.provider}`)}
            </dd>
          </>
        )}
        {node.health && (
          <>
            <dt className="text-muted-foreground">{t("projectModel.map.panel.health")}</dt>
            <dd>
              {t(`cloud.health.${node.health}`)}
              {(node.error_count_24h ?? 0) > 0 ? ` · ${t("cloud.errorCount", { count: node.error_count_24h ?? 0 })}` : ""}
            </dd>
          </>
        )}
      </dl>

      {node.kind === "resource" && (node.shared_with?.length ?? 0) > 0 && (
        <div className="flex flex-col gap-1">
          <span className="text-micro font-medium uppercase tracking-wide text-muted-foreground">
            {t("projectModel.map.panel.sharedWithTitle")}
          </span>
          <div className="flex flex-wrap gap-1.5">
            {node.shared_with!.map((project) => (
              <Link key={project.id} to={`/projects/${project.id}`}>
                <Badge variant="outline">{project.name}</Badge>
              </Link>
            ))}
          </div>
        </div>
      )}

      <EdgeGroup
        title={t("projectModel.map.panel.outgoing", { count: outgoing.length })}
        edges={outgoing}
        nodesById={nodesById}
        direction="out"
        onSelectEdge={onSelectEdge}
      />
      <EdgeGroup
        title={t("projectModel.map.panel.incoming", { count: incoming.length })}
        edges={incoming}
        nodesById={nodesById}
        direction="in"
        onSelectEdge={onSelectEdge}
      />

      {(node.kind === "component" && node.repository_id) || (node.foreign && otherProject) ? (
        <div className="mt-auto flex flex-col gap-1.5 pt-2">
          {node.kind === "component" && node.repository_id && (
            <Button size="sm" variant="outline" asChild>
              <Link to={node.foreign ? `/repositories/${node.repository_id}` : `/repositories/${node.repository_id}?project=${currentProjectId}`}>
                {t("projectModel.map.panel.openRepository")}
              </Link>
            </Button>
          )}
          {node.foreign && otherProject && (
            <Button size="sm" variant="outline" asChild>
              <Link to={`/projects/${otherProject.id}`}>{t("projectModel.map.panel.openProject")}</Link>
            </Button>
          )}
        </div>
      ) : null}
    </Card>
  );
}

interface EdgeGroupProps {
  title: string;
  edges: MapEdge[];
  nodesById: Map<string, MapNode>;
  direction: "out" | "in";
  onSelectEdge: (edgeId: string) => void;
}

function EdgeGroup({ title, edges, nodesById, direction, onSelectEdge }: EdgeGroupProps) {
  const { t } = useI18n();
  if (edges.length === 0) return null;
  return (
    <div className="flex flex-col gap-1">
      <span className="text-micro font-medium uppercase tracking-wide text-muted-foreground">{title}</span>
      <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
        {edges.map((edge) => {
          const other = nodesById.get(direction === "out" ? edge.to : edge.from);
          return (
            <li key={edge.id}>
              <button
                type="button"
                onClick={() => onSelectEdge(edge.id)}
                className="flex w-full items-center justify-between gap-2 px-2 py-1.5 text-left text-caption hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <span className="truncate">
                  {direction === "out" ? "→ " : "← "}
                  {other?.label ?? t("projectModel.linkTargetUnknown")}
                </span>
                {edge.status === "suggested" ? (
                  <Badge variant="warning" className="shrink-0 px-1.5 py-0 text-micro">
                    {t("projectModel.linkStatuses.suggested")}
                  </Badge>
                ) : (
                  <span className="shrink-0 text-micro text-muted-foreground">{t(`projectModel.linkProtocols.${edge.protocol}`)}</span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

interface EdgeDetailProps {
  edge: MapEdge;
  nodesById: Map<string, MapNode>;
  onSelectNode: (nodeId: string) => void;
  onChanged: () => void;
  onClose: () => void;
  className?: string;
}

function EdgeDetail({ edge, nodesById, onSelectNode, onChanged, onClose, className }: EdgeDetailProps) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const from = nodesById.get(edge.from);
  const to = nodesById.get(edge.to);
  const suggested = edge.status === "suggested";

  const run = async (status: "confirmed" | "dismissed") => {
    setBusy(true);
    try {
      await api.updateLink(edge.link_id, { status });
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectModel.review.failed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card data-testid="map-side-panel" className={cn("flex flex-col gap-3 p-4", className)}>
      <div className="flex items-start justify-between gap-2">
        <span className="font-semibold">{t(`projectModel.linkProtocols.${edge.protocol}`)}</span>
        <CloseButton onClose={onClose} />
      </div>

      <div className="flex flex-col gap-1 text-caption">
        <button type="button" className="truncate text-left hover:underline" onClick={() => from && onSelectNode(from.id)}>
          {from?.label ?? t("projectModel.linkTargetUnknown")}
        </button>
        <span className="pl-2 text-micro text-muted-foreground">↓ {edge.detail || t(`projectModel.linkProtocols.${edge.protocol}`)}</span>
        <button type="button" className="truncate text-left hover:underline" onClick={() => to && onSelectNode(to.id)}>
          {to?.label ?? t("projectModel.linkTargetUnknown")}
        </button>
      </div>

      <div className="flex flex-wrap gap-1.5">
        {edge.cross_project && <Badge variant="info">{t("projectModel.map.panel.crossProject")}</Badge>}
        {!suggested && <Badge variant="success">{t("projectModel.linkStatuses.confirmed")}</Badge>}
      </div>

      {suggested && (
        <div className="flex gap-1.5">
          <Button size="sm" disabled={busy} onClick={() => void run("confirmed")}>
            {t("projectModel.review.confirmLink")}
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => void run("dismissed")}>
            {t("projectModel.review.dismiss")}
          </Button>
        </div>
      )}
    </Card>
  );
}
