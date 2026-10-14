import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { ToolchainStepIcon } from "@/components/toolchain/ToolchainStepList";
import { useI18n } from "@/hooks/useI18n";
import type { ToolchainItem, ToolchainJobStep } from "@/lib/desktop-bridge";
import { toolchainMethodLine } from "@/lib/toolchain";
import { stepFinished, stepSatisfied } from "@/lib/toolchainJob";

/**
 * What the shell detected, or — while the current job still has the item in
 * hand — where that job is with it, since the detection from before the job
 * says "Missing" for something that is being installed right now.
 */
export function ToolchainItemStatusBadge({ item, step }: { item: ToolchainItem; step?: ToolchainJobStep }) {
  const { t } = useI18n();
  if (step && !stepFinished(step)) {
    return (
      <Badge variant={step.state === "waiting" ? "warning" : "info"} className="gap-1 text-micro">
        <ToolchainStepIcon state={step.state} className="h-3 w-3 text-current" />
        {t(`toolchain.stepState.${step.state}`)}
      </Badge>
    );
  }
  // The job installed it after this report was taken; the next sweep agrees.
  if (item.status !== "installed" && step && stepSatisfied(step)) {
    return (
      <Badge variant="success" className="text-micro">
        {t("toolchain.itemStatus.installed")}
      </Badge>
    );
  }
  if (item.status === "installed") {
    return (
      <Badge variant="success" className="text-micro">
        {item.version
          ? t("toolchain.itemStatus.installedVersion", { version: item.version })
          : t("toolchain.itemStatus.installed")}
      </Badge>
    );
  }
  if (item.status === "unusable") {
    return (
      <Badge variant="warning" className="text-micro" title={item.detail}>
        {t("toolchain.itemStatus.unusable")}
      </Badge>
    );
  }
  return (
    <Badge variant="secondary" className="text-micro">
      {t("toolchain.itemStatus.missing")}
    </Badge>
  );
}

interface ToolchainItemRowProps {
  item: ToolchainItem;
  /** Before the label — the first-run screen's checkbox. */
  leading?: ReactNode;
  /** The id of the `leading` control, so the label toggles it. */
  controlId?: string;
  /** After the text — Install / Open page. */
  action?: ReactNode;
  /** Who needs it, as one line. */
  reason?: string;
  /** Why it cannot be installed from here yet, as one line. */
  hint?: string;
  showStatus?: boolean;
  /** The command the shell will run, for transparency. Display only. */
  showCommand?: boolean;
  step?: ToolchainJobStep;
}

/** One developer tool as a list row; the parent supplies the bordered `divide-y` container. */
export function ToolchainItemRow({
  item,
  leading,
  controlId,
  action,
  reason,
  hint,
  showStatus = false,
  showCommand = false,
  step,
}: ToolchainItemRowProps) {
  const { t } = useI18n();
  const method = toolchainMethodLine(t, item);
  const detail = item.status !== "installed" ? item.detail : undefined;

  return (
    <div className="flex min-w-0 items-start gap-3 px-4 py-3" data-testid={`toolchain-item-${item.id}`}>
      {leading && <div className="mt-0.5 shrink-0">{leading}</div>}
      <div className="min-w-0 flex-1 space-y-0.5">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          {controlId ? (
            <Label htmlFor={controlId} className="cursor-pointer">
              {item.label}
            </Label>
          ) : (
            <span className="text-body font-medium">{item.label}</span>
          )}
          {showStatus && <ToolchainItemStatusBadge item={item} step={step} />}
        </div>
        {reason && <p className="text-xs text-muted-foreground">{reason}</p>}
        {method && <p className="text-xs text-muted-foreground">{method}</p>}
        {detail && <p className="break-words text-xs text-muted-foreground">{detail}</p>}
        {hint && <p className="text-xs text-foreground">{hint}</p>}
        {showCommand && item.command && (
          <p className="truncate font-mono text-micro text-muted-foreground" title={item.command}>
            {item.command}
          </p>
        )}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}
