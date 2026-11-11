import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { ServicePicker } from "@/components/operations/telemetry/ServicePicker";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatMs, type TelemetryTraceSummary } from "@/lib/telemetryHub";

const SORTS = ["recent", "slowest"] as const;

/** `/operations/telemetry/traces`: entry spans found in the window; one opens
 * its waterfall. */
export default function TelemetryTracesPage() {
  const { t, lang } = useI18n();
  const h = (key: string) => t(`telemetryHub.traces.${key}`);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const [service, setService] = useUrlParam("service");
  const [errorsOnly, setErrorsOnly] = useUrlParam("errors");
  const [minMs, setMinMs] = useUrlParam("minMs");
  const [sortParam, setSort] = useUrlParam("sort");
  const sort = SORTS.find((s) => s === sortParam) ?? "recent";
  const time = new Intl.DateTimeFormat(locale, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
  const min = Number(minMs);

  const read = useTelemetryQuery<TelemetryTraceSummary[]>("traces", {
    range: scope.range,
    env: scope.env,
    service: service || undefined,
    errors: errorsOnly === "1" ? true : undefined,
    minMs: Number.isFinite(min) && min > 0 ? min : undefined,
    sort,
    limit: 100,
  });

  return (
    <div className="space-y-4">
      <TelemetryToolbar>
        <ServicePicker value={service} onChange={setService} label={h("service")} />
        <Input
          className="h-8 w-36"
          inputMode="numeric"
          value={minMs}
          onChange={(e) => setMinMs(e.target.value.replace(/[^0-9.]/g, ""))}
          placeholder={h("minMs")}
          aria-label={h("minMs")}
        />
        <Select value={sort} onValueChange={(v) => setSort(v === "recent" ? null : v)}>
          <SelectTrigger className="h-8 w-40" aria-label={h("sort")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SORTS.map((s) => (
              <SelectItem key={s} value={s}>
                {h(`sorts.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="flex items-center gap-2">
          <Switch id="telemetry-traces-errors" checked={errorsOnly === "1"} onCheckedChange={(on) => setErrorsOnly(on ? "1" : null)} />
          <Label htmlFor="telemetry-traces-errors">{h("errorsOnly")}</Label>
        </div>
      </TelemetryToolbar>

      <TelemetryQueryView query={read} isEmpty={(d) => d.length === 0} emptyTitle={h("empty")} emptyDescription={h("emptyHint")}>
        {(rows) => (
          <TelemetryTable
            label={h("listLabel")}
            columns={[
              { key: "time", label: h("columns.time") },
              { key: "root", label: h("columns.request") },
              { key: "service", label: h("columns.service") },
              { key: "status", label: h("columns.status") },
              { key: "dur", label: h("columns.duration"), numeric: true },
            ]}
          >
            {rows.map((tr) => (
              <TelemetryRow key={tr.id}>
                <TelemetryCell className="whitespace-nowrap font-mono text-muted-foreground">{time.format(tr.ts)}</TelemetryCell>
                <TelemetryCell>
                  <Link to={href(`/operations/telemetry/traces/${encodeURIComponent(tr.id)}`)} className="font-mono hover:underline">
                    {[tr.method, tr.route].filter(Boolean).join(" ") || tr.id}
                  </Link>
                </TelemetryCell>
                <TelemetryCell>{tr.service}</TelemetryCell>
                <TelemetryCell>
                  {tr.isError ? <Badge variant="destructive">{tr.status || h("failed")}</Badge> : <Badge variant="secondary">{tr.status || h("ok")}</Badge>}
                </TelemetryCell>
                <TelemetryCell numeric>{formatMs(tr.dur, locale)}</TelemetryCell>
              </TelemetryRow>
            ))}
          </TelemetryTable>
        )}
      </TelemetryQueryView>
    </div>
  );
}
