import { ChevronDown, ChevronUp, ListChecks } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { api, type ReadinessStep, type ReadinessStepId } from "@/api";
import { ReadinessStepRow, type ReadinessRowAction } from "@/components/projects/repository/ReadinessStepRow";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useRepositoryReadiness } from "@/hooks/useRepositoryReadiness";
import { useToolchain } from "@/hooks/useToolchain";
import { logEvent } from "@/lib/analytics";
import { factList, factNumber, destinationFor, resolveReadiness } from "@/lib/readiness";
import { TOOLCHAIN_SETTINGS_PATH } from "@/lib/toolchainJob";

type RepositoryTabTarget = "deploy" | "findings" | "design";

interface ReadinessCardProps {
  repositoryId: string;
  /** Open the card even if the person left it folded: the page was reached right after an import. */
  expandOnArrival?: boolean;
  onOpenTab: (tab: RepositoryTabTarget) => void;
}

const FOLD_KEY = "tt.readiness.folded.";

function readFolded(repositoryId: string): boolean {
  try {
    return window.localStorage.getItem(FOLD_KEY + repositoryId) === "1";
  } catch {
    return false;
  }
}

function writeFolded(repositoryId: string, folded: boolean): void {
  try {
    if (folded) window.localStorage.setItem(FOLD_KEY + repositoryId, "1");
    else window.localStorage.removeItem(FOLD_KEY + repositoryId);
  } catch {
    // The fold is a convenience; it never blocks the checklist.
  }
}

/**
 * "Get this project ready": what a freshly imported repository is missing,
 * in the order that makes sense, with a button for each step and one for all
 * of them. Statuses come from the server (derived from live data); the
 * developer-tools step is settled here from the desktop shell's report.
 */
