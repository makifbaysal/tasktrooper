import type { Edge, Node } from "@xyflow/react";
import type { MapEdge, MapNode, MapTier } from "@/api";

// Client → service → library → data → external, left to right — mirrors the
// server's TierForRole/TierForResource ordering (project_map.go). Library is
// spliced out of the visible set (not just hidden) so the columns to its
// right shift left, rather than leaving a blank column, when the toggle is off.
export const TIER_ORDER: MapTier[] = ["client", "service", "library", "data", "external"];

export const MAP_COLUMN_WIDTH = 260;
export const MAP_NODE_WIDTH = 220;
export const MAP_ROW_HEIGHT = 96;
export const MAP_TOP_PADDING = 16;
// A same-enough starting guess for a node's rendered height, before its
// ResizeObserver entry corrects it — real browsers self-correct on the next
// frame; jsdom (tests) never fires one at all, so edges need this to compute
// a path from the very first render.
export const MAP_NODE_INITIAL_HEIGHT = 68;

export function visibleTiers(showLibraries: boolean): MapTier[] {
  return showLibraries ? TIER_ORDER : TIER_ORDER.filter((tier) => tier !== "library");
}

/** Component nodes group by repository then path; resource nodes by kind,
 * vendor and name — so the same repo's/resource's rows land next to each
 * other inside a tier's column. */
function nodeGroupKey(node: MapNode): string {
  if (node.kind === "component") {
    return `${node.repository_name ?? ""}\u0000${node.path ?? ""}\u0000${node.label}`;
  }
  return `${node.resource_kind ?? ""}\u0000${node.vendor ?? ""}\u0000${node.label}`;
}

export interface PositionedMapNode {
  node: MapNode;
  tier: MapTier;
  column: number;
  row: number;
  x: number;
  y: number;
}

export interface MapLayoutOptions {
  /** Include the "library" tier as its own column. Default false. */
  showLibraries?: boolean;
}

/**
 * Deterministic tiered layout: x is the node's tier's column among the
 * currently visible tiers, y stacks nodes within a tier's column, grouped so
 * one repository's (or one resource's) rows are adjacent. Nodes whose tier is
 * not currently visible (library, when the toggle is off) are dropped.
 */
export function layoutMapNodes(nodes: MapNode[], options: MapLayoutOptions = {}): PositionedMapNode[] {
  const tiers = visibleTiers(options.showLibraries ?? false);
  const columnOf = new Map<MapTier, number>(tiers.map((tier, index) => [tier, index]));

  const byTier = new Map<MapTier, MapNode[]>(tiers.map((tier) => [tier, []]));
  for (const node of nodes) {
    byTier.get(node.tier)?.push(node);
  }

  const positioned: PositionedMapNode[] = [];
  for (const tier of tiers) {
    const group = [...(byTier.get(tier) ?? [])].sort((a, b) => nodeGroupKey(a).localeCompare(nodeGroupKey(b)));
    const column = columnOf.get(tier)!;
    group.forEach((node, row) => {
      positioned.push({
        node,
        tier,
        column,
        row,
        x: column * MAP_COLUMN_WIDTH,
        // Odd columns sit half a row lower so an edge that skips a column
        // (service → external) passes between that column's nodes, not
        // behind one, where it would read as starting there.
        y: MAP_TOP_PADDING + row * MAP_ROW_HEIGHT + (column % 2 === 1 ? MAP_ROW_HEIGHT / 2 : 0),
      });
    });
  }
  return positioned;
}

export interface MapNodeData extends Record<string, unknown> {
  node: MapNode;
  /** Set by the organism, not this module, once a selection fades out
   * everything not connected to it. */
  dimmed?: boolean;
}

export interface MapEdgeData extends Record<string, unknown> {
  edge: MapEdge;
  dimmed?: boolean;
  highlighted?: boolean;
}

/** React Flow nodes for one positioned layout — not draggable/connectable,
 * this map is read-only with a fixed layout. */
export function toFlowNodes(positioned: PositionedMapNode[]): Node<MapNodeData>[] {
  return positioned.map(({ node, x, y }) => ({
    id: node.id,
    type: node.kind,
    position: { x, y },
    data: { node },
    draggable: false,
    connectable: false,
    focusable: true,
    initialWidth: MAP_NODE_WIDTH,
    initialHeight: MAP_NODE_INITIAL_HEIGHT,
  }));
}

export function toFlowEdges(edges: MapEdge[]): Edge<MapEdgeData>[] {
  return edges.map((edge) => ({
    id: edge.id,
    source: edge.from,
    target: edge.to,
    type: "map",
    data: { edge },
    focusable: true,
  }));
}
