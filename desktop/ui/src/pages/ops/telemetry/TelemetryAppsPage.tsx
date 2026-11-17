import { useMemo } from "react";
import { Link } from "react-router-dom";
import { Sparkline } from "@/components/operations/charts/Sparkline";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { HealthMark } from "@/components/operations/telemetry/HealthMark";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatRatio, sparkPoints, type TelemetryService } from "@/lib/telemetryHub";

export const CLIENT_TYPES = ["web", "mobile"];

export function appHref(name: string): string {
  return `/operations/telemetry/apps/${encodeURIComponent(name)}`;
}

/** `/operations/telemetry/apps`: the web and mobile apps reporting through the
 * browser, React Native or Flutter SDKs. */
export default function TelemetryAppsPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.apps.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const read = useTelemetryQuery<TelemetryService[]>("services", { range: scope.range, env: scope.env });
  const apps = useMemo(() => (read.data ?? []).filter((s) => CLIENT_TYPES.includes(s.type)).sort((a, b) => b.rpm - a.rpm || a.name.localeCompare(b.name)), [read.data]);

  return (
    <div className="space-y-4">
      <TelemetryToolbar />
      <TelemetryQueryView query={read} isEmpty={() => apps.length === 0} emptyTitle={v("empty")} emptyDescription={v("emptyHint")}>
        {() => (
          <TelemetryTable
            label={v("listLabel")}
            columns={[
              { key: "health", label: v("columns.health"), className: "w-12" },
              { key: "name", label: v("columns.app") },
              { key: "type", label: v("columns.platform") },
              { key: "views", label: v("columns.views"), numeric: true },
              { key: "trend", label: v("columns.trend") },
              { key: "err", label: v("columns.errorRate"), numeric: true },
              { key: "version", label: v("columns.version") },
            ]}
          >
            {apps.map((a) => (
              <TelemetryRow key={a.id}>
                <TelemetryCell>
                  <HealthMark health={a.status} />
                </TelemetryCell>
                <TelemetryCell>
                  <Link to={href(appHref(a.name))} className="font-medium hover:underline">
                    {a.name}
                  </Link>
                </TelemetryCell>
                <TelemetryCell>
                  <Badge variant="secondary">{v(`platforms.${a.type}`)}</Badge>
                </TelemetryCell>
                <TelemetryCell numeric>{formatCount(a.rpm, locale)}</TelemetryCell>
                <TelemetryCell>
                  <Sparkline points={sparkPoints(a.spark)} ariaLabel={v("trendLabel", { name: a.name })} />
                </TelemetryCell>
                <TelemetryCell numeric>{formatRatio(a.errorRate, locale)}</TelemetryCell>
                <TelemetryCell className="font-mono text-micro text-muted-foreground">{a.version || "-"}</TelemetryCell>
              </TelemetryRow>
            ))}
          </TelemetryTable>
        )}
      </TelemetryQueryView>
    </div>
  );
}
