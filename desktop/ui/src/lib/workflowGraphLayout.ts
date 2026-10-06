export const WORKFLOW_NODE_WIDTH = 148;
export const WORKFLOW_NODE_HEIGHT = 50;
const COLUMN_GAP = 196;
const STACK_GAP = 70;
const LANE_GAP = 84;
// Above this many ranks the chain folds into two lanes (the second running
// right to left), so a twelve-column board stays readable at fit-view scale.
const SINGLE_LANE_MAX_RANKS = 6;

export interface WorkflowLayoutColumn {
  slug: string;
  position: number;
}

export interface WorkflowLayoutEdge {
  from: string;
  to: string;
}

export interface WorkflowNodePosition {
  x: number;
  y: number;
}

/**
 * The columns most others lead into (blocked, need revision). Their incoming
 * arrows are what turns a drawn workflow into a hairball, so the layout gives
 * them their own row and the editor draws those arrows only on demand.
 */
export function findWorkflowHubs(columns: WorkflowLayoutColumn[], edges: WorkflowLayoutEdge[]): Set<string> {
  const ordered = [...columns].sort((a, b) => a.position - b.position);
  const known = new Set(ordered.map((c) => c.slug));
  const inDegree = new Map<string, number>();
  for (const e of edges) {
    if (e.from === e.to || !known.has(e.from) || !known.has(e.to)) continue;
    inDegree.set(e.to, (inDegree.get(e.to) ?? 0) + 1);
  }
  const threshold = Math.max(4, Math.ceil(ordered.length * 0.4));
  return new Set(ordered.filter((c, i) => i > 0 && (inDegree.get(c.slug) ?? 0) >= threshold).map((c) => c.slug));
}

/**
 * Lays a board's columns out as a workflow graph: the main path ranked by
 * longest forward path, ranks folded into two lanes when the path is long,
 * and the hubs most columns lead into (blocked, need revision) on their own
 * row between the lanes so their many incoming edges stay short.
 */
export function layoutWorkflow(
  columns: WorkflowLayoutColumn[],
  edges: WorkflowLayoutEdge[],
): Map<string, WorkflowNodePosition> {
  const ordered = [...columns].sort((a, b) => a.position - b.position);
  const index = new Map(ordered.map((c, i) => [c.slug, i]));
  const valid = edges.filter((e) => e.from !== e.to && index.has(e.from) && index.has(e.to));

  const hubs = findWorkflowHubs(ordered, valid);

  const incoming = new Map<string, string[]>();
  for (const e of valid) {
    if (hubs.has(e.from) || hubs.has(e.to)) continue;
    if (index.get(e.to)! <= index.get(e.from)!) continue;
    incoming.set(e.to, [...(incoming.get(e.to) ?? []), e.from]);
  }

  const rank = new Map<string, number>();
  let previous: string | null = null;
  for (const c of ordered) {
    if (hubs.has(c.slug)) continue;
    const from = incoming.get(c.slug) ?? [];
    const r: number = from.length
      ? Math.max(...from.map((f) => (rank.get(f) ?? 0) + 1))
      : previous === null
        ? 0
        : (rank.get(previous) ?? 0) + 1;
    rank.set(c.slug, r);
    previous = c.slug;
  }

  const ranks: string[][] = [];
  for (const c of ordered) {
    const r = rank.get(c.slug);
    if (r === undefined) continue;
    (ranks[r] ??= []).push(c.slug);
  }
  const used = ranks.filter((group) => group && group.length > 0);

  const lanes: string[][][] =
    used.length > SINGLE_LANE_MAX_RANKS
      ? [used.slice(0, Math.ceil(used.length / 2)), used.slice(Math.ceil(used.length / 2))]
      : [used];
  const laneWidth = Math.max(...lanes.map((lane) => lane.length));
  const laneHeight = (lane: string[][]) => Math.max(1, ...lane.map((group) => group.length)) * STACK_GAP;

  const out = new Map<string, WorkflowNodePosition>();
  const placeLane = (lane: string[][], top: number, reversed: boolean) => {
    const height = laneHeight(lane);
    lane.forEach((group, i) => {
      const col = reversed ? laneWidth - 1 - i : i;
      group.forEach((slug, j) => {
        out.set(slug, {
          x: col * COLUMN_GAP,
          y: top + height / 2 + (j - (group.length - 1) / 2) * STACK_GAP - WORKFLOW_NODE_HEIGHT / 2,
        });
      });
    });
    return top + height;
  };

  let bottom = placeLane(lanes[0], 0, false);
  const hubList = ordered.filter((c) => hubs.has(c.slug)).map((c) => c.slug);
  const middleX = ((laneWidth - 1) * COLUMN_GAP) / 2;
  const placeHubs = (top: number) => {
    hubList.forEach((slug, i) => {
      out.set(slug, { x: middleX + (i - (hubList.length - 1) / 2) * COLUMN_GAP * 1.5, y: top });
    });
    return top + WORKFLOW_NODE_HEIGHT;
  };
  if (hubList.length > 0) bottom = placeHubs(bottom + LANE_GAP);
  if (lanes[1]) placeLane(lanes[1], bottom + LANE_GAP, true);
  return out;
}
