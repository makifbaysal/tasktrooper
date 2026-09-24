import { CheckCircle2, CircleDashed, Loader2 } from "lucide-react";
import type { ProjectScan, ScanEvent, ScanStage } from "@/api";
import { useI18n } from "@/hooks/useI18n";
import { scanStagesInOrder } from "@/lib/project-model";
import { cn } from "@/lib/utils";

type StageState = "pending" | "running" | "done";

function stageState(scan: ProjectScan | null | undefined, stage: ScanStage, event: ScanEvent | undefined): StageState {
  if (event?.done) return "done";
  if (event) return "running";
  if (scan?.status === "running" && scan.stage === stage) return "running";
  return "pending";
}

interface ScanProgressListProps {
  scan: ProjectScan | null | undefined;
  className?: string;
}

/** The scan's fixed stage pipeline, each marked pending/running/done; a done
 * stage shows the event's own summary sentence. */
export function ScanProgressList({ scan, className }: ScanProgressListProps) {
  const { t } = useI18n();
  const latestByStage = new Map<ScanStage, ScanEvent>();
  for (const event of scan?.events ?? []) {
    latestByStage.set(event.stage, event);
  }

  return (
    <ol className={cn("flex flex-col divide-y divide-border", className)}>
      {scanStagesInOrder().map((stage) => {
        const event = latestByStage.get(stage);
        const state = stageState(scan, stage, event);
        return (
          <li key={stage} className="flex items-start gap-3 py-2">
            {state === "done" && <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" aria-hidden />}
            {state === "running" && (
              <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-primary" aria-hidden />
            )}
            {state === "pending" && (
              <CircleDashed className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
            )}
            <div className="flex flex-col">
              <span className={cn("text-body", state === "pending" && "text-muted-foreground")}>
                {t(`projectModel.scanStages.${stage}`)}
              </span>
              {state === "done" && event?.summary && (
                <span className="text-caption text-muted-foreground">{event.summary}</span>
              )}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
