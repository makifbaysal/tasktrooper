import { Background, Controls, MiniMap, ReactFlow, type Edge as FlowEdge, type Node as FlowNode } from "@xyflow/react";
import { Network } from "lucide-react";
import { useCallback, useMemo, type MouseEvent as ReactMouseEvent } from "react";
import { useNavigate } from "react-router-dom";
import type { WorkspaceMap } from "@/api";
import { ProjectGroupNode } from "@/components/projects/map/ProjectGroupNode";
import { WorkspaceRepoNode } from "@/components/projects/map/WorkspaceRepoNode";
import { WorkspaceResourceNode } from "@/components/projects/map/WorkspaceResourceNode";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import { buildWorkspaceMapGraph } from "@/lib/workspaceMapLayout";
import { cn } from "@/lib/utils";

const NODE_TYPES = {
  projectGroup: ProjectGroupNode,
  workspaceRepo: WorkspaceRepoNode,
  workspaceResource: WorkspaceResourceNode,
};

interface WorkspaceMapViewProps {
  map: WorkspaceMap;
  className?: string;
}

/**
 * The "Tüm projeler haritası" screen: every project as a box holding its
 * repositories, cross-project edges only where `edges` actually has them,
 * shared resources as small nodes connecting the projects that use them,
 * and independent projects standing alone in their own grid. Clicking a
 * project opens its Architecture tab.
 */
export function WorkspaceMapView({ map, className }: WorkspaceMapViewProps) {
  const { t } = useI18n();
  const navigate = useNavigate();

  const { nodes, edges } = useMemo(() => buildWorkspaceMapGraph(map), [map]);

  const onNodeClick = useCallback(
    (_event: ReactMouseEvent, node: FlowNode) => {
      if (!node.id.startsWith("p:")) return;
      navigate(`/projects/${node.id.slice(2)}?tab=architecture`);
    },
    [navigate],
  );

  if (map.projects.length === 0) {
    return (
      <EmptyState
        icon={Network}
        title={t("projectsHub.empty.title")}
        description={t("projectsHub.empty.description")}
        className={cn("py-16", className)}
      />
    );
  }

  return (
    <div
      className={cn("relative h-[640px] w-full overflow-hidden rounded-xl border border-border", className)}
      role="img"
      aria-label={t("projectsHub.map.canvasLabel")}
    >
      <ReactFlow
        nodes={nodes as FlowNode[]}
        edges={edges as FlowEdge[]}
        nodeTypes={NODE_TYPES}
        onNodeClick={onNodeClick}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesReconnectable={false}
        elementsSelectable
        fitView
        fitViewOptions={{ padding: 0.15 }}
        minZoom={0.2}
      >
        <Background gap={24} />
        <Controls showInteractive={false} />
        <MiniMap pannable zoomable />
      </ReactFlow>
    </div>
  );
}
