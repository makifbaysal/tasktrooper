import { CircleCheck, CircleDashed, CircleHelp, CircleSlash, CircleX, ExternalLink, History, Loader2, Rocket, Undo2, type LucideIcon } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, type DeployEnv, type DeployEnvironment, type DeploymentRun } from "@/api";
import {
  canRollBack,
  dispatchNeedsPhrase,
  runDurationMs,
  runStartedAt,
  workflowDeployRows,
  workflowRunState,
  type WorkflowDeployRow,
  type WorkflowRunState,
} from "@/components/operations/changes/workflowDeploys";
import { DetailPanel } from "@/components/operations/console/DetailPanel";
import { useOpsDirectory } from "@/components/operations/shell/useOpsEntities";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useDeployMatrix, useDeployRuns } from "@/hooks/useWorkflowDeploys";
import { environmentFromLabel } from "@/lib/environmentLabel";
import { githubCommitUrl } from "@/lib/githubUrl";
import { cn, formatDate, formatDuration, formatRelativeTime } from "@/lib/utils";

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-2 align-middle";

const RUN_META: Record<WorkflowRunState, { variant: NonNullable<BadgeProps["variant"]>; icon: LucideIcon; spin?: boolean }> = {
  queued: { variant: "secondary", icon: CircleDashed },
  running: { variant: "info", icon: Loader2, spin: true },
  success: { variant: "success", icon: CircleCheck },
  failure: { variant: "destructive", icon: CircleX },
  cancelled: { variant: "secondary", icon: CircleSlash },
  unknown: { variant: "outline", icon: CircleHelp },
};

const TRIGGERS = new Set(["ui", "rollback", "external", "local"]);

function WorkflowRunPill({ run }: { run: DeploymentRun }) {
  const { t } = useI18n();
  const state = workflowRunState(run);
  const { variant, icon: Icon, spin } = RUN_META[state];
  return (
    <Badge variant={variant} className="gap-1 px-2 py-0 text-micro whitespace-nowrap">
      <Icon className={cn("h-3 w-3 shrink-0", spin && "animate-spin")} aria-hidden />
      {t(`operations.changes.workflow.states.${state}`)}
    </Badge>
  );
}

type Action = "deploy" | "rollback";

interface WorkflowDeploysSectionProps {
  environment: DeployEnvironment;
  /** Narrows the section to one repository (the page's entity filter). */
  repositoryId?: string;
}

/** Deploy targets that dispatch a GitHub Actions workflow on their own,
 * outside release tracking: per repository × environment the last run, its
 * history, and Deploy / Roll back with the server's typed-name rules. Renders
 * nothing when no target can be dispatched or has ever run here. */
