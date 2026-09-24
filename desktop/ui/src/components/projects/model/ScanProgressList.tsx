import { CheckCircle2, CircleDashed, Loader2, XCircle } from "lucide-react";
import type { ProjectScan, ScanEvent, ScanStage } from "@/api";
import { useI18n } from "@/hooks/useI18n";
import { scanStagesInOrder } from "@/lib/project-model";
import { cn } from "@/lib/utils";

type StageState = "pending" | "running" | "done" | "failed";

/** The clone happens inside the GitHub import call, before any scan exists,
 * so only the caller knows how it went. */
export interface CloneProgress {
  state: "running" | "done" | "failed";
  error?: string;
}

function stageState(scan: ProjectScan | null | undefined, stage: ScanStage, event: ScanEvent | undefined): StageState {
  if (event?.done) return "done";
  if (scan?.status === "failed" && (event || scan.stage === stage)) return "failed";
  if (event) return "running";
  if (scan?.status === "running" && scan.stage === stage) return "running";
  return "pending";
}

interface ScanProgressListProps {
  scan: ProjectScan | null | undefined;
  clone?: CloneProgress;
  className?: string;
}

/** The scan's fixed stage pipeline, each marked pending/running/done/failed;
 * a done stage shows the event's own summary sentence, a failed one the error.
 * The clone row only appears when something cloned: a rescan never does. */
export function ScanProgressList({ scan, clone, className }: ScanProgressListProps) {
  const { t } = useI18n();
  const latestByStage = new Map<ScanStage, ScanEvent>();
  for (const event of scan?.events ?? []) {
    latestByStage.set(event.stage, event);
  }
  const stages = scanStagesInOrder().filter((stage) => stage !== "clone" || clone || latestByStage.has("clone"));

  return (
    <ol className={cn("flex flex-col divide-y divide-border", className)}>
      {stages.map((stage) => {
        const event = latestByStage.get(stage);
        const state = stage === "clone" && clone ? clone.state : stageState(scan, stage, event);
        const error = stage === "clone" && clone ? clone.error : scan?.error;
        return (
          <li key={stage} className="flex items-start gap-3 py-2">
            {state === "done" && <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" aria-hidden />}
            {state === "running" && (
              <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-primary" aria-hidden />
            )}
            {state === "failed" && <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" aria-hidden />}
            {state === "pending" && (
              <CircleDashed className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
            )}
            <div className="flex min-w-0 flex-col">
              <span
                className={cn(
                  "text-body",
                  state === "pending" && "text-muted-foreground",
                  state === "failed" && "text-destructive",
                )}
              >
                {t(`projectModel.scanStages.${stage}`)}
              </span>
              {state === "done" && event?.summary && (
                <span className="text-caption text-muted-foreground">{event.summary}</span>
              )}
              {state === "failed" && error && (
                <span className="break-words text-caption text-destructive/90">{error}</span>
              )}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
