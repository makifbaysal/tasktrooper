import { Activity } from "lucide-react";
import type { OrchestrationPlan, SessionRun, SessionStep } from "@/api";
import { SessionGraphView } from "@/components/chat/SessionGraphView";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { EmptyState } from "@/components/ui/empty-state";
import { formatDate } from "@/lib/utils";
import { taskStatusVariant } from "@/lib/planUtils";
import { getLiveStepSummary } from "@/lib/sessionGraph";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface ActivityPanelProps {
  activeRuns: SessionRun[];
  runs: SessionRun[];
  selectedRunId: string | null;
  onSelectRun: (id: string) => void;
  steps: SessionStep[];
  plan: OrchestrationPlan | null;
  isLive?: boolean;
  embedded?: boolean;
}

export function ActivityPanel({
  activeRuns,
  runs,
  selectedRunId,
  onSelectRun,
  steps,
  plan,
  isLive = false,
  embedded = false,
}: ActivityPanelProps) {
  const { t } = useI18n();
  const sessionActiveRuns = activeRuns.filter(
    (run) => !run.session_id || runs.some((sessionRun) => sessionRun.id === run.id),
  );
  const liveSummary = isLive ? getLiveStepSummary(steps, isLive) : null;

  // The scroll wrapper only works where the panel is given a height — the chat
  // page's full-height sidebar. Embedded in the task drawer the parent height is
  // auto, and ScrollArea's viewport is absolutely positioned, so a `flex-1`
  // ScrollArea measured zero and the panel rendered as a header with nothing
  // under it: no active run, no steps, no graph. Embedded therefore flows
  // naturally and lets the drawer do the scrolling.
  const Body = embedded ? "div" : ScrollArea;

  return (
    <div
      data-testid="activity-panel"
      className={cn(
        "flex min-w-0 flex-col bg-muted/10",
        embedded
          ? "w-full"
          : "h-full w-80 shrink-0 overflow-hidden border-l border-border shadow-[var(--shadow-raised)]",
      )}
    >
      <div className="shrink-0 border-b border-border p-3">
        <h2 className="flex min-w-0 items-center gap-2 text-heading font-semibold">
          <Activity className="h-4 w-4 shrink-0" />
          <span className="truncate">{t("chatArea.chat.activityPanel.title")}</span>
          {isLive && (
            <Badge variant="warning" className="ml-auto text-micro">
              {t("chatArea.chat.activityPanel.live")}
            </Badge>
          )}
        </h2>
      </div>

      <Body className={cn("min-w-0", !embedded && "min-h-0 flex-1")}>
        <div className="min-w-0 max-w-full space-y-4 overflow-x-hidden p-3">
          <section className="min-w-0 max-w-full">
            <h3 className="mb-2 text-micro font-medium uppercase tracking-wide text-muted-foreground">
              {t("chatArea.chat.activityPanel.activeRuns")}
            </h3>
            {sessionActiveRuns.length === 0 ? (
              <p className="text-caption text-muted-foreground">{t("chatArea.chat.activityPanel.noActiveRuns")}</p>
            ) : (
              <ul className="space-y-2">
                {sessionActiveRuns.map((run) => (
                  <li key={run.id} className="min-w-0 max-w-full rounded-lg border border-warning/30 bg-warning/5 p-2 text-caption">
                    <div className="flex min-w-0 items-center justify-between gap-2">
                      <Badge variant={taskStatusVariant(run.status)} className="shrink-0">
                        {run.status}
                      </Badge>
                      {run.model && <span className="min-w-0 truncate text-muted-foreground">{run.model}</span>}
                    </div>
                    {liveSummary && (
                      <p className="mt-1.5 break-words font-medium text-foreground">{liveSummary}</p>
                    )}
                    <p className="mt-1 text-muted-foreground">{formatDate(run.started_at)}</p>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <Separator />

          <section className="min-w-0 max-w-full">
            <h3 className="mb-2 text-micro font-medium uppercase tracking-wide text-muted-foreground">
              {t("chatArea.chat.activityPanel.runGraph")}
            </h3>
            {runs.length === 0 ? (
              <EmptyState icon={Activity} title={t("chatArea.chat.activityPanel.noRuns")} className="py-6" />
            ) : (
              <div className="min-w-0 max-w-full space-y-3">
                <Select value={selectedRunId ?? ""} onValueChange={onSelectRun}>
                  <SelectTrigger className="h-8 w-full max-w-full text-caption">
                    <SelectValue placeholder={t("chatArea.chat.activityPanel.selectRun")} />
                  </SelectTrigger>
                  <SelectContent>
                    {runs.map((run) => (
                      <SelectItem key={run.id} value={run.id}>
                        {run.status} — {formatDate(run.started_at)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>

                <SessionGraphView steps={steps} plan={plan} isLive={isLive} />
              </div>
            )}
          </section>
        </div>
      </Body>
    </div>
  );
}
