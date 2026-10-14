import { Progress } from "@/components/ui/progress";
import { useI18n } from "@/hooks/useI18n";
import type { ToolchainJobState } from "@/lib/desktop-bridge";
import { jobSummary } from "@/lib/toolchainJob";
import { cn } from "@/lib/utils";

/** The job's bar, its percent (announced politely) and how many steps have finished. */
export function ToolchainProgress({ state, className }: { state: ToolchainJobState; className?: string }) {
  const { t } = useI18n();
  const summary = jobSummary(state);
  const percent = Math.round(Math.min(100, Math.max(0, state.percent)));

  return (
    <div className={cn("space-y-1.5", className)}>
      <Progress value={percent} />
      <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
        <span aria-live="polite" className="font-mono font-medium tabular-nums text-foreground">
          {t("toolchain.progress.percent", { percent })}
        </span>
        <span>
          {t("toolchain.progress.summary", { done: summary.total - summary.remaining, total: summary.total })}
          {summary.anyFailed && (
            <span className="text-destructive"> · {t("toolchain.progress.failed", { count: summary.failed })}</span>
          )}
        </span>
      </div>
    </div>
  );
}
