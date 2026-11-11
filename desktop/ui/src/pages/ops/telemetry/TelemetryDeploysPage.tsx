import { DeploysTable } from "@/components/operations/telemetry/DeploysTable";
import { ServicePicker } from "@/components/operations/telemetry/ServicePicker";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import type { TelemetryDeploy } from "@/lib/telemetryHub";

/** `/operations/telemetry/deploys`: versions the product saw go live, from
 * spans and from CI or platform markers. The app's own releases and rollouts
 * stay under Changes. */
export default function TelemetryDeploysPage() {
  const { t } = useI18n();
  const h = (key: string) => t(`telemetryHub.deploys.${key}`);
  const scope = useTelemetryScope();
  const [service, setService] = useUrlParam("service");
  const read = useTelemetryQuery<TelemetryDeploy[]>("deploys", { range: scope.range, env: scope.env, service: service || undefined });
  return (
    <div className="space-y-4">
      <TelemetryToolbar>
        <ServicePicker value={service} onChange={setService} label={h("service")} />
      </TelemetryToolbar>
      <TelemetryQueryView query={read} isEmpty={(d) => d.length === 0} emptyTitle={h("empty")} emptyDescription={h("emptyHint")}>
        {(rows) => <DeploysTable deploys={rows} />}
      </TelemetryQueryView>
    </div>
  );
}
