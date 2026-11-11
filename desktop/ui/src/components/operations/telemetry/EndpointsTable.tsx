import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { endpointLabel, formatCount, formatMs, formatRatio, type TelemetryEndpoint } from "@/lib/telemetryHub";

export function endpointHref(service: string, endpointId: string): string {
  return `/operations/telemetry/endpoints/${encodeURIComponent(service)}/${encodeURIComponent(endpointId)}`;
}

interface EndpointsTableProps {
  service: string;
  endpoints: TelemetryEndpoint[];
}

/** A service's endpoints (or consumer processes), slowest first. */
export function EndpointsTable({ service, endpoints }: EndpointsTableProps) {
  const { t, lang } = useI18n();
  const h = (key: string) => t(`telemetryHub.endpoints.${key}`);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const rows = [...endpoints].sort((a, b) => b.p95 - a.p95);
  return (
    <TelemetryTable
      label={h("listLabel")}
      columns={[
        { key: "endpoint", label: h("columns.endpoint") },
        { key: "rpm", label: h("columns.rpm"), numeric: true },
        { key: "err", label: h("columns.errorRate"), numeric: true },
        { key: "p50", label: h("columns.p50"), numeric: true },
        { key: "p95", label: h("columns.p95"), numeric: true },
        { key: "p99", label: h("columns.p99"), numeric: true },
      ]}
    >
      {rows.map((e) => (
        <TelemetryRow key={e.id}>
          <TelemetryCell>
            <Link to={href(endpointHref(service, e.id))} className="font-mono hover:underline">
              {endpointLabel(e)}
            </Link>
            {e.monitored && (
              <Badge variant="info" className="ml-2">
                {h("monitored")}
              </Badge>
            )}
          </TelemetryCell>
          <TelemetryCell numeric>{formatCount(e.rpm, locale)}</TelemetryCell>
          <TelemetryCell numeric>{formatRatio(e.errorRate, locale)}</TelemetryCell>
          <TelemetryCell numeric>{formatMs(e.p50, locale)}</TelemetryCell>
          <TelemetryCell numeric>{formatMs(e.p95, locale)}</TelemetryCell>
          <TelemetryCell numeric>{formatMs(e.p99, locale)}</TelemetryCell>
        </TelemetryRow>
      ))}
    </TelemetryTable>
  );
}
