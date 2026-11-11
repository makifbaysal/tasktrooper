import { Activity, PlugZap, RefreshCw, Settings as SettingsIcon, WifiOff } from "lucide-react";
import type { ReactNode } from "react";
import { Link, Outlet } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryStatus, type TelemetryConnection } from "@/hooks/useTelemetryStatus";

export const TELEMETRY_SETTINGS_PATH = "/settings/telemetry";

interface TelemetryGuideProps {
  connection: Exclude<TelemetryConnection, "checking" | "ready">;
  error?: string;
  onRetry: () => void;
}

/** What a telemetry screen shows instead of data: where to go to connect the
 * product, or that the connection saved does not answer. */
export function TelemetryGuide({ connection, error, onRetry }: TelemetryGuideProps) {
  const { t } = useI18n();
  const g = (key: string) => t(`telemetryHub.gate.${key}`);
  const settings = (
    <Button asChild>
      <Link to={TELEMETRY_SETTINGS_PATH}>
        <SettingsIcon aria-hidden />
        {g("openSettings")}
      </Link>
    </Button>
  );

  if (connection === "notConfigured") {
    return (
      <EmptyState
        icon={PlugZap}
        title={g("notConfiguredTitle")}
        description={g("notConfiguredBody")}
        action={settings}
        className="py-20"
      />
    );
  }
  if (connection === "unreachable") {
    return (
      <EmptyState
        icon={WifiOff}
        variant="critical"
        title={g("unreachableTitle")}
        description={g("unreachableBody")}
        className="py-20"
        action={
          <div className="flex flex-wrap items-center justify-center gap-2">
            <Button variant="outline" onClick={onRetry}>
              <RefreshCw aria-hidden />
              {g("retry")}
            </Button>
            {settings}
          </div>
        }
      />
    );
  }
  return (
    <div className="mx-auto max-w-xl space-y-3 py-12">
      <Notice variant="error" title={g("failedTitle")}>
        {error ?? g("failedBody")}
      </Notice>
      <Button variant="outline" onClick={onRetry}>
        <RefreshCw aria-hidden />
        {g("retry")}
      </Button>
    </div>
  );
}

interface TelemetryGateProps {
  children?: ReactNode;
}

/** Wraps the telemetry screens: they render once the server has a telemetry
 * address and key and reaches the product, and guidance shows until then.
 * As a route element it renders its child routes. */
export function TelemetryGate({ children }: TelemetryGateProps) {
  const { connection, error, recheck } = useTelemetryStatus();
  const { t } = useI18n();
  if (connection === "checking") {
    return (
      <div className="space-y-3" role="status" aria-label={t("telemetryHub.gate.checking")}>
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-40 w-full rounded-xl" />
        <span className="sr-only">
          <Activity aria-hidden />
          {t("telemetryHub.gate.checking")}
        </span>
      </div>
    );
  }
  if (connection === "ready") return <>{children ?? <Outlet />}</>;
  return <TelemetryGuide connection={connection} error={error} onRetry={recheck} />;
}
