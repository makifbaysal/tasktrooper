import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryEnv } from "@/hooks/useTelemetryScope";
import type { TelemetryService } from "@/lib/telemetryHub";

const ALL = "__all__";

interface ServicePickerProps {
  value: string;
  onChange: (service: string | null) => void;
  label: string;
}

/** A filter over the services the product knows, "all" when none is picked. */
export function ServicePicker({ value, onChange, label }: ServicePickerProps) {
  const { t } = useI18n();
  const env = useTelemetryEnv();
  const { data } = useTelemetryQuery<TelemetryService[]>("services", { range: "24h", env }, { intervalMs: 0 });
  const names = [...new Set([...(data ?? []).map((s) => s.name), ...(value ? [value] : [])])].sort();
  return (
    <Select value={value || ALL} onValueChange={(v) => onChange(v === ALL ? null : v)}>
      <SelectTrigger className="h-8 w-52" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{t("telemetryHub.common.allServices")}</SelectItem>
        {names.map((n) => (
          <SelectItem key={n} value={n}>
            {n}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
