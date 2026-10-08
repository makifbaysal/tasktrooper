import { Maximize, Minus, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { HelpTooltip } from "@/components/ui/help-tooltip";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export type CanvasZoomAction = "in" | "out" | "reset" | "fit";

interface CanvasZoomControlsProps {
  /** The frame's last reported zoom; null until it has reported one. */
  zoom: number | null;
  onAction: (action: CanvasZoomAction) => void;
  className?: string;
}

/**
 * Molecule: the zoom bar floating over a design canvas frame — out, the
 * current level (click for 100%), in, fit to width, and how to pan.
 */
export function CanvasZoomControls({ zoom, onAction, className }: CanvasZoomControlsProps) {
  const { t } = useI18n();
  const percent = zoom === null ? 100 : Math.round(zoom * 100);

  return (
    <div
      role="toolbar"
      aria-label={t("analysisReview.canvas.label")}
      className={cn(
        "flex items-center gap-0.5 rounded-lg border border-border bg-background/95 p-1 shadow-sm backdrop-blur",
        className,
      )}
    >
      <Button
        variant="ghost"
        size="icon"
        className="h-7 w-7"
        onClick={() => onAction("out")}
        aria-label={t("analysisReview.canvas.zoomOut")}
        title={t("analysisReview.canvas.zoomOut")}
      >
        <Minus />
      </Button>
      <Button
        variant="ghost"
        size="sm"
        className="h-7 min-w-14 px-2 font-mono tabular-nums"
        onClick={() => onAction("reset")}
        aria-label={t("analysisReview.canvas.reset")}
        title={t("analysisReview.canvas.reset")}
      >
        {percent}%
      </Button>
      <Button
        variant="ghost"
        size="icon"
        className="h-7 w-7"
        onClick={() => onAction("in")}
        aria-label={t("analysisReview.canvas.zoomIn")}
        title={t("analysisReview.canvas.zoomIn")}
      >
        <Plus />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        className="h-7 w-7"
        onClick={() => onAction("fit")}
        aria-label={t("analysisReview.canvas.fit")}
        title={t("analysisReview.canvas.fit")}
      >
        <Maximize />
      </Button>
      <HelpTooltip text={t("analysisReview.canvas.hint")} className="px-1.5" />
    </div>
  );
}
