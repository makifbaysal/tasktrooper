import { ArrowLeft } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { RedCharts } from "@/components/operations/telemetry/RedCharts";
import { RcaPanel } from "@/components/operations/telemetry/RcaPanel";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { StatTile } from "@/components/ui/stat-tile";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { intlLocale } from "@/lib/languages";
import { endpointLabel, formatCount, formatMs, formatRatio, type TelemetryDeploy, type TelemetryEndpoint } from "@/lib/telemetryHub";
import { serviceHref } from "@/pages/ops/telemetry/TelemetryServicesPage";

/** `/operations/telemetry/endpoints/:service/:endpoint`: one endpoint's
 * signals and why it got slower or faster than before. */
export default function TelemetryEndpointPage() {
  const { service = "", endpoint = "" } = useParams();
  const { t, lang } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.endpoint.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const base = `services/${encodeURIComponent(service)}/endpoints/${encodeURIComponent(endpoint)}`;
  const q = { range: scope.range, env: scope.env };
  const read = useTelemetryQuery<TelemetryEndpoint>(base, q);
  const deploys = useTelemetryQuery<TelemetryDeploy[]>(`services/${encodeURIComponent(service)}/deploys`, q);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button asChild variant="ghost" size="sm">
          <Link to={href(serviceHref(service))}>
            <ArrowLeft aria-hidden />
            {h("back", { service })}
          </Link>
        </Button>
      </div>
      <TelemetryToolbar />
      <TelemetryQueryView query={read}>
        {(e) => (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="font-mono text-heading font-semibold">{endpointLabel(e)}</h2>
              <Badge variant="outline">{service}</Badge>
              {e.monitored && <Badge variant="info">{h("monitored")}</Badge>}
            </div>
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <StatTile label={h("rpm")} value={formatCount(e.rpm, locale)} />
              <StatTile label={h("errorRate")} value={formatRatio(e.errorRate, locale)} />
              <StatTile label={h("p50")} value={formatMs(e.p50, locale)} />
              <StatTile label={h("p95")} value={formatMs(e.p95, locale)} foot={h("p99", { value: formatMs(e.p99, locale) })} />
            </div>
          </div>
        )}
      </TelemetryQueryView>
      <RedCharts path={`${base}/series`} scope={scope} deploys={deploys.data} />
      <RcaPanel path={`${base}/rca`} scope={scope} />
    </div>
  );
}
