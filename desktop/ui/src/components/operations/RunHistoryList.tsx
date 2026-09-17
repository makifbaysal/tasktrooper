import { CheckCircle2, History, Loader2, MinusCircle, XCircle, type LucideIcon } from "lucide-react";
import { type DeploymentRun } from "@/api";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatRelativeDate } from "@/lib/utils";

interface RunHistoryListProps {
  runs: DeploymentRun[];
}

// Mirrors PipelineStages' jobStatusIcon: one glyph per run outcome, with a
// catch-all for "completed but neither success nor failure" (e.g.
// cancelled) so every value of domain.RunConclusion still renders something.
function conclusionIcon(run: DeploymentRun): { Icon: LucideIcon; className: string } {
  if (run.conclusion === "success") return { Icon: CheckCircle2, className: "text-success" };
  if (run.conclusion === "failure") return { Icon: XCircle, className: "text-destructive" };
  if (run.status !== "completed") return { Icon: Loader2, className: "text-info animate-spin" };
  return { Icon: MinusCircle, className: "text-warning" };
}

/**
 * RunHistoryList — one repo x env's deploy runs, most recent first (the API
 * already orders them): outcome icon, linked short SHA, relative time, who
 * triggered it, and a rollback marker when the run itself was a rollback.
 */
export function RunHistoryList({ runs }: RunHistoryListProps) {
  const { t } = useI18n();

  if (runs.length === 0) {
    return <EmptyState icon={History} title={t("operations.deployments.noHistory")} />;
  }

  return (
    <div className="divide-y divide-border rounded-lg border border-border">
      {runs.map((run) => {
        const { Icon, className } = conclusionIcon(run);
        return (
          <div key={run.id} className="flex flex-wrap items-center gap-2 px-4 py-3 text-sm">
            <Icon className={cn("h-4 w-4 shrink-0", className)} />
            {run.html_url ? (
              <a
                href={run.html_url}
                target="_blank"
                rel="noreferrer"
                className="font-mono text-xs text-primary underline underline-offset-2"
              >
                {run.head_sha.slice(0, 7)}
              </a>
            ) : (
              // A local (break-glass) run has no Actions page behind it, so the
              // SHA is plain text rather than a link to nowhere.
              <span className="font-mono text-xs">{run.head_sha.slice(0, 7) || "—"}</span>
            )}
            <span className="text-xs text-muted-foreground">
              {formatRelativeDate(run.started_at ?? run.completed_at ?? run.created_at)}
            </span>
            {run.trigger_source === "local" && (
              <Badge variant="secondary" title={run.workflow_file}>
                {t("operations.deployments.localRun")}
              </Badge>
            )}
            {run.trigger_source !== "external" && (
              <Badge variant="outline">
                {t("operations.deployments.triggeredBy", { actor: run.triggered_by })}
              </Badge>
            )}
            {run.rollback_of_sha && (
              <Badge variant="secondary">{t("operations.deployments.rollback")}</Badge>
            )}
          </div>
        );
      })}
    </div>
  );
}
