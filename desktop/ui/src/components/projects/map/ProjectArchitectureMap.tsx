import { Background, Controls, MiniMap, ReactFlow, type Edge as FlowEdge, type Node as FlowNode } from "@xyflow/react";
import { Boxes } from "lucide-react";
import { useCallback, useMemo, useState, type MouseEvent as ReactMouseEvent } from "react";
import type { ProjectMap, ProjectRef } from "@/api";
import { ComponentMapNode } from "@/components/projects/map/ComponentMapNode";
import { MapEdgeLine } from "@/components/projects/map/MapEdgeLine";
import { MapFilters } from "@/components/projects/map/MapFilters";
import { MapSidePanel, type MapSelection } from "@/components/projects/map/MapSidePanel";
import { ResourceMapNode } from "@/components/projects/map/ResourceMapNode";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import { layoutMapNodes, toFlowEdges, toFlowNodes, type MapEdgeData, type MapNodeData } from "@/lib/architectureMapLayout";
import { cn } from "@/lib/utils";

// Stable references: React Flow remounts every node/edge of that type if
// these objects change identity between renders.
const NODE_TYPES = { component: ComponentMapNode, resource: ResourceMapNode };
const EDGE_TYPES = { map: MapEdgeLine };

interface ProjectArchitectureMapProps {
  map: ProjectMap;
  currentProjectId: string;
  /** For resolving a foreign node's own project name/link — ProjectDetail's
   * own cross_projects, already loaded by the page. */
  crossProjects: ProjectRef[];
  onChanged: () => void;
  className?: string;
}

/**
 * The project "Mimari" tab: a deterministic tiered React Flow canvas (client
 * → service → library → data → external) of this project's components and
 * the resources/foreign components they reach. Selecting a node highlights
 * its edges and opens a side panel; a suggested edge's panel can Confirm or
 * Dismiss it in place.
 */
export function ProjectArchitectureMap({ map, currentProjectId, crossProjects, onChanged, className }: ProjectArchitectureMapProps) {
  const { t } = useI18n();
  const [showLibraries, setShowLibraries] = useState(false);
  const [showSuggestions, setShowSuggestions] = useState(true);
  const [showForeign, setShowForeign] = useState(true);
  const [selection, setSelection] = useState<MapSelection | null>(null);

  const nodesById = useMemo(() => new Map(map.nodes.map((n) => [n.id, n])), [map.nodes]);
  const hasOwnComponents = useMemo(() => map.nodes.some((n) => n.kind === "component" && !n.foreign), [map.nodes]);

  const filteredNodes = useMemo(
    () => map.nodes.filter((n) => (n.tier !== "library" || showLibraries) && (!n.foreign || showForeign)),
    [map.nodes, showLibraries, showForeign],
  );
  const visibleIds = useMemo(() => new Set(filteredNodes.map((n) => n.id)), [filteredNodes]);
  const filteredEdges = useMemo(
    () =>
      map.edges.filter(
        (e) => visibleIds.has(e.from) && visibleIds.has(e.to) && (e.status !== "suggested" || showSuggestions),
      ),
    [map.edges, visibleIds, showSuggestions],
  );

  const positioned = useMemo(() => layoutMapNodes(filteredNodes, { showLibraries }), [filteredNodes, showLibraries]);
  const baseFlowNodes = useMemo(() => toFlowNodes(positioned), [positioned]);
  const baseFlowEdges = useMemo(() => toFlowEdges(filteredEdges), [filteredEdges]);

  const connectedIds = useMemo(() => {
    if (selection?.type !== "node") return null;
    const ids = new Set<string>([selection.id]);
    for (const edge of filteredEdges) {
      if (edge.from === selection.id) ids.add(edge.to);
      if (edge.to === selection.id) ids.add(edge.from);
    }
    return ids;
  }, [selection, filteredEdges]);

  const flowNodes = useMemo<FlowNode<MapNodeData>[]>(
    () =>
      baseFlowNodes.map((n) => ({
        ...n,
        selected: selection?.type === "node" && selection.id === n.id,
        data: { ...n.data, dimmed: connectedIds ? !connectedIds.has(n.id) : false },
      })),
    [baseFlowNodes, selection, connectedIds],
  );

  const flowEdges = useMemo<FlowEdge<MapEdgeData>[]>(
    () =>
      baseFlowEdges.map((e, i) => {
        const isSelectedEdge = selection?.type === "edge" && selection.id === e.id;
        const touchesSelectedNode = selection?.type === "node" && (e.source === selection.id || e.target === selection.id);
        return {
          ...e,
          selected: isSelectedEdge,
          data: {
            edge: filteredEdges[i],
            highlighted: isSelectedEdge || touchesSelectedNode,
            dimmed: selection?.type === "node" ? !touchesSelectedNode : false,
          },
        };
      }),
    [baseFlowEdges, filteredEdges, selection],
  );

  const onNodeClick = useCallback((_event: ReactMouseEvent, node: FlowNode<MapNodeData>) => {
    setSelection({ type: "node", id: node.id });
  }, []);
  const onEdgeClick = useCallback((_event: ReactMouseEvent, edge: FlowEdge<MapEdgeData>) => {
    setSelection({ type: "edge", id: edge.id });
  }, []);
  const onPaneClick = useCallback(() => setSelection(null), []);
  const onSelectNode = useCallback((id: string) => setSelection({ type: "node", id }), []);
  const onSelectEdge = useCallback((id: string) => setSelection({ type: "edge", id }), []);
  const onClosePanel = useCallback(() => setSelection(null), []);

  if (!hasOwnComponents) {
    return (
      <EmptyState
        icon={Boxes}
        title={t("projectModel.map.empty.title")}
        description={t("projectModel.map.empty.description")}
        className={cn("py-16", className)}
      />
    );
  }

  return (
    <div className={cn("flex flex-col gap-3", className)}>
      <MapFilters
        showLibraries={showLibraries}
        onShowLibrariesChange={setShowLibraries}
        showSuggestions={showSuggestions}
        onShowSuggestionsChange={setShowSuggestions}
        showForeign={showForeign}
        onShowForeignChange={setShowForeign}
      />
      <div className="flex gap-3">
        <div className="relative h-[600px] flex-1 overflow-hidden rounded-xl border border-border" role="img" aria-label={t("projectModel.map.canvasLabel", { project: map.project.name })}>
          {/* Remounting on a filter change is what makes fitView refit the
              now-different node set — React Flow only fits once, on mount. */}
          <ReactFlow
            key={`${showLibraries}-${showForeign}`}
            nodes={flowNodes}
            edges={flowEdges}
            nodeTypes={NODE_TYPES}
            edgeTypes={EDGE_TYPES}
            onNodeClick={onNodeClick}
            onEdgeClick={onEdgeClick}
            onPaneClick={onPaneClick}
            nodesDraggable={false}
            nodesConnectable={false}
            edgesReconnectable={false}
            elementsSelectable
            fitView
            fitViewOptions={{ padding: 0.2 }}
            minZoom={0.25}
          >
            <Background gap={24} />
            <Controls showInteractive={false} />
            <MiniMap pannable zoomable />
          </ReactFlow>
        </div>
        {selection && (
          <MapSidePanel
            currentProjectId={currentProjectId}
            crossProjects={crossProjects}
            edges={map.edges}
            nodesById={nodesById}
            selection={selection}
            onSelectEdge={onSelectEdge}
            onSelectNode={onSelectNode}
            onChanged={onChanged}
            onClose={onClosePanel}
            className="h-[600px] w-80 shrink-0"
          />
        )}
      </div>
    </div>
  );
}
