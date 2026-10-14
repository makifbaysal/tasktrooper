import {
  Ban,
  CheckCircle2,
  Circle,
  Hourglass,
  Loader2,
  MinusCircle,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import { useI18n } from "@/hooks/useI18n";
import type { ToolchainJobStep, ToolchainStepState } from "@/lib/desktop-bridge";
import { cn } from "@/lib/utils";

const STEP_ICON: Record<ToolchainStepState, { icon: LucideIcon; className: string }> = {
  queued: { icon: Circle, className: "text-muted-foreground" },
  running: { icon: Loader2, className: "animate-spin text-info" },
  waiting: { icon: Hourglass, className: "text-warning" },
  done: { icon: CheckCircle2, className: "text-success" },
  failed: { icon: XCircle, className: "text-destructive" },
  skipped: { icon: MinusCircle, className: "text-muted-foreground" },
  cancelled: { icon: Ban, className: "text-muted-foreground" },
};

export function ToolchainStepIcon({ state, className }: { state: ToolchainStepState; className?: string }) {
  const { icon: Icon, className: tone } = STEP_ICON[state];
  return <Icon aria-hidden className={cn("h-4 w-4 shrink-0", tone, className)} />;
}

/** One install step: its state, and the shell's one line on what is happening or what went wrong. */
export function ToolchainStepRow({ step }: { step: ToolchainJobStep }) {
  const { t } = useI18n();
  const problem = step.state === "failed" || step.state === "skipped" || step.state === "cancelled";
  const line = problem ? (step.error ?? step.detail) : step.detail;
  const stepPercent = step.state === "running" && step.percent !== undefined ? Math.round(step.percent) : null;

  return (
    <li className="flex min-w-0 gap-2.5 py-2" data-testid={`toolchain-step-${step.id}`}>
      <ToolchainStepIcon state={step.state} className="mt-0.5" />
      <div className="min-w-0 flex-1 space-y-0.5">
        <div className="flex min-w-0 items-baseline justify-between gap-2">
          <span className="truncate text-sm font-medium">{step.label}</span>
          <span className="shrink-0 text-xs text-muted-foreground">
            {t(`toolchain.stepState.${step.state}`)}
            {stepPercent !== null && <span className="font-mono tabular-nums"> · {stepPercent}%</span>}
          </span>
        </div>
        {line && (
          <p
            className={cn("line-clamp-2 break-words text-xs", step.state === "failed" ? "text-destructive" : "text-muted-foreground")}
            title={line}
          >
            {line}
          </p>
        )}
        {step.state === "waiting" && <p className="text-xs text-foreground">{t("toolchain.waitingHint")}</p>}
      </div>
    </li>
  );
}

export function ToolchainStepList({ steps, className }: { steps: readonly ToolchainJobStep[]; className?: string }) {
  return (
    <ul className={cn("divide-y divide-border", className)}>
      {steps.map((step) => (
        <ToolchainStepRow key={step.id} step={step} />
      ))}
    </ul>
  );
}
