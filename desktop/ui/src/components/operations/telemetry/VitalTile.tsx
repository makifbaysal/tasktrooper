import { Badge } from "@/components/ui/badge";
import { StatTile } from "@/components/ui/stat-tile";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { formatMs } from "@/lib/telemetryHub";
import { vitalRating, type TelemetryVital } from "@/lib/telemetryViews";

const VARIANT = { good: "success", needsImprovement: "warning", poor: "destructive" } as const;

/** A Web Vital or start-up timing at p75, rated against its good / poor thresholds. */
export function VitalTile({ vital }: { vital: TelemetryVital }) {
  const { t, lang } = useI18n();
  const locale = intlLocale(lang);
  const rating = vitalRating(vital);
  const fmt = (n: number) => (vital.unit === "ms" ? formatMs(n, locale) : new Intl.NumberFormat(locale, { maximumFractionDigits: 3 }).format(n));
  return (
    <StatTile label={vital.label} value={fmt(vital.value)} foot={t("telemetryViews.apps.vitalThresholds", { good: fmt(vital.good), poor: fmt(vital.poor) })}>
      <Badge variant={VARIANT[rating]} className="mt-1 w-fit">
        {t(`telemetryViews.apps.ratings.${rating}`)}
      </Badge>
    </StatTile>
  );
}
