import { Database, Ellipsis } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { TimeSeriesChart, type ChartSeries, type DeployMarker } from "@/components/operations/charts/TimeSeriesChart";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useI18n } from "@/hooks/useI18n";

export const DATA_SOURCES_PATH = "/operations/sources";

const NO_SERIES: ChartSeries[] = [];

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
}

/** One metric chart card: a title row with its "…" menu, then the chart. Until
 * a data source feeds the signal it is the chart's empty state plus the way to
 * bind one. */
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
}: ChartSlotProps) {
  const { t } = useI18n();
  const href = useOpsHref();
  const empty = series.length === 0;
  const sourcesHref = href(DATA_SOURCES_PATH);
  return (
    <Card className="flex min-w-0 flex-col gap-2 p-3">
      <div className="flex min-h-7 items-center justify-between gap-2">
        <h3 className="min-w-0 truncate text-caption font-semibold">
          {title}
          {unit && <span className="font-normal text-muted-foreground"> ({unit})</span>}
        </h3>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" aria-label={t("operations.chart.menu", { title })}>
              <Ellipsis aria-hidden />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="min-w-[12rem]">
            {menuItems}
            <DropdownMenuItem asChild>
              <Link to={sourcesHref}>
                <Database className="mr-2 h-3.5 w-3.5" aria-hidden />
                {t("operations.console.connectSource")}
              </Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <TimeSeriesChart
        series={series}
        ariaLabel={title}
        unit={unit}
        syncKey={syncKey}
        height={height}
        loading={loading}
        deployMarkers={deployMarkers}
        xRange={xRange}
        showTableToggle
        emptyLabel={t("operations.console.noMetricsSource")}
      />
      {empty && !loading && (
        <Link to={sourcesHref} className="inline-flex w-fit items-center gap-1.5 text-micro font-medium text-info hover:underline">
          <Database className="h-3 w-3" aria-hidden />
          {t("operations.console.connectSource")}
        </Link>
      )}
    </Card>
  );
}
