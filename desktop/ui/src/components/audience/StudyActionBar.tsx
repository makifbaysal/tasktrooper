import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface StudyActionBarProps {
  /** What is happening, announced politely as it changes. */
  status: ReactNode;
  /** The bar under the status line, usually a Progress. */
  progress?: ReactNode;
  /** The buttons on the right. */
  actions: ReactNode;
  className?: string;
}

/** The bar at the bottom of a study step: progress on the left, the step's action on the right. */
export function StudyActionBar({ status, progress, actions, className }: StudyActionBarProps) {
  return (
    <div
      className={cn(
        "sticky bottom-0 z-10 -mx-1 mt-6 flex flex-col gap-3 rounded-xl border border-border bg-background/95 p-4 shadow-[var(--shadow-raised)] backdrop-blur sm:flex-row sm:items-center sm:justify-between",
        className,
      )}
    >
      <div className="min-w-0 flex-1 space-y-2">
        <p className="text-body" role="status" aria-live="polite">
          {status}
        </p>
        {progress}
      </div>
      <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">{actions}</div>
    </div>
  );
}
