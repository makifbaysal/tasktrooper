import type { Edge, Node } from "@xyflow/react";
import type { WorkspaceMap, WorkspaceMapProject } from "@/api";
import type { ProjectGroupNodeData } from "@/components/projects/map/ProjectGroupNode";
import type { WorkspaceRepoNodeData } from "@/components/projects/map/WorkspaceRepoNode";
import type { WorkspaceResourceNodeData } from "@/components/projects/map/WorkspaceResourceNode";

export const WORKSPACE_PROJECT_WIDTH = 420;
export const WORKSPACE_PROJECT_GAP_X = 60;
export const WORKSPACE_REPO_ROW_HEIGHT = 60;
export const WORKSPACE_HEADER_HEIGHT = 68;
export const WORKSPACE_PROJECT_PADDING = 10;
export const WORKSPACE_INDEPENDENT_COLUMNS = 3;
export const WORKSPACE_INDEPENDENT_ROW_HEIGHT = 280;
export const WORKSPACE_RESOURCE_WIDTH = 200;

function projectHeight(project: WorkspaceMapProject): number {
  return WORKSPACE_HEADER_HEIGHT + Math.max(project.repositories.length, 1) * WORKSPACE_REPO_ROW_HEIGHT + WORKSPACE_PROJECT_PADDING;
}

/** A project is "linked" the moment it has a cross-project edge or shares a
 * resource with another project; everything else is independent. Mirrors
 * ProjectOverview.cross_projects/shared_resources being empty for an
 * independent project server-side. */
export function isProjectLinked(projectId: string, map: Pick<WorkspaceMap, "edges" | "shared_resources">): boolean {
  return (
    map.edges.some((e) => e.from_project_id === projectId || e.to_project_id === projectId) ||
    map.shared_resources.some((r) => r.project_ids.includes(projectId))
  );
}

function pushProjectNodes(nodes: Node[], project: WorkspaceMapProject, x: number, y: number, independent: boolean): void {
  const height = projectHeight(project);
  nodes.push({
    id: `p:${project.id}`,
    type: "projectGroup",
    position: { x, y },
    style: { width: WORKSPACE_PROJECT_WIDTH, height },
    // Every dimension here is already computed, not guessed — set as
    // "initial" too so an edge to this node has something to anchor to
    // before its own ResizeObserver entry (never fired at all in jsdom
    // tests) would otherwise correct it.
    initialWidth: WORKSPACE_PROJECT_WIDTH,
    initialHeight: height,
    data: { project, independent } satisfies ProjectGroupNodeData,
    draggable: false,
    connectable: false,
  });
  // Every child must follow its parent in the array — React Flow resolves
  // parentId against nodes already seen.
  project.repositories.forEach((repository, i) => {
    const repoHeight = WORKSPACE_REPO_ROW_HEIGHT - 8;
    nodes.push({
      id: `r:${project.id}:${repository.id}`,
      type: "workspaceRepo",
      parentId: `p:${project.id}`,
      extent: "parent",
      position: { x: WORKSPACE_PROJECT_PADDING, y: WORKSPACE_HEADER_HEIGHT + i * WORKSPACE_REPO_ROW_HEIGHT },
      style: { width: WORKSPACE_PROJECT_WIDTH - WORKSPACE_PROJECT_PADDING * 2, height: repoHeight },
      initialWidth: WORKSPACE_PROJECT_WIDTH - WORKSPACE_PROJECT_PADDING * 2,
      initialHeight: repoHeight,
      data: { repository } satisfies WorkspaceRepoNodeData,
      draggable: false,
      connectable: false,
      selectable: false,
    });
  });
}

export interface WorkspaceMapGraph {
  nodes: Node[];
  edges: Edge[];
}

/**
 * Deterministic layout for the "Tüm projeler haritası" screen: linked
 * projects in a row with their cross-project edges and shared-resource
 * nodes, independent projects in their own grid below with no edges at all.
 */
export function buildWorkspaceMapGraph(map: WorkspaceMap): WorkspaceMapGraph {
  const nodes: Node[] = [];
  const edges: Edge[] = [];

  const linked = map.projects.filter((p) => isProjectLinked(p.id, map));
  const independent = map.projects.filter((p) => !isProjectLinked(p.id, map));

  const linkedIds = new Set(linked.map((p) => p.id));
  let x = 0;
  for (const project of linked) {
    pushProjectNodes(nodes, project, x, 0, false);
    x += WORKSPACE_PROJECT_WIDTH + WORKSPACE_PROJECT_GAP_X;
  }

  const linkedMaxHeight = linked.reduce((max, p) => Math.max(max, projectHeight(p)), 0);
  const resourceY = linkedMaxHeight + 100;

  let rx = 0;
  for (const shared of map.shared_resources) {
    const resourceNodeId = `res:${shared.resource.id}`;
    nodes.push({
      id: resourceNodeId,
      type: "workspaceResource",
      position: { x: rx, y: resourceY },
      style: { width: WORKSPACE_RESOURCE_WIDTH },
      initialWidth: WORKSPACE_RESOURCE_WIDTH,
      initialHeight: 44,
      data: { shared } satisfies WorkspaceResourceNodeData,
      draggable: false,
      connectable: false,
    });
    rx += WORKSPACE_RESOURCE_WIDTH + 40;
    for (const projectId of shared.project_ids) {
      if (!linkedIds.has(projectId)) continue;
      edges.push({
        id: `res-edge:${shared.resource.id}:${projectId}`,
        source: resourceNodeId,
        target: `p:${projectId}`,
        type: "straight",
        style: { stroke: "var(--muted-foreground)", strokeDasharray: "4 3" },
        focusable: false,
        selectable: false,
      });
    }
  }

  for (const e of map.edges) {
    const label = e.suggested > 0 ? `${e.links} · +${e.suggested}` : `${e.links}`;
    edges.push({
      id: `edge:${e.from_project_id}:${e.to_project_id}`,
      source: `p:${e.from_project_id}`,
      target: `p:${e.to_project_id}`,
      type: "smoothstep",
      label,
      labelBgPadding: [4, 2],
      labelBgBorderRadius: 4,
      style: { stroke: "var(--color-info)", strokeWidth: 2 },
      data: { workspaceEdge: e },
    });
  }

  const independentAreaY = (map.shared_resources.length > 0 ? resourceY + 140 : linkedMaxHeight) + (linked.length > 0 || map.shared_resources.length > 0 ? 60 : 0);
  independent.forEach((project, i) => {
    const col = i % WORKSPACE_INDEPENDENT_COLUMNS;
    const row = Math.floor(i / WORKSPACE_INDEPENDENT_COLUMNS);
    const ix = col * (WORKSPACE_PROJECT_WIDTH + WORKSPACE_PROJECT_GAP_X);
    const iy = independentAreaY + row * WORKSPACE_INDEPENDENT_ROW_HEIGHT;
    pushProjectNodes(nodes, project, ix, iy, true);
  });

  return { nodes, edges };
}
