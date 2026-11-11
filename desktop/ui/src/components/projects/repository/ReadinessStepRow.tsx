import { CheckCircle2, Circle, CircleHelp, Lock, MinusCircle } from "lucide-react";
import type { ReadinessStatus } from "@/api";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export interface ReadinessRowAction {
  label: string;
  onClick: () => void;
  busy?: boolean;
  disabled?: boolean;
}

interface ReadinessStepRowProps {
  number: number;
  status: ReadinessStatus;
  title: string;
  why: string;
  detail?: string;
  action?: ReadinessRowAction;
  taskLink?: ReadinessRowAction;
  onSkip?: () => void;
  onUnskip?: () => void;
}

function StatusGlyph({ status }: { status: ReadinessStatus }) {
  const { t } = useI18n();
  const label = t(`readiness.status.${status}`);
  const common = { "aria-label": label, role: "img" } as const;
  switch (status) {
    case "done":
      return <CheckCircle2 className="h-5 w-5 text-success" {...common} />;
    case "running":
      return (
        <span {...common}>
          <Spinner size="md" className="h-5 w-5 text-primary" />
        </span>
      );
    case "blocked":
      return <Lock className="h-5 w-5 text-muted-foreground" {...common} />;
    case "skipped":
      return <MinusCircle className="h-5 w-5 text-muted-foreground" {...common} />;
    case "unknown":
      return <CircleHelp className="h-5 w-5 text-muted-foreground" {...common} />;
    default:
      return <Circle className="h-5 w-5 text-foreground" {...common} />;
  }
}

/** One line of the setup checklist: glyph, what and why, what it found, and what the person can do. */
export function ReadinessStepRow({ number, status, title, why, detail, action, taskLink, onSkip, onUnskip }: ReadinessStepRowProps) {
  const { t } = useI18n();
  const muted = status === "done" || status === "skipped";
  return (
    <li className="flex items-start gap-3 py-3" data-status={status}>
      <div className="mt-0.5 shrink-0">
        <StatusGlyph status={status} />
      </div>
      <div className="min-w-0 flex-1">
        <p className={cn("text-body font-medium", muted && "text-muted-foreground")}>
          <span className="mr-1.5 tabular-nums text-muted-foreground">{number}.</span>
          {title}
        </p>
        <p className="text-caption text-muted-foreground">{why}</p>
        {detail && <p className="mt-0.5 text-caption text-foreground">{detail}</p>}
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        {taskLink && (
          <Button size="sm" variant="outline" onClick={taskLink.onClick}>
            {taskLink.label}
          </Button>
        )}
        {action && (
          <Button size="sm" onClick={action.onClick} disabled={action.disabled || action.busy}>
            {action.busy && <Spinner size="sm" className="text-current" />}
            {action.label}
          </Button>
        )}
        {onSkip && (
          <Button size="sm" variant="ghost" onClick={onSkip}>
            {t("readiness.skip")}
          </Button>
        )}
        {onUnskip && (
          <Button size="sm" variant="ghost" onClick={onUnskip}>
            {t("readiness.unskip")}
          </Button>
        )}
      </div>
    </li>
  );
}
