import { ArrowLeft } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryTraceView } from "@/components/operations/telemetry/TelemetryTraceView";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import type { TelemetryTrace } from "@/lib/telemetryHub";

/** `/operations/telemetry/traces/:traceId` */
export default function TelemetryTracePage() {
  const { traceId = "" } = useParams();
  const { t } = useI18n();
  const href = useOpsHref();
  useOpsPageScope({ time: false, compare: false, env: false });
  const read = useTelemetryQuery<TelemetryTrace>(`traces/${encodeURIComponent(traceId)}`, undefined, { intervalMs: 0 });
  return (
    <div className="space-y-4">
      <Button asChild variant="ghost" size="sm" className="-ml-2 w-fit">
        <Link to={href("/operations/telemetry/traces")}>
          <ArrowLeft aria-hidden />
          {t("telemetryHub.traces.back")}
        </Link>
      </Button>
      <TelemetryQueryView query={read}>{(trace) => <TelemetryTraceView trace={trace} />}</TelemetryQueryView>
    </div>
  );
}
