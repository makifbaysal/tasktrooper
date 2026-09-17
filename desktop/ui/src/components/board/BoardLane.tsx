import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";

export interface BoardLaneStage {
  /** The board column slug — also the drop target's column id. */
  slug: string;
  label: string;
  count: number;
}

interface BoardLaneProps {
  /** Stages of this lane, top to bottom. One entry renders a plain column. */
  stages: BoardLaneStage[];
  /** True while a card is being dragged, so only then is a lane highlighted. */
  dragging: boolean;
  /** Slug of the stage currently under the pointer, or null. */
  dropColumn: string | null;
  onDropColumnChange: (slug: string | null) => void;
  onDropTask: (slug: string) => void;
  /** Cards of one stage (or its empty state) — the caller owns card rendering. */
  renderStage: (stage: BoardLaneStage) => ReactNode;
  className?: string;
}

/**
 * One board lane: a single column, or several columns stacked vertically inside
 * the width of one (see `BOARD_STACKED_LANES` in lib/project-board).
 *
 * Every stage stays a drop target of its own, keyed by its own column slug —
 * stacking is layout only, so dragging into the lower stage still moves the task
 * to that stage's column. Each stage scrolls independently and is height-capped,
 * so a long queue in one cannot push the other out of the lane.
 */
export function BoardLane({
  stages,
  dragging,
  dropColumn,
  onDropColumnChange,
  onDropTask,
  renderStage,
  className,
}: BoardLaneProps) {
  const stacked = stages.length > 1;

  return (
    <div className={cn("flex h-full min-h-0 shrink-0 flex-col gap-3", className)}>
      {stages.map((stage) => (
        <div
          key={stage.slug}
          data-column-slug={stage.slug}
          className={cn(
            "flex min-h-0 flex-col rounded-xl border bg-background transition-colors",
            // Stacked stages split the lane's height evenly (flex-1 basis-0)
            // and scroll inside it, so the lane never grows with its longest
            // queue; a lone stage still fills the lane exactly as before.
            // No max height here: the lane is already bounded by the board's
            // own height, and capping each stage left dead space under a
            // stacked lane on any screen taller than the cap, while its
            // single-stage neighbours ran the full height.
            stacked ? "min-h-32 flex-1 basis-0" : "h-full flex-1",
            dragging && dropColumn === stage.slug
              ? "border-primary bg-primary/5 ring-2 ring-primary/20"
              : "border-border",
          )}
          onDragOver={(e) => {
            e.preventDefault();
            onDropColumnChange(stage.slug);
          }}
          onDragLeave={(e) => {
            // Moving onto a card *inside* this stage fires dragleave too; only
            // a leave that actually exits the stage clears the highlight, which
            // matters most between two stacked stages.
            if (e.currentTarget.contains(e.relatedTarget as Node | null)) return;
            onDropColumnChange(null);
          }}
          onDrop={() => onDropTask(stage.slug)}
        >
          <div className="flex shrink-0 items-center justify-between border-b border-border px-3 py-2.5">
            <span className="truncate text-heading font-medium">{stage.label}</span>
            <Badge variant="secondary" className="h-5 min-w-5 justify-center px-1.5 text-micro">
              {stage.count}
            </Badge>
          </div>
          <ScrollArea className="min-h-0 flex-1">
            <div className="p-2">{renderStage(stage)}</div>
          </ScrollArea>
        </div>
      ))}
    </div>
  );
}
