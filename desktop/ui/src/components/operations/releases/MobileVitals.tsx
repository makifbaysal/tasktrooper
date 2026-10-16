import { Link } from "react-router-dom";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { ROLE_VIEWS, type SignalSpec } from "@/components/operations/console/roleViews";
import { SignalTile } from "@/components/operations/console/SignalTile";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { useI18n } from "@/hooks/useI18n";

const ROLLOUT_SIGNAL = "mobile.rollout_fraction";

// The rollout share is read from the store itself on this page, so only the
// crash and launch signals wait on a data source.
export const MOBILE_VITALS: SignalSpec[] = ROLE_VIEWS.mobile.charts.filter((s) => s.id !== ROLLOUT_SIGNAL);

/** Crash and launch health per release; the slots stay until a data source sends them. */
export function MobileVitals() {
  const { t } = useI18n();
  const href = useOpsHref();
  return (
    <div className="grid grid-cols-1 gap-2 @xl:grid-cols-3">
      {MOBILE_VITALS.map((spec) => (
        <SignalTile
          key={spec.id}
          compact
          label={t(`operations.signals.${spec.label}`)}
          unit={spec.unit ? t(`operations.units.${spec.unit}`) : undefined}
          title={t("operations.console.noMetricsSource")}
          value={<span className="font-mono text-muted-foreground">—</span>}
          foot={
            <span className="flex flex-wrap items-baseline gap-x-2">
              <span>{t("operations.console.noMetricsSource")}</span>
              <Link to={href(DATA_SOURCES_PATH)} className="text-info hover:underline">
                {t("operations.console.connectSource")}
              </Link>
            </span>
          }
        />
      ))}
    </div>
  );
}
