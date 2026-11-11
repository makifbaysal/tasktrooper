import { useSearchParams } from "react-router-dom";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { TELEMETRY_ENV_PARAM } from "@/hooks/useTelemetryScope";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";

const ALL = "__all__";

/** The environments the product has seen, kept in the URL. */
export function TelemetryEnvSelect() {
  const { t } = useI18n();
  const [params, setParams] = useSearchParams();
  const { data } = useTelemetryQuery<string[]>("environments", undefined, { intervalMs: 0 });
  const current = params.get(TELEMETRY_ENV_PARAM) ?? ALL;
  const names = [...(data ?? [])];
  if (current !== ALL && !names.includes(current)) names.push(current);

  const change = (value: string) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value === ALL) next.delete(TELEMETRY_ENV_PARAM);
        else next.set(TELEMETRY_ENV_PARAM, value);
        return next;
      },
      { replace: true },
    );

  return (
    <Select value={current} onValueChange={change}>
      <SelectTrigger className="h-8 w-44" aria-label={t("telemetryHub.common.environment")}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{t("telemetryHub.common.allEnvironments")}</SelectItem>
        {names.map((n) => (
          <SelectItem key={n} value={n}>
            {n}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