export function ReadinessCard({ repositoryId, expandOnArrival = false, onOpenTab }: ReadinessCardProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { readiness, loading, reload, replace } = useRepositoryReadiness(repositoryId);
  const toolchain = useToolchain({ report: true });
  const [folded, setFolded] = useState(() => !expandOnArrival && readFolded(repositoryId));
  const [busy, setBusy] = useState<ReadinessStepId | null>(null);
  const [confirmAll, setConfirmAll] = useState(false);
  const [runningAll, setRunningAll] = useState(false);

  const resolved = useMemo(
    () => (readiness ? resolveReadiness(readiness, toolchain.report, toolchain.available) : null),
    [readiness, toolchain.report, toolchain.available],
  );

  if (loading && !readiness) return <Skeleton className="h-24 w-full" />;
  if (!readiness || !resolved || resolved.steps.length === 0) return null;

  const total = resolved.total;
  const plan = readiness.run_all;
  const failed = (e: unknown) => toast.error(e instanceof Error ? e.message : t("readiness.actionFailed"));
  const toggleFold = () => {
    writeFolded(repositoryId, !folded);
    setFolded(!folded);
  };

  const setDismissed = async (dismissed: boolean) => {
    logEvent("readiness_action", { action: dismissed ? "hide" : "show" });
    try {
      replace(await api.setReadinessDismissed(repositoryId, dismissed));
    } catch (e) {
      failed(e);
    }
  };

  if (readiness.dismissed) {
    return (
      <Card className="flex items-center justify-between gap-3 px-4 py-2.5">
        <div className="flex items-center gap-2 text-caption text-muted-foreground">
          <ListChecks className="h-4 w-4" aria-hidden />
          <span>
            {t("readiness.title")} · {t("readiness.progress", { done: resolved.done, total })}
          </span>
        </div>
        <Button size="sm" variant="ghost" onClick={() => void setDismissed(false)}>
          {t("readiness.show")}
        </Button>
      </Card>
    );
  }

  const runStep = async (step: ReadinessStep) => {
    setBusy(step.id);
    logEvent("readiness_action", { action: "run_step", step: step.id });
    try {
      const res = await api.runReadinessStep(repositoryId, step.id, true);
      toast.success(res.task ? t("readiness.taskOpened", { key: res.task.key }) : t("readiness.started"));
      await reload();
    } catch (e) {
      failed(e);
      await reload();
    } finally {
      setBusy(null);
    }
  };

  const setSkipped = async (step: ReadinessStep, skipped: boolean) => {
    logEvent("readiness_action", { action: skipped ? "skip" : "unskip", step: step.id });
    try {
      replace(await api.setReadinessStepSkipped(repositoryId, step.id, skipped));
    } catch (e) {
      failed(e);
    }
  };

  const runAll = async () => {
    setRunningAll(true);
    logEvent("readiness_action", { action: "run_all" });
    try {
      const res = await api.runReadinessAll(repositoryId, plan.tasks);
      const opened = res.results.filter((r) => r.task).length;
      toast.success(t("readiness.allStarted", { tasks: opened, runs: res.results.length - opened }));
      setConfirmAll(false);
      await reload();
    } catch (e) {
      failed(e);
      setConfirmAll(false);
      await reload();
    } finally {
      setRunningAll(false);
    }
  };

  const actionFor = (step: ReadinessStep): ReadinessRowAction | undefined => {
    const startable = step.status === "todo" || step.status === "unknown";
    if (!startable) return undefined;
    if (step.action === "run" || step.action === "task") {
      return { label: t("readiness.doIt"), onClick: () => void runStep(step), busy: busy === step.id };
    }
    if (step.action === "navigate") {
      const dest = destinationFor(step);
      if (!dest) return undefined;
      return {
        label: t("readiness.open"),
        onClick: () => (dest.kind === "tab" ? onOpenTab(dest.tab) : navigate(dest.path)),
      };
    }
    if (step.action === "desktop" && step.status === "todo") {
      if (resolved.toolchain.installable.length > 0) {
        return {
          label: t("readiness.install"),
          onClick: () => {
            logEvent("readiness_action", { action: "run_step", step: step.id });
            void toolchain.install(resolved.toolchain.installable).catch(failed);
          },
        };
      }
      return { label: t("readiness.open"), onClick: () => navigate(TOOLCHAIN_SETTINGS_PATH) };
    }
    return undefined;
  };

  const detailFor = (step: ReadinessStep): string => {
    if (step.status === "blocked" && step.reason?.startsWith("needs_")) {
      return t("readiness.waitingFor", { step: t(`readiness.steps.${step.reason.slice(6)}.title`) });
    }
    if (step.task && step.status === "running") return t("readiness.taskOpen", { key: step.task.key });
    if (step.status === "skipped") return t("readiness.skippedNote");
    const key = `readiness.detail.${step.id}.${step.reason || step.status}`;
    const text = t(key, {
      count: factNumber(step, "count"),
      high: factNumber(step, "high"),
      components: factList(step, "components").join(", "),
      languages: factList(step, "languages").join(", "),
      tools: factList(step, "tools").join(", "),
      missing: factList(step, "missing").map((m) => t(`readiness.missing.${m}`)).join(", "),
      files: factNumber(step, "files"),
      processed: factNumber(step, "processed"),
      total: factNumber(step, "total"),
      checks: factNumber(step, "checks"),
      cmp: factNumber(step, "components"),
    });
    return text === key ? "" : text;
  };

  const canSkip = (step: ReadinessStep) => step.status === "todo" || step.status === "blocked" || step.status === "unknown";

  return (
    <Card className="p-4" data-testid="readiness-card">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <h2 className="flex items-center gap-2 font-semibold">
            <ListChecks className="h-4 w-4 text-primary" aria-hidden />
            {t("readiness.title")}
          </h2>
          <p className="text-caption text-muted-foreground">
            {resolved.complete
              ? t("readiness.complete")
              : `${t("readiness.progress", { done: resolved.done, total })}${
                  resolved.skipped > 0 ? ` · ${t("readiness.skippedCount", { count: resolved.skipped })}` : ""
                }`}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {!resolved.complete && plan.tasks + plan.runs > 0 && (
            <Button size="sm" onClick={() => setConfirmAll(true)} disabled={runningAll}>
              {t("readiness.runAll")}
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={() => void setDismissed(true)}>
            {t("readiness.hide")}
          </Button>
          <Button size="icon" variant="ghost" onClick={toggleFold} aria-label={folded ? t("readiness.expand") : t("readiness.collapse")} aria-expanded={!folded}>
            {folded ? <ChevronDown className="h-4 w-4" /> : <ChevronUp className="h-4 w-4" />}
          </Button>
        </div>
      </div>
      <Progress
        className="mt-3"
        value={((resolved.done + resolved.skipped) / total) * 100}
        tone={resolved.complete ? "success" : "default"}
        label={t("readiness.progress", { done: resolved.done, total })}
      />

      {!folded && (
        <>
          <p className="mt-3 text-caption text-muted-foreground">{t("readiness.intro")}</p>
          <ol className="mt-1 divide-y divide-border">
            {resolved.steps.map((step, index) => (
              <ReadinessStepRow
                key={step.id}
                number={index + 1}
                status={step.status}
                title={t(`readiness.steps.${step.id}.title`)}
                why={t(`readiness.steps.${step.id}.why`)}
                detail={detailFor(step)}
                action={actionFor(step)}
                taskLink={
                  step.task
                    ? {
                        label: t("readiness.viewTask", { key: step.task.key }),
                        onClick: () => navigate(`/board?task=${encodeURIComponent(step.task!.id)}`),
                      }
                    : undefined
                }
                onSkip={canSkip(step) ? () => void setSkipped(step, true) : undefined}
                onUnskip={step.status === "skipped" ? () => void setSkipped(step, false) : undefined}
              />
            ))}
          </ol>
        </>
      )}

      <ConfirmDialog
        open={confirmAll}
        onOpenChange={setConfirmAll}
        title={t("readiness.runAllTitle")}
        description={[
          plan.tasks > 0 ? t("readiness.runAllTasks", { count: plan.tasks }) : "",
          plan.runs > 0 ? t("readiness.runAllRuns", { count: plan.runs }) : "",
          plan.tasks > 0 ? t("readiness.runAllCost") : "",
        ]
          .filter(Boolean)
          .join(" ")}
        confirmLabel={t("readiness.runAllConfirm")}
        loading={runningAll}
        onConfirm={() => runAll()}
      />
    </Card>
  );
}
