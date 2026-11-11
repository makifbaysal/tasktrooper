import { Search } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "@/api";
import { VolumeHistogram } from "@/components/operations/charts/VolumeHistogram";
import { FacetChips, type FacetChip } from "@/components/operations/console/FacetChips";
import { LogTable } from "@/components/operations/logs/LogTable";
import { severitySeries } from "@/components/operations/logs/severity";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { ServicePicker } from "@/components/operations/telemetry/ServicePicker";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { LOG_LEVELS, logVolumeBuckets, toLogEntries, type TelemetryLogLine, type TelemetryLogPage, type TelemetrySeriesSet } from "@/lib/telemetryHub";

const ALL = "all";
const PAGE = 200;
const NO_MAP = new Map();

/** `/operations/telemetry/logs`: the product's log lines on the console's own
 * log table, with volume per severity above them. */
export default function TelemetryLogsPage() {
  const { t } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.logs.${key}`, params);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const [service, setService] = useUrlParam("service");
  const [level, setLevel] = useUrlParam("level");
  const [text, setText] = useUrlParam("q");
  const [draft, setDraft] = useState(text);
  const [older, setOlder] = useState<TelemetryLogLine[]>([]);
  const [cursor, setCursor] = useState<number | undefined>(undefined);
  const [loadingOlder, setLoadingOlder] = useState(false);

  useEffect(() => setDraft(text), [text]);
  useEffect(() => {
    const id = setTimeout(() => {
      if (draft !== text) setText(draft);
    }, 400);
    return () => clearTimeout(id);
  }, [draft, text, setText]);

  const filter = useMemo(
    () => ({ range: scope.range, env: scope.env, service: service || undefined, level: level || undefined, q: text || undefined }),
    [scope.range, scope.env, service, level, text],
  );
  const filterKey = JSON.stringify(filter);
  useEffect(() => {
    setOlder([]);
    setCursor(undefined);
  }, [filterKey]);

  const page = useTelemetryQuery<TelemetryLogPage>("logs", { ...filter, limit: PAGE });
  const volume = useTelemetryQuery<TelemetrySeriesSet>("logs/series", filter);

  const newest = page.data?.lines ?? [];
  const nextBefore = cursor === undefined ? page.data?.nextBefore : cursor;
  const lines = useMemo(() => {
    const seen = new Set(newest.map((l) => l.id));
    return [...newest, ...older.filter((l) => !seen.has(l.id))];
  }, [newest, older]);
  const entries = useMemo(() => toLogEntries(lines), [lines]);
  const buckets = useMemo(() => logVolumeBuckets(volume.data), [volume.data]);
  const series = useMemo(() => severitySeries((s) => t(`operations.opsQuery.severity.${s}`)), [t]);

  const loadOlder = useCallback(async () => {
    if (!nextBefore) return;
    setLoadingOlder(true);
    try {
      const more = await api.telemetryGet<TelemetryLogPage>("logs", { ...filter, limit: PAGE, before: nextBefore });
      setOlder((prev) => [...prev, ...(more.lines ?? [])]);
      setCursor((more.lines?.length ?? 0) >= PAGE ? more.nextBefore : 0);
    } finally {
      setLoadingOlder(false);
    }
  }, [filter, nextBefore]);

  const levels = page.data?.levels ?? {};
  const facets: FacetChip<string>[] = [
    { value: ALL, label: h("allLevels"), count: Object.values(levels).reduce((a, b) => a + b, 0) },
    ...LOG_LEVELS.map((l) => ({ value: l, label: h(`levels.${l}`), count: levels[l] ?? 0 })),
  ];
  const canLoadOlder = Boolean(nextBefore) && (cursor !== undefined || (page.data?.lines?.length ?? 0) >= PAGE);

  return (
    <div className="space-y-4">
      <TelemetryToolbar>
        <div className="relative w-72 max-w-full">
          <Search className="pointer-events-none absolute top-2.5 left-2.5 h-3.5 w-3.5 text-muted-foreground" aria-hidden />
          <Input className="h-8 pl-8" value={draft} onChange={(e) => setDraft(e.target.value)} placeholder={h("search")} aria-label={h("search")} />
        </div>
        <ServicePicker value={service} onChange={setService} label={h("service")} />
      </TelemetryToolbar>
      <FacetChips label={h("levelFilter")} items={facets} active={level || ALL} onSelect={(v) => setLevel(v === ALL ? null : v)} />

      <Card className="p-4">
        <VolumeHistogram buckets={buckets} series={series} ariaLabel={h("volume")} loading={volume.data === undefined && !volume.error} emptyLabel={volume.error ?? h("noVolume")} showTableToggle />
      </Card>

      <TelemetryQueryView query={page} isEmpty={() => lines.length === 0} emptyTitle={h("empty")} emptyDescription={h("emptyHint")}>
        {() => (
          <div className="space-y-3">
            <LogTable
              entries={entries}
              environments={NO_MAP}
              entities={NO_MAP}
              showEntity={false}
              traceHref={(traceId) => href(`/operations/telemetry/traces/${encodeURIComponent(traceId)}`)}
            />
            {canLoadOlder && (
              <div className="flex justify-center">
                <Button variant="outline" onClick={() => void loadOlder()} disabled={loadingOlder}>
                  {loadingOlder ? h("loadingOlder") : h("loadOlder")}
                </Button>
              </div>
            )}
          </div>
        )}
      </TelemetryQueryView>
    </div>
  );
}
