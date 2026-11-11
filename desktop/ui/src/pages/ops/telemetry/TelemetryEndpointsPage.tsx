import { useEffect } from "react";
import { EndpointsTable } from "@/components/operations/telemetry/EndpointsTable";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import type { TelemetryEndpoint, TelemetryService } from "@/lib/telemetryHub";

/** `/operations/telemetry/endpoints`: the endpoints of one service at a time;
 * the product keeps them per service. */
export default function TelemetryEndpointsPage() {
  const { t } = useI18n();
  const h = (key: string) => t(`telemetryHub.endpoints.${key}`);
  const scope = useTelemetryScope();
  const [picked, setService] = useUrlParam("service");
  const q = { range: scope.range, env: scope.env };
  const services = useTelemetryQuery<TelemetryService[]>("services", q, { intervalMs: 0 });
  const names = (services.data ?? []).filter((s) => s.type !== "web" && s.type !== "mobile").map((s) => s.name).sort();
  const service = picked && names.includes(picked) ? picked : names[0];

  useEffect(() => {
    if (!picked && service) setService(service);
  }, [picked, service, setService]);

  const endpoints = useTelemetryQuery<TelemetryEndpoint[]>(`services/${encodeURIComponent(service ?? "")}/endpoints`, q, { enabled: Boolean(service) });

  return (
    <div className="space-y-4">
      <TelemetryToolbar>
        <Select value={service ?? ""} onValueChange={setService} disabled={names.length === 0}>
          <SelectTrigger className="h-8 w-64" aria-label={h("service")}>
            <SelectValue placeholder={h("pickService")} />
          </SelectTrigger>
          <SelectContent>
            {names.map((n) => (
              <SelectItem key={n} value={n}>
                {n}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </TelemetryToolbar>
      {services.data !== undefined && names.length === 0 ? (
        <p className="py-8 text-center text-caption text-muted-foreground">{h("noServices")}</p>
      ) : service ? (
        <TelemetryQueryView query={endpoints} isEmpty={(d) => d.length === 0} emptyTitle={h("empty")}>
          {(rows) => <EndpointsTable service={service} endpoints={rows} />}
        </TelemetryQueryView>
      ) : (
        <TelemetryQueryView query={services}>{() => null}</TelemetryQueryView>
      )}
    </div>
  );
}
