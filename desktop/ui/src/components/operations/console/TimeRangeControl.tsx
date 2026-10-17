import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";

export type TimeRange = "15m" | "1h" | "6h" | "24h" | "7d";

export const TIME_RANGES: TimeRange[] = ["15m", "1h", "6h", "24h", "7d"];

export const DEFAULT_TIME_RANGE: TimeRange = "24h";

export const TIME_RANGE_MS: Record<TimeRange, number> = {
  "15m": 15 * 60_000,
  "1h": 3_600_000,
  "6h": 6 * 3_600_000,
  "24h": 24 * 3_600_000,
  "7d": 7 * 24 * 3_600_000,
};

export function parseTimeRange(value: string | null | undefined): TimeRange {
  return TIME_RANGES.find((r) => r === value) ?? DEFAULT_TIME_RANGE;
}

export function TimeRangeControl({ value, onChange }: { value: TimeRange; onChange: (range: TimeRange) => void }) {
  const { t } = useI18n();
  return (
    <Tabs value={value} onValueChange={(v) => onChange(parseTimeRange(v))} variant="pill" className="w-fit">
      <TabsList aria-label={t("operations.timeRange.label")}>
        {TIME_RANGES.map((r) => (
          <TabsTrigger key={r} value={r}>
            {t(`operations.timeRange.${r}`)}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}
