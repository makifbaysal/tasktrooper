import { ToolchainProgress } from "@/components/toolchain/ToolchainProgress";
import { ToolchainStepList } from "@/components/toolchain/ToolchainStepList";
import { useI18n } from "@/hooks/useI18n";
import type { ToolchainJobState } from "@/lib/desktop-bridge";
import { cn } from "@/lib/utils";

interface ToolchainJobDetailsProps {
  state: ToolchainJobState;
  /** The job recommends a restart the agents have not had yet (`useRestartPending`). */
  restartPending?: boolean;
  /** Caps the step list, e.g. inside the header's popover. */
  listClassName?: string;
  className?: string;
}

/** What the header popover and the Settings job card both show: the bar, every step, the restart hint. */
export function ToolchainJobDetails({ state, restartPending = false, listClassName, className }: ToolchainJobDetailsProps) {
  const { t } = useI18n();
  return (
    <div className={cn("space-y-3", className)}>
      <ToolchainProgress state={state} />
      <ToolchainStepList steps={state.steps} className={listClassName} />
      {restartPending && <p className="text-xs text-muted-foreground">{t("toolchain.restart.hint")}</p>}
    </div>
  );
}