export function WorkflowDeploysSection({ environment, repositoryId }: WorkflowDeploysSectionProps) {
  const { t, lang } = useI18n();
  const w = (key: string, params?: Record<string, string | number>) => t(`operations.changes.workflow.${key}`, params);
  const matrix = useDeployMatrix();
  const { repositories } = useOpsDirectory();
  const rows = useMemo(() => workflowDeployRows(matrix.value, { environment, repositoryId }), [matrix.value, environment, repositoryId]);

  const [panel, setPanel] = useState<WorkflowDeployRow | null>(null);
  const [pending, setPending] = useState<{ row: WorkflowDeployRow; action: Action } | null>(null);
  const [ref, setRef] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);
  const refId = useId();
  const refHintId = useId();
  const runs = useDeployRuns(panel ? { repositoryId: panel.repositoryId, env: panel.env } : null);

  const remoteOf = (id: string) => repositories.find((r) => r.id === id)?.remoteUrl;
  const envLabel = (env: DeployEnv) => {
    const mapped = environmentFromLabel(env);
    return mapped ? t(`cloud.environments.${mapped}`) : env;
  };

  // Most workspaces have no such target: nothing is drawn (not even a
  // skeleton) until the matrix says there is one.
  if (rows.length === 0) return null;

  const ask = (row: WorkflowDeployRow, action: Action) => {
    setRef("");
    setRefusal(null);
    setPending({ row, action });
  };

  const act = async (typed: string): Promise<boolean> => {
    if (!pending) return true;
    const { row, action } = pending;
    setBusy(true);
    try {
      if (action === "deploy") {
        await api.dispatchDeploy(row.repositoryId, row.env, {
          ref: ref.trim() || undefined,
          confirm: dispatchNeedsPhrase(row.env) ? typed : undefined,
        });
      } else {
        await api.rollbackDeploy(row.repositoryId, row.env, typed);
      }
      toast.success(w(action === "deploy" ? "deployed" : "rolledBack"));
      matrix.reload();
      if (panel?.key === row.key) void runs.reload();
      return true;
    } catch (err) {
      setRefusal(err instanceof Error ? err.message : t("common.actionFailed"));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const target = pending?.row;
  const names = target ? { repository: target.repositoryName, env: envLabel(target.env) } : { repository: "", env: "" };

  return (
    <section className="space-y-2" aria-labelledby={`${refId}-title`}>
      <div>
        <h2 id={`${refId}-title`} className="text-body font-semibold">
          {w("title")}
        </h2>
        <p className="text-caption text-muted-foreground">{w("description")}</p>
      </div>
      {matrix.error && (
        <Notice variant="warning" title={w("loadFailed")}>
          {matrix.error}
        </Notice>
      )}
      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[860px] border-collapse text-caption" aria-label={w("title")}>
            <thead className="bg-muted/40">
              <tr className="border-b border-border">
                <th scope="col" className={TH}>{w("columns.repository")}</th>
                <th scope="col" className={TH}>{w("columns.environment")}</th>
                <th scope="col" className={TH}>{w("columns.lastRun")}</th>
                <th scope="col" className={TH}>{w("columns.commit")}</th>
                <th scope="col" className={TH}>{w("columns.trigger")}</th>
                <th scope="col" className={cn(TH, "text-right")}>{w("columns.when")}</th>
                <th scope="col" className={cn(TH, "text-right")}>
                  <span className="sr-only">{w("columns.actions")}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const run = row.cell.last_run;
                const commitUrl = githubCommitUrl(remoteOf(row.repositoryId), run?.head_sha);
                const rollbackOK = canRollBack(row.cell);
                return (
                  <tr key={row.key} className="border-b border-border/60 last:border-0">
                    <td className={cn(TD, "max-w-56 truncate font-medium")}>{row.repositoryName}</td>
                    <td className={cn(TD, "whitespace-nowrap")}>
                      {envLabel(row.env)}
                      <span className="ml-1 font-mono text-micro text-muted-foreground">({row.env})</span>
                    </td>
                    <td className={cn(TD, "whitespace-nowrap")}>
                      {run ? (
                        <span className="inline-flex items-center gap-2">
                          <WorkflowRunPill run={run} />
                          {run.html_url ? (
                            <a href={run.html_url} target="_blank" rel="noreferrer" className="font-mono text-info hover:underline">
                              #{run.run_number}
                            </a>
                          ) : (
                            <span className="font-mono text-muted-foreground">#{run.run_number}</span>
                          )}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">{w("noRun")}</span>
                      )}
                    </td>
                    <td className={cn(TD, "whitespace-nowrap font-mono")}>
                      {run?.head_sha ? (
                        commitUrl ? (
                          <a href={commitUrl} target="_blank" rel="noreferrer" className="text-info hover:underline" title={run.head_sha}>
                            {run.head_sha.slice(0, 7)}
                          </a>
                        ) : (
                          <span title={run.head_sha}>{run.head_sha.slice(0, 7)}</span>
                        )
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className={cn(TD, "whitespace-nowrap")}>
                      {run ? (
                        <>
                          <span className="block">{w(`triggers.${TRIGGERS.has(run.trigger_source) ? run.trigger_source : "external"}`)}</span>
                          {run.triggered_by && <span className="block text-micro text-muted-foreground">{w("by", { actor: run.triggered_by })}</span>}
                        </>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className={cn(TD, "text-right whitespace-nowrap")}>
                      {run ? (
                        <span title={formatDate(runStartedAt(run))}>{formatRelativeTime(runStartedAt(run), lang)}</span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className={cn(TD, "text-right")}>
                      <div className="flex justify-end gap-1.5">
                        <Button size="sm" variant="ghost" className="h-7" onClick={() => setPanel(row)} aria-label={w("runsLabel", { repository: row.repositoryName, env: envLabel(row.env) })}>
                          <History aria-hidden />
                          {w("runs")}
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7"
                          disabled={!row.cell.dispatchable || busy}
                          title={row.cell.dispatchable ? undefined : w("notDispatchable")}
                          onClick={() => ask(row, "deploy")}
                        >
                          <Rocket aria-hidden />
                          {w("deploy")}
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7 border-destructive/40 text-destructive hover:text-destructive"
                          disabled={!rollbackOK || busy}
                          title={rollbackOK ? undefined : row.cell.dispatchable ? w("noRollback") : w("notDispatchable")}
                          onClick={() => ask(row, "rollback")}
                        >
                          <Undo2 aria-hidden />
                          {w("rollback")}
                        </Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </Card>

      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={pending ? w(`${pending.action}Title`, names) : ""}
        description={
          pending?.action === "rollback"
            ? w("rollbackDescription", { sha: (pending.row.cell.rollback_sha || "").slice(0, 7) })
            : w("deployDescription")
        }
        confirmLabel={pending ? w(`${pending.action}Confirm`) : undefined}
        variant={pending?.action === "rollback" || (target && dispatchNeedsPhrase(target.env)) ? "destructive" : "default"}
        confirmPhrase={pending && (pending.action === "rollback" || dispatchNeedsPhrase(pending.row.env)) ? pending.row.repositoryName : undefined}
        loading={busy}
        error={refusal}
        errorTitle={w("refused")}
        onConfirm={act}
      >
        {pending?.action === "deploy" && (
          <div className="space-y-1">
            <Label htmlFor={refId}>{w("refLabel")}</Label>
            <Input
              id={refId}
              value={ref}
              onChange={(e) => setRef(e.target.value)}
              placeholder="main"
              autoComplete="off"
              className="font-mono"
              aria-describedby={refHintId}
            />
            <p id={refHintId} className="text-micro text-muted-foreground">
              {w("refHint")}
            </p>
          </div>
        )}
      </ConfirmDialog>

      <DetailPanel
        open={panel !== null}
        onOpenChange={(open) => !open && setPanel(null)}
        title={panel ? w("runsTitle", { repository: panel.repositoryName, env: envLabel(panel.env) }) : ""}
        description={w("description")}
      >
        {runs.error && (
          <Notice variant="error" title={w("runsFailed")}>
            {runs.error}
          </Notice>
        )}
        {runs.runs === null && !runs.error && <Skeleton className="h-40 w-full" />}
        {runs.runs !== null && runs.runs.length === 0 && <p className="text-caption text-muted-foreground">{w("runsEmpty")}</p>}
        {runs.runs !== null && runs.runs.length > 0 && panel && (
          <ul className="divide-y divide-border rounded-md border border-border text-caption" aria-label={w("runsTitle", { repository: panel.repositoryName, env: envLabel(panel.env) })}>
            {runs.runs.map((run) => {
              const commitUrl = githubCommitUrl(remoteOf(panel.repositoryId), run.head_sha);
              const duration = runDurationMs(run);
              return (
                <li key={run.id} className="space-y-1 px-3 py-2">
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                    <WorkflowRunPill run={run} />
                    {run.html_url ? (
                      <a href={run.html_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 font-mono text-info hover:underline">
                        #{run.run_number}
                        <ExternalLink className="h-3 w-3" aria-hidden />
                      </a>
                    ) : (
                      <span className="font-mono">#{run.run_number}</span>
                    )}
                    {run.head_sha &&
                      (commitUrl ? (
                        <a href={commitUrl} target="_blank" rel="noreferrer" className="font-mono text-info hover:underline" title={run.head_sha}>
                          {run.head_sha.slice(0, 7)}
                        </a>
                      ) : (
                        <span className="font-mono" title={run.head_sha}>
                          {run.head_sha.slice(0, 7)}
                        </span>
                      ))}
                    {run.head_ref && <span className="font-mono text-muted-foreground">{run.head_ref}</span>}
                    <span className="ml-auto text-muted-foreground" title={formatDate(runStartedAt(run))}>
                      {formatRelativeTime(runStartedAt(run), lang)}
                      {duration !== undefined && <span className="font-mono tabular-nums"> · {formatDuration(duration, lang)}</span>}
                    </span>
                  </div>
                  <p className="text-micro text-muted-foreground">
                    {w(`triggers.${TRIGGERS.has(run.trigger_source) ? run.trigger_source : "external"}`)}
                    {run.triggered_by && ` · ${w("by", { actor: run.triggered_by })}`}
                    {run.rollback_of_sha && ` · ${w("rollbackOf", { sha: run.rollback_of_sha.slice(0, 7) })}`}
                  </p>
                </li>
              );
            })}
          </ul>
        )}
      </DetailPanel>
    </section>
  );
}
