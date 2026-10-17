import { Siren } from "lucide-react";
import { Link } from "react-router-dom";
import { HealthSquare } from "@/components/operations/console/HealthSquare";
import { OPS_STATUSES, type OpsStatus } from "@/components/operations/console/opsStatus";
import { ALL } from "@/components/operations/console/opsEntities";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface HealthSummaryStripProps {
  counts: Record<OpsStatus, number>;
  active: OpsStatus | typeof ALL;
  onSelect: (status: OpsStatus | typeof ALL) => void;
  openIssues: number;
  issuesHref: string;
  environmentLabel: string;
}

/** The explorer's health facets — each a filter with its count — and the
 * open issues behind them, in the environment picked above. */
export function HealthSummaryStrip({ counts, active, onSelect, openIssues, issuesHref, environmentLabel }: HealthSummaryStripProps) {
  const { t } = useI18n();
  return (
    <div className="grid grid-cols-2 gap-2 @2xl:grid-cols-5" role="group" aria-label={t("operations.explorer.healthFilter")}>
      {OPS_STATUSES.map((status) => {
        const pressed = active === status;
        return (
          <Button
            key={status}
            variant="outline"
            aria-pressed={pressed}
            onClick={() => onSelect(pressed ? ALL : status)}
            className={cn(
              "h-auto min-w-0 justify-start gap-3 bg-card px-3 py-2.5 text-left whitespace-normal shadow-[var(--shadow-raised)]",
              pressed && "border-ring bg-accent ring-1 ring-ring",
            )}
          >
            <HealthSquare status={status} size="lg" decorative />
            <span className="flex min-w-0 flex-col">
              <span className="text-caption leading-tight font-normal text-muted-foreground">{t(`operations.status.${status}`)}</span>
              <span className="font-mono text-title font-semibold tabular-nums">{counts[status]}</span>
            </span>
          </Button>
        );
      })}
      <Button variant="outline" asChild className="col-span-2 h-auto min-w-0 justify-start gap-3 bg-card px-3 py-2.5 text-left whitespace-normal shadow-[var(--shadow-raised)] @2xl:col-span-1">
        <Link to={issuesHref} aria-label={t("operations.explorer.openIssuesLabel", { count: openIssues, env: environmentLabel })}>
          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
            <Siren className="h-4 w-4" aria-hidden />
          </span>
          <span className="flex min-w-0 flex-col">
            <span className="text-caption leading-tight font-normal text-muted-foreground">{t("operations.explorer.openIssues")}</span>
            <span className="font-mono text-title font-semibold tabular-nums">{openIssues}</span>
          </span>
        </Link>
      </Button>
    </div>
  );
}
