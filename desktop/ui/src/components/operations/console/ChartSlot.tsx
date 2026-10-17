import { CloudOff, Database, Ellipsis, Info, Link2, SlidersHorizontal } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { TimeSeriesChart, type ChartSeries, type DeployMarker } from "@/components/operations/charts/TimeSeriesChart";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";

export const DATA_SOURCES_PATH = "/operations/sources";

const NO_SERIES: ChartSeries[] = [];

/** Where the slot's signal stands: fed by a binding, waiting for one, a read
 * that failed, or fed by a source that reads only the value as it is now
 * (`live`: no history to draw). Left out, an empty slot reads as "no data
 * source bound". */
export type ChartSlotStatus = "bound" | "unbound" | "error" | "live";

interface ChartSlotProps {
  title: string;
  unit?: string;
  series?: ChartSeries[];
  syncKey?: string;
  loading?: boolean;
  deployMarkers?: DeployMarker[];
  xRange?: [number, number];
  height?: number;
  /** Extra items for the card's "…" menu. */
  menuItems?: ReactNode;
  /** Formats a value with its unit; the chart then shows no unit of its own. */
  formatValue?: (v: number) => string;
  status?: ChartSlotStatus;
  error?: string;
  /** The failed state's heading; "Couldn't read this signal" by default. */
  errorTitle?: string;
  /** An action of the failed state's own (e.g. "Fill in parameters"), beside or instead of Retry. */
  errorAction?: ReactNode;
  onRetry?: () => void;
  onBind?: () => void;
  /** The entity's Signals page. */
  manageHref?: string;
  /** A small line under the chart that does not replace it (e.g. the previous period could not be read). */
  note?: ReactNode;
  /** What stands in for the chart while `status` is "live" (the values read now). */
  live?: ReactNode;
}

/** One metric chart card: a title row with its "…" menu, then the chart.
 * Until a data source feeds the signal it is the chart's empty state plus
 * the way to bind one. */
export function ChartSlot({
  title,
  unit,
  series = NO_SERIES,
  syncKey,
  loading = false,
  deployMarkers,
  xRange,
  height = 180,
  menuItems,
  formatValue,
  status,
  error,
  errorTitle,
  errorAction,
  onRetry,
  onBind,
  manageHref,
  note,
  live,
}: ChartSlotProps) {
  const { t } = useI18n();
  const href = useOpsHref();
  const empty = series.length === 0;
  const sourcesHref = href(DATA_SOURCES_PATH);
  const unbound = status === "unbound" || (status === undefined && empty);

  let body: ReactNode;
  if (status === "error" && !loading) {
    body = (
      <div style={{ height }} className="flex items-center justify-center rounded-lg border border-dashed border-border">
        <EmptyState
          icon={CloudOff}
          variant="critical"
          title={errorTitle ?? t("operations.chart.loadFailed")}
          description={error}
          className="py-0"
          action={
            (onRetry || errorAction) && (
              <div className="flex flex-wrap justify-center gap-2">
                {errorAction}
                {onRetry && (
                  <Button variant="outline" size="sm" onClick={onRetry}>
                    {t("operations.chart.retry")}
                  </Button>
                )}
              </div>
            )
          }
        />
      </div>
    );
  } else if (status === "live" && !loading) {
    body = (
      <div style={{ minHeight: height }} className="flex flex-col justify-center">
        {live}
      </div>
    );
  } else {
    body = (
      <TimeSeriesChart
        series={unbound ? NO_SERIES : series}
        ariaLabel={title}
        unit={formatValue ? undefined : unit}
        formatValue={formatValue}
        syncKey={syncKey}
        height={height}
        loading={loading}
        deployMarkers={deployMarkers}
        xRange={xRange}
        showTableToggle
        emptyLabel={unbound ? t("operations.console.noMetricsSource") : undefined}
      />
    );
  }

  return (
    <Card className="flex min-w-0 flex-col gap-2 p-3">
      <div className="flex min-h-7 items-center justify-between gap-2">
        <h3 className="min-w-0 truncate text-caption font-semibold">
          {title}
          {unit && !formatValue && <span className="font-normal text-muted-foreground"> ({unit})</span>}
        </h3>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" aria-label={t("operations.chart.menu", { title })}>
              <Ellipsis aria-hidden />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="min-w-[12rem]">
            {menuItems}
            {manageHref && (
              <DropdownMenuItem asChild>
                <Link to={manageHref}>
                  <SlidersHorizontal className="mr-2 h-3.5 w-3.5" aria-hidden />
                  {t("operations.console.manageSignals")}
                </Link>
              </DropdownMenuItem>
            )}
            <DropdownMenuItem asChild>
              <Link to={sourcesHref}>
                <Database className="mr-2 h-3.5 w-3.5" aria-hidden />
                {t("operations.console.connectSource")}
              </Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {body}
      {note && (
        <p className="flex items-start gap-1.5 text-micro text-muted-foreground">
          <Info className="mt-px h-3 w-3 shrink-0" aria-hidden />
          <span className="min-w-0">{note}</span>
        </p>
      )}
      {unbound && !loading && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          {onBind ? (
            <Button variant="outline" size="sm" className="h-7" onClick={onBind} aria-label={t("operations.console.bindLabel", { title })}>
              <Link2 aria-hidden />
              {t("operations.console.bind")}
            </Button>
          ) : (
            <Link to={sourcesHref} className="inline-flex w-fit items-center gap-1.5 text-micro font-medium text-info hover:underline">
              <Database className="h-3 w-3" aria-hidden />
              {t("operations.console.connectSource")}
            </Link>
          )}
          {manageHref && (
            <Link to={manageHref} className="inline-flex w-fit items-center gap-1.5 text-micro font-medium text-info hover:underline">
              <SlidersHorizontal className="h-3 w-3" aria-hidden />
              {t("operations.console.manageSignals")}
            </Link>
          )}
        </div>
      )}
    </Card>
  );
}
