import { Handle, Position, useConnection, type Node, type NodeProps } from "@xyflow/react";
import { useI18n } from "@/hooks/useI18n";
import { WORKFLOW_NODE_HEIGHT, WORKFLOW_NODE_WIDTH } from "@/lib/workflowGraphLayout";
import { cn } from "@/lib/utils";

export type WorkflowStatusCategory = "todo" | "active" | "revision" | "blocked" | "done";

export interface WorkflowStatusNodeData extends Record<string, unknown> {
  label: string;
  category: WorkflowStatusCategory;
  /** No outgoing rule: a task may leave this column for any other. */
  free: boolean;
  /** A hub whose incoming arrows are hidden: how many columns lead here. */
  inbound: number | null;
  dimmed: boolean;
}

export type WorkflowStatusFlowNode = Node<WorkflowStatusNodeData, "status">;

const CATEGORY_STYLE: Record<WorkflowStatusCategory, string> = {
  todo: "border-border bg-muted text-foreground",
  active: "border-info/50 bg-info/10 text-foreground",
  revision: "border-warning/50 bg-warning/10 text-foreground",
  blocked: "border-destructive/50 bg-destructive/10 text-foreground",
  done: "border-success/50 bg-success/10 text-foreground",
};

export function WorkflowStatusNode({ id, data, selected }: NodeProps<WorkflowStatusFlowNode>) {
  const { t } = useI18n();
  const connection = useConnection();
  // The whole node becomes a drop target only while a transition is being
  // drawn from another node; otherwise it must stay clickable and draggable.
  const isDropTarget = connection.inProgress && connection.fromNode?.id !== id;

  return (
    <div
      className={cn(
        "group relative flex flex-col items-center justify-center gap-0.5 rounded-lg border px-3 text-caption font-semibold uppercase tracking-wide shadow-sm transition-opacity",
        CATEGORY_STYLE[data.category],
        selected && "ring-2 ring-ring ring-offset-1 ring-offset-background",
        data.dimmed && "opacity-40",
      )}
      style={{ width: WORKFLOW_NODE_WIDTH, height: WORKFLOW_NODE_HEIGHT }}
    >
      <span className="max-w-full truncate">{data.label}</span>
      {(data.inbound !== null || data.free) && (
        <span className="max-w-full truncate text-micro font-medium normal-case tracking-normal text-muted-foreground">
          {[
            data.inbound !== null ? t("settingsPages.board.graph.fromStatuses", { count: data.inbound }) : null,
            data.free ? t("settingsPages.board.freeToAnywhere") : null,
          ]
            .filter(Boolean)
            .join(" · ")}
        </span>
      )}
      <Handle
        type="target"
        position={Position.Left}
        isConnectableStart={false}
        className={cn("!border-0 !bg-transparent", isDropTarget ? "!inset-0 !h-full !w-full !transform-none !rounded-lg" : "!opacity-0")}
      />
      <Handle
        type="source"
        position={Position.Right}
        title={t("settingsPages.board.graph.dragToConnect")}
        className="!h-3 !w-3 !border-2 !border-background !bg-primary opacity-0 transition-opacity group-hover:opacity-100"
      />
    </div>
  );
}
