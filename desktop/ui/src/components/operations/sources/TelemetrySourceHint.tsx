import { Settings } from "lucide-react";
import { useEffect } from "react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryStatus } from "@/hooks/useTelemetryStatus";

export const TELEMETRY_SOURCE_KIND = "tasktrooper_telemetry";

/** The add form of the TaskTrooper Telemetry kind: it has no fields, because it
 * reads the connection saved in Settings. Tells the form whether that
 * connection exists (false only once the server said it does not). */
export function TelemetrySourceHint({ onConfigured, onNavigate }: { onConfigured: (configured: boolean) => void; onNavigate?: () => void }) {
  const { t } = useI18n();
  const { connection } = useTelemetryStatus();
  const missing = connection === "notConfigured";
  useEffect(() => onConfigured(!missing), [missing, onConfigured]);
  return (
    <Notice variant={missing ? "warning" : "info"} title={t("operations.sources.kinds.tasktrooper_telemetry.label")}>
      <p>{missing ? t("operations.sources.form.telemetryNotConfigured") : t("operations.sources.form.telemetryHint")}</p>
      <Button asChild variant="outline" size="sm" className="mt-2">
        <Link to="/settings/telemetry" onClick={onNavigate}>
          <Settings aria-hidden />
          {t("operations.sources.form.openTelemetrySettings")}
        </Link>
      </Button>
    </Notice>
  );
}
