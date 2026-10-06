import { BaseEdge, EdgeLabelRenderer, useInternalNode, type Edge, type EdgeProps, type InternalNode } from "@xyflow/react";
import { X } from "lucide-react";
import { useI18n } from "@/hooks/useI18n";
import { WORKFLOW_NODE_HEIGHT, WORKFLOW_NODE_WIDTH } from "@/lib/workflowGraphLayout";
import { cn } from "@/lib/utils";

export interface WorkflowTransitionEdgeData extends Record<string, unknown> {
  /** The opposite transition exists too; bend this one so both stay visible. */
  paired: boolean;
  dimmed: boolean;
  onRemove: (id: string) => void;
}

export type WorkflowTransitionFlowEdge = Edge<WorkflowTransitionEdgeData, "transition">;

interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
}

function boxOf(node: InternalNode | undefined): Box | null {
  if (!node) return null;
  const { x, y } = node.internals.positionAbsolute;
  return { x, y, w: node.measured.width ?? WORKFLOW_NODE_WIDTH, h: node.measured.height ?? WORKFLOW_NODE_HEIGHT };
}

// Where the line from `box`'s centre towards `toward` leaves the box's border.
function borderPoint(box: Box, toward: { x: number; y: number }) {
  const cx = box.x + box.w / 2;
  const cy = box.y + box.h / 2;
  const dx = toward.x - cx;
  const dy = toward.y - cy;
  if (dx === 0 && dy === 0) return { x: cx, y: cy };
  const scale = 1 / Math.max(Math.abs(dx) / (box.w / 2), Math.abs(dy) / (box.h / 2));
  return { x: cx + dx * scale, y: cy + dy * scale };
}

/** A straight arrow from border to border — the Jira workflow look — bent
 * into an arc when the opposite transition is drawn too. */
export function WorkflowTransitionEdge({ id, source, target, data, selected, markerEnd }: EdgeProps<WorkflowTransitionFlowEdge>) {
  const { t } = useI18n();
  const from = boxOf(useInternalNode(source));
  const to = boxOf(useInternalNode(target));
  if (!from || !to || !data) return null;

  const fromCenter = { x: from.x + from.w / 2, y: from.y + from.h / 2 };
  const toCenter = { x: to.x + to.w / 2, y: to.y + to.h / 2 };
  const dx = toCenter.x - fromCenter.x;
  const dy = toCenter.y - fromCenter.y;
  const length = Math.hypot(dx, dy) || 1;
  const bend = data.paired ? 26 : 0;
  const control = {
    x: (fromCenter.x + toCenter.x) / 2 + (-dy / length) * bend,
    y: (fromCenter.y + toCenter.y) / 2 + (dx / length) * bend,
  };
  const start = borderPoint(from, data.paired ? control : toCenter);
  const end = borderPoint(to, data.paired ? control : fromCenter);
  const path = data.paired
    ? `M ${start.x} ${start.y} Q ${control.x} ${control.y} ${end.x} ${end.y}`
    : `M ${start.x} ${start.y} L ${end.x} ${end.y}`;
  const mid = data.paired
    ? { x: 0.25 * start.x + 0.5 * control.x + 0.25 * end.x, y: 0.25 * start.y + 0.5 * control.y + 0.25 * end.y }
    : { x: (start.x + end.x) / 2, y: (start.y + end.y) / 2 };

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        interactionWidth={18}
        className={cn("transition-opacity", data.dimmed && !selected && "opacity-15")}
        style={{
          stroke: selected ? "var(--color-primary)" : "var(--color-muted-foreground)",
          strokeWidth: selected ? 2.25 : 1.25,
        }}
      />
      {selected && (
        <EdgeLabelRenderer>
          <button
            type="button"
            className="nodrag nopan pointer-events-auto absolute flex h-6 w-6 items-center justify-center rounded-full border border-border bg-popover text-muted-foreground shadow-[var(--shadow-raised)] hover:text-destructive"
            style={{ transform: `translate(-50%, -50%) translate(${mid.x}px, ${mid.y}px)` }}
            onClick={() => data.onRemove(id)}
            title={t("settingsPages.board.graph.removeTransition")}
            aria-label={t("settingsPages.board.graph.removeTransition")}
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </EdgeLabelRenderer>
      )}
    </>
  );
}
