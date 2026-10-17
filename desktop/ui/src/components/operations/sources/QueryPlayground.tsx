import { Play } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { ApiError, api, dataSourceFieldErrors, type MetricQueryResult } from "@/api";
import { isInstantResult } from "@/components/operations/charts/chartData";
import { LiveValueList, liveValueItems } from "@/components/operations/charts/LiveValueList";
import { TimeSeriesChart } from "@/components/operations/charts/TimeSeriesChart";
import { TIME_RANGE_MS, TimeRangeControl, type TimeRange } from "@/components/operations/console/TimeRangeControl";
import {
  fieldErrorText,
  queryHint,
  queryPlaceholder,
  queryWindow,
  toChartSeries,
} from "@/components/operations/sources/dataSourceSchema";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

const DEFAULT_RANGE: TimeRange = "1h";

interface QueryPlaygroundProps {
  sourceId: string;
  kind: string;
  query: string;
  onQueryChange: (query: string) => void;
}

interface QueryFailure {
  title: string;
  message: string;
}

/** "Try a query": one range query against a metrics source, drawn with the console's chart. */
export function QueryPlayground({ sourceId, kind, query, onQueryChange }: QueryPlaygroundProps) {
  const { t } = useI18n();
  const q = (key: string, params?: Record<string, string | number>) => t(`operations.sources.query.${key}`, params);
  const [range, setRange] = useState<TimeRange>(DEFAULT_RANGE);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<MetricQueryResult | null>(null);
  const [failure, setFailure] = useState<QueryFailure | null>(null);
  const seq = useRef(0);

  const series = useMemo(() => toChartSeries(result?.series ?? []), [result]);
  const instant = result !== null && isInstantResult(series);
  const hint = queryHint(t, kind);

  const describe = (err: unknown): QueryFailure => {
    const fields = dataSourceFieldErrors(err);
    if (fields) {
      const message = Object.entries(fields)
        .map(([key, code]) => `${q(`fields.${key}`)}: ${fieldErrorText(t, key, code)}`)
        .join(" ");
      return { title: q("failed"), message };
    }
    const message = err instanceof Error ? err.message : t("common.actionFailed");
    if (err instanceof ApiError) {
      if (err.type === "bad_query") return { title: q("badQuery"), message };
      if (err.type === "upstream") return { title: q("upstream"), message };
      if (err.type === "unsupported") return { title: q("unsupported"), message: "" };
    }
    return { title: q("failed"), message };
  };

  const run = async (windowRange = range) => {
    const text = query.trim();
    if (!text) return;
    const mine = ++seq.current;
    setRunning(true);
    setFailure(null);
    try {
      const res = await api.queryDataSource(sourceId, { query: text, ...queryWindow(TIME_RANGE_MS[windowRange]), step_seconds: 0 });
      if (mine !== seq.current) return;
      setResult(res);
    } catch (err) {
      if (mine !== seq.current) return;
      setResult(null);
      setFailure(describe(err));
    } finally {
      if (mine === seq.current) setRunning(false);
    }
  };

  const warnings = result?.warnings ?? [];

  return (
    <section className="space-y-3" aria-labelledby="ds-query-title">
      <div>
        <h3 id="ds-query-title" className="text-heading font-semibold">
          {q("title")}
        </h3>
        <p className="text-caption text-muted-foreground">{q("description")}</p>
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="ds-query">{q("label")}</Label>
        <Textarea
          id="ds-query"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              void run();
            }
          }}
          placeholder={queryPlaceholder(t, kind)}
          rows={3}
          spellCheck={false}
          className="font-mono text-caption"
          aria-describedby="ds-query-hint"
        />
        <p id="ds-query-hint" className="text-caption text-muted-foreground">
          {hint ? `${hint} ${q("shortcut")}` : q("shortcut")}
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <TimeRangeControl
          value={range}
          onChange={(next) => {
            setRange(next);
            if (result) void run(next);
          }}
        />
        <Button onClick={() => void run()} disabled={running || query.trim() === ""}>
          <Play aria-hidden />
          {running ? q("running") : q("run")}
        </Button>
        {result && !running && (
          <span className="text-caption text-muted-foreground">{q("seriesCount", { count: result.series.length })}</span>
        )}
      </div>
      {failure && (
        <Notice variant="error" title={failure.title}>
          {failure.message && <p className="break-words font-mono text-caption">{failure.message}</p>}
        </Notice>
      )}
      {warnings.length > 0 && (
        <Notice variant="warning" title={q("warnings")}>
          <ul className="list-disc space-y-0.5 pl-4">
            {warnings.map((w) => (
              <li key={w} className="break-words">
                {w}
              </li>
            ))}
          </ul>
        </Notice>
      )}
      {instant ? (
        <div className="space-y-1.5">
          <LiveValueList items={liveValueItems(series)} ariaLabel={q("valuesLabel")} height={260} />
          <p className="text-caption text-muted-foreground">{q("liveNote")}</p>
        </div>
      ) : (
        (result || running) && (
          <TimeSeriesChart
            series={series}
            ariaLabel={q("chartLabel")}
            height={220}
            loading={running && !result}
            showTableToggle
            emptyLabel={q("noSeries")}
          />
        )
      )}
    </section>
  );
}
