import { AlertTriangle, CheckCircle2, Loader2, MinusCircle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import type { ToolchainJobState } from "@/lib/desktop-bridge";
import { jobOutcome, jobSummary } from "@/lib/toolchainJob";
import { cn } from "@/lib/utils";

/**
 * The job in one pill: "Setting up · 60%" while it runs; once it is done,
 * "Setup complete" only when every step left its tool installed — otherwise
 * failed, stopped or incomplete. Nothing for an idle job. The long running
 * label gives way to the bare percent on narrow screens.
 */
export function ToolchainStatusBadge({ state, className }: { state: ToolchainJobState; className?: string }) {
  const { t } = useI18n();
  const outcome = jobOutcome(state);
  const base = cn("gap-1.5 px-2 py-0.5 text-micro", className);

  switch (outcome) {
    case "idle":
      return null;
    case "running": {
      const percent = Math.round(Math.min(100, Math.max(0, state.percent)));
      return (
        <Badge variant="info" className={base}>
          <Loader2 aria-hidden className="h-3 w-3 animate-spin" />
          <span className="hidden md:inline">{t("toolchain.badge.running", { percent })}</span>
          <span className="font-mono tabular-nums md:hidden">{t("toolchain.progress.percent", { percent })}</span>
        </Badge>
      );
    }
    case "failed":
      return (
        <Badge variant="warning" className={base}>
          <AlertTriangle aria-hidden className="h-3 w-3" />
          {t("toolchain.badge.failed", { count: jobSummary(state).failed })}
        </Badge>
      );
    case "stopped":
      return (
        <Badge variant="secondary" className={base}>
          <MinusCircle aria-hidden className="h-3 w-3" />
          {t("toolchain.badge.stopped")}
        </Badge>
      );
    case "incomplete":
      return (
        <Badge variant="warning" className={base}>
          <AlertTriangle aria-hidden className="h-3 w-3" />
          {t("toolchain.badge.incomplete")}
        </Badge>
      );
    case "complete":
      return (
        <Badge variant="success" className={base}>
          <CheckCircle2 aria-hidden className="h-3 w-3" />
          {t("toolchain.badge.done")}
        </Badge>
      );
  }
}

