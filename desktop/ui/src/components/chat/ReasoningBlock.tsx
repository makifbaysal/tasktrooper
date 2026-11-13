import { Brain, ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface ReasoningBlockProps {
  /** Reasoning stretches, oldest first. Empty renders nothing. */
  segments: string[];
  /**
   * Controlled open state. The chat passes it so the block a running turn
   * renders and the one its finished message renders share it: without that
   * the block folds itself shut, or springs open, the moment the turn lands.
   */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  /**
   * The turn is still running and its last segment is still being written.
   * Folded, the header then shows that segment's latest line, so a long
   * tool-calling run is visibly alive without the block having to be open.
   */
  live?: boolean;
  className?: string;
}

/**
 * The model's reasoning ahead of its tool calls, folded away. Deliberately not a
 * message bubble: this text explains why a call was made, and rendering it like
 * a reply is what made the agent look as if it had answered twice — once with
 * its thinking, then again for real.
 */
export function ReasoningBlock({ segments, open: openProp, onOpenChange, live = false, className }: ReasoningBlockProps) {
  const { t } = useI18n();
  const [ownOpen, setOwnOpen] = useState(false);
  const open = openProp ?? ownOpen;
  if (segments.length === 0) return null;

  const toggle = () => {
    if (openProp === undefined) setOwnOpen(!open);
    onOpenChange?.(!open);
  };
  const preview = live && !open ? latestLine(segments[segments.length - 1]) : "";

  return (
    // min-w-0: this block is dropped straight into a `flex justify-start` row
    // by MessageList. A flex item's default min-width is its content size, so
    // without this a wide table/URL inside a reasoning segment would grow the
    // whole block past its `max-w-[85%]` instead of letting the table's own
    // overflow-x:auto (below) do the clipping.
    <div
      data-testid="reasoning-block"
      className={cn("min-w-0 rounded-xl border border-dashed border-border/70 bg-muted/40", className)}
    >
      <button
        type="button"
        onClick={toggle}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:text-foreground"
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0" />
        )}
        <Brain className={cn("h-3.5 w-3.5 shrink-0", live && "animate-pulse")} />
        <span className="shrink-0">{t("chatArea.chat.reasoning.title", { count: segments.length })}</span>
        {preview && (
          <span data-testid="reasoning-preview" className="min-w-0 truncate opacity-70">
            {preview}
          </span>
        )}
      </button>
      {open && (
        <div className="space-y-3 border-t border-dashed border-border/70 px-3 py-2">
          {segments.map((segment, i) => (
            <MarkdownContent key={`${i}-${segment.slice(0, 24)}`} content={segment} className="text-xs opacity-80" />
          ))}
        </div>
      )}
    </div>
  );
}

function latestLine(segment: string | undefined): string {
  const lines = (segment ?? "").split("\n");
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i].trim();
    if (line) return line;
  }
  return "";
}
