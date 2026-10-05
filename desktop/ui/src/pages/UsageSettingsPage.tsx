import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { BarChart3 } from "lucide-react";
import { toast } from "sonner";
import { api, type UsageByModel, type UsageSummary } from "@/api";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { HelpTooltip } from "@/components/ui/help-tooltip";
import { Skeleton } from "@/components/ui/skeleton";
import { StatTile } from "@/components/ui/stat-tile";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { UsageDailyChart, type DailySeries } from "@/components/admin/UsageDailyChart";
import { useCachedState } from "@/hooks/useCachedState";
import { useI18n } from "@/hooks/useI18n";
import {
  EMPTY_TOTALS,
  breakdown,
  browserTimeZone,
  cacheHitRate,
  fillDays,
  formatCompact,
  formatDay,
  formatInteger,
  formatPercent,
  rowsOfKind,
  sumTotals,
} from "@/lib/usage";
import { cn } from "@/lib/utils";

const RANGES = [7, 30, 90] as const;

type Metric = "input" | "output";

// Brand names, so not translated; "local" and unknown providers are.
const PROVIDER_NAMES: Record<string, string> = {
  claude_code: "Claude Code",
  cursor_agent: "Cursor",
  antigravity: "Antigravity",
  opencode: "OpenCode",
  anthropic: "Anthropic",
  gemini: "Gemini",
  openai: "OpenAI",
  groq: "Groq",
};

// Sentinels the server writes when it could not know the model.
const DEFAULT_MODEL = "(default)";
const UNRECORDED_MODEL = "(unrecorded)";

// Token counts only: prices differ per provider, plan and mode, and a CLI on a
// subscription is not billed per token at all, so any cost figure here would be
// a guess dressed as a number.
export function UsageSettingsPage() {
  const { t, lang } = useI18n();
  const [days, setDays] = useCachedState<number>("usage:days", 30);
  const [metric, setMetric] = useCachedState<Metric>("usage:metric", "input");
  const [summary, setSummary] = useCachedState<UsageSummary | null>("usage:summary", null);
  const [loading, setLoading] = useState(false);
  const latest = useRef(0);

  const load = useCallback(
    async (range: number) => {
      const id = ++latest.current;
      setLoading(true);
      try {
        const next = await api.usageSummary(range, browserTimeZone());
        if (id === latest.current) setSummary(next);
      } catch (err) {
        if (id === latest.current) {
          toast.error(err instanceof Error ? err.message : t("settingsPages.usage.loadFailed"));
        }
      } finally {
        if (id === latest.current) setLoading(false);
      }
    },
    [t, setSummary],
  );

  useEffect(() => {
    void load(days);
  }, [days, load]);

  const view = useMemo(() => {
    if (!summary) return null;
    const rows = summary.by_model ?? [];
    const generationRows = rowsOfKind(rows, "cli", "api").sort(
      (a, b) => b.prompt_tokens + b.completion_tokens - (a.prompt_tokens + a.completion_tokens),
    );
    return {
      generation: summary.generation ?? EMPTY_TOTALS,
      embedding: summary.embedding ?? EMPTY_TOTALS,
      cli: sumTotals(rowsOfKind(rows, "cli")),
      api: sumTotals(rowsOfKind(rows, "api")),
      generationRows,
      embeddingRows: rowsOfKind(rows, "embedding"),
      daily: fillDays(summary.from, summary.to, summary.daily ?? []),
    };
  }, [summary]);

  const u = (key: string, params?: Record<string, string | number>) => t(`settingsPages.usage.${key}`, params);

  const rangeControl = (
    <Tabs value={String(days)} onValueChange={(v) => setDays(Number(v))} variant="pill" className="w-fit">
      <TabsList aria-label={u("rangeLabel")}>
        {RANGES.map((r) => (
          <TabsTrigger key={r} value={String(r)}>
            {u("lastNDays", { days: r })}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );

  if (!view || !summary) {
    return (
      <div className="space-y-6">
        {rangeControl}
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-28" />
          ))}
        </div>
        <Skeleton className="h-80" />
      </div>
    );
  }

  const { generation, embedding, cli, api: apiTotals, generationRows, embeddingRows, daily } = view;
  const parts = breakdown(generation);
  const hit = cacheHitRate(generation);
  const hasAny = generation.calls > 0 || embedding.calls > 0;
  const shortDay = (day: string) => formatDay(day, lang, { day: "numeric", month: "short" });

  const series: DailySeries[] =
    metric === "input"
      ? [
          { key: "cacheRead", label: u("seriesCacheRead"), swatch: "bg-chart-3", value: (d) => d.cache_read_tokens },
          { key: "cacheWrite", label: u("seriesCacheWrite"), swatch: "bg-chart-2", value: (d) => d.cache_write_tokens },
          { key: "fresh", label: u("seriesFresh"), swatch: "bg-chart-1", value: (d) => breakdown(d).fresh },
        ]
      : [{ key: "output", label: u("seriesOutput"), swatch: "bg-chart-4", value: (d) => d.completion_tokens }];

  const segments = [
    { key: "fresh", label: u("seriesFresh"), swatch: "bg-chart-1", value: parts.fresh },
    { key: "cacheWrite", label: u("seriesCacheWrite"), swatch: "bg-chart-2", value: parts.cacheWrite },
    { key: "cacheRead", label: u("seriesCacheRead"), swatch: "bg-chart-3", value: parts.cacheRead },
    { key: "output", label: u("seriesOutput"), swatch: "bg-chart-4", value: parts.output },
  ];

  const perSession = (tokens: number) =>
    generation.calls > 0 ? u("perSessionFoot", { avg: formatCompact(Math.round(tokens / generation.calls)) }) : undefined;

  const providerName = (p: string) => PROVIDER_NAMES[p] ?? (p === "local" ? u("providerLocal") : p || u("providerUnknown"));

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        {rangeControl}
        <p className="text-caption text-muted-foreground">
          {u("windowCaption", {
            from: formatDay(summary.from, lang, { day: "numeric", month: "short" }),
            to: formatDay(summary.to, lang, { day: "numeric", month: "short", year: "numeric" }),
            timezone: summary.timezone,
          })}
        </p>
      </div>

      {!hasAny ? (
        <Card>
          <EmptyState icon={BarChart3} title={u("emptyTitle")} description={u("emptyDescription")} />
        </Card>
      ) : (
        <div className={cn("space-y-6 transition-opacity duration-200", loading && "opacity-60")} aria-busy={loading}>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <StatTile
              className="p-5"
              label={u("inputTokens")}
              value={formatCompact(generation.prompt_tokens)}
              title={formatInteger(generation.prompt_tokens, lang)}
              foot={perSession(generation.prompt_tokens)}
            />
            <StatTile
              className="p-5"
              label={u("outputTokens")}
              value={formatCompact(generation.completion_tokens)}
              title={formatInteger(generation.completion_tokens, lang)}
              foot={perSession(generation.completion_tokens)}
            />
            <StatTile
              className="p-5"
              label={u("cacheHit")}
              value={hit !== null ? formatPercent(hit, lang) : "—"}
              foot={u("cacheHitFoot")}
            >
              <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-chart-3/20">
                <div className="h-full rounded-full bg-chart-3" style={{ width: `${(hit ?? 0) * 100}%` }} />
              </div>
            </StatTile>
            <StatTile
              className="p-5"
              label={u("sessions")}
              value={formatInteger(generation.calls, lang)}
              foot={u("sessionsFoot", { cli: formatInteger(cli.calls, lang), api: formatInteger(apiTotals.calls, lang) })}
            />
          </div>

          <Card className="p-5">
            <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
              <div>
                <h3 className="text-heading font-semibold">{u("dailyTitle")}</h3>
                <p className="text-caption text-muted-foreground">{u("dailySubtitle")}</p>
              </div>
              <Tabs value={metric} onValueChange={(v) => setMetric(v as Metric)} variant="pill" className="w-fit">
                <TabsList aria-label={u("dailyTitle")}>
                  <TabsTrigger value="input">{u("metricInput")}</TabsTrigger>
                  <TabsTrigger value="output">{u("metricOutput")}</TabsTrigger>
                </TabsList>
              </Tabs>
            </div>
            {series.length > 1 && (
              <ul className="mb-4 flex flex-wrap gap-x-5 gap-y-1 text-caption text-muted-foreground">
                {[...series].reverse().map((s) => (
                  <li key={s.key} className="flex items-center gap-1.5">
                    <span className={cn("h-2.5 w-2.5 rounded-[3px]", s.swatch)} />
                    {s.label}
                  </li>
                ))}
              </ul>
            )}
            <UsageDailyChart
              days={daily}
              series={series}
              lang={lang}
              ariaLabel={u("chartLabel", {
                metric: metric === "input" ? u("metricInput") : u("metricOutput"),
                from: shortDay(summary.from),
                to: shortDay(summary.to),
              })}
              totalLabel={u("tooltipTotal")}
              callsLabel={(count) => u("tooltipCalls", { count })}
            />
          </Card>

          <div className="grid gap-6 xl:grid-cols-3 xl:items-start">
            <Card className="p-5 xl:col-span-2">
              <h3 className="text-heading font-semibold">{u("byModelTitle")}</h3>
              <p className="mb-4 text-caption text-muted-foreground">{u("byModelSubtitle")}</p>
              {generationRows.length === 0 ? (
                <p className="py-6 text-center text-body text-muted-foreground">{u("noGeneration")}</p>
              ) : (
                <ModelTable rows={generationRows} total={parts.total} lang={lang} u={u} providerName={providerName} />
              )}
            </Card>

            <div className="space-y-6">
              <Card className="p-5">
                <h3 className="text-heading font-semibold">{u("breakdownTitle")}</h3>
                <p className="mb-4 text-caption text-muted-foreground">{u("breakdownSubtitle")}</p>
                <div className="flex h-3 w-full gap-[2px] overflow-hidden rounded-full bg-muted">
                  {segments
                    .filter((s) => parts.total > 0 && s.value / parts.total >= 0.005)
                    .map((s) => (
                      <div key={s.key} className={s.swatch} style={{ flex: `${s.value} 1 0px` }} />
                    ))}
                </div>
                <ul className="mt-4 space-y-2.5">
                  {segments.map((s) => (
                    <li key={s.key} className="flex items-center gap-2 text-body">
                      <span className={cn("h-2.5 w-2.5 shrink-0 rounded-[3px]", s.swatch)} />
                      <span className="min-w-0 flex-1 truncate text-muted-foreground">{s.label}</span>
                      <span className="font-medium tabular-nums" title={formatInteger(s.value, lang)}>
                        {formatCompact(s.value)}
                      </span>
                      <span className="w-12 text-right text-caption tabular-nums text-muted-foreground">
                        {parts.total > 0 ? formatPercent(s.value / parts.total, lang) : "—"}
                      </span>
                    </li>
                  ))}
                </ul>
              </Card>

              <Card className="p-5">
                <div className="flex items-center gap-1.5">
                  <h3 className="text-heading font-semibold">{u("embeddingTitle")}</h3>
                  <HelpTooltip text={u("embeddingHelp")} />
                </div>
                {embedding.calls === 0 ? (
                  <p className="mt-2 text-body text-muted-foreground">{u("embeddingEmpty")}</p>
                ) : (
                  <>
                    <div className="mt-3 flex items-baseline gap-4">
                      <p>
                        <span className="text-title font-semibold">{formatCompact(embedding.calls)}</span>{" "}
                        <span className="text-caption text-muted-foreground">{u("embeddingCalls")}</span>
                      </p>
                      <p>
                        <span className="text-title font-semibold">~{formatCompact(embedding.prompt_tokens)}</span>{" "}
                        <span className="text-caption text-muted-foreground">{u("embeddingTokens")}</span>
                      </p>
                    </div>
                    <ul className="mt-3 space-y-1.5 border-t border-border pt-3">
                      {embeddingRows.map((r) => (
                        <li key={`${r.provider}/${r.model}`} className="flex items-center gap-2 text-caption">
                          <ModelName model={r.model} u={u} />
                          <span className="text-muted-foreground">{providerName(r.provider)}</span>
                          <span className="ml-auto tabular-nums text-muted-foreground">{formatInteger(r.calls, lang)}</span>
                        </li>
                      ))}
                    </ul>
                  </>
                )}
              </Card>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

type Translate = (key: string, params?: Record<string, string | number>) => string;

function ModelName({ model, u }: { model: string; u: Translate }) {
  if (model === UNRECORDED_MODEL) {
    return (
      <span className="inline-flex items-center gap-1 italic text-muted-foreground">
        {u("unrecordedModel")}
        <HelpTooltip text={u("unrecordedHint")} />
      </span>
    );
  }
  if (model === DEFAULT_MODEL || model === "") {
    return <span className="italic text-muted-foreground">{u("defaultModel")}</span>;
  }
  return <span className="truncate font-mono text-caption">{model}</span>;
}

function ModelTable({
  rows,
  total,
  lang,
  u,
  providerName,
}: {
  rows: UsageByModel[];
  total: number;
  lang: string;
  u: Translate;
  providerName: (p: string) => string;
}) {
  return (
    <div className="-mx-5 overflow-x-auto px-5">
      <table className="w-full min-w-[640px] text-body">
        <thead>
          <tr className="border-b border-border text-left text-caption text-muted-foreground">
            <th className="pb-2 font-medium">{u("modelColumn")}</th>
            <th className="pb-2 font-medium">{u("sourceColumn")}</th>
            <th className="pb-2 pl-4 text-right font-medium">{u("callsColumn")}</th>
            <th className="pb-2 pl-4 text-right font-medium">{u("inputColumn")}</th>
            <th className="pb-2 pl-4 text-right font-medium">{u("cacheColumn")}</th>
            <th className="pb-2 pl-4 text-right font-medium">{u("outputColumn")}</th>
            <th className="pb-2 pl-6 font-medium">{u("shareColumn")}</th>
          </tr>
        </thead>
        <tbody className="tabular-nums">
          {rows.map((r) => {
            const tokens = r.prompt_tokens + r.completion_tokens;
            const share = total > 0 ? tokens / total : 0;
            const hit = cacheHitRate(r);
            return (
              <tr key={`${r.kind}/${r.provider}/${r.model}`} className="border-b border-border/50 last:border-0">
                <td className="max-w-64 py-2.5 pr-3">
                  <ModelName model={r.model} u={u} />
                </td>
                <td className="py-2.5 pr-3 whitespace-nowrap">
                  {providerName(r.provider)}
                  <span className="ml-1.5 text-caption text-muted-foreground">
                    · {r.kind === "cli" ? u("kindCli") : u("kindApi")}
                  </span>
                </td>
                <td className="pl-4 py-2.5 text-right">{formatInteger(r.calls, lang)}</td>
                <td className="pl-4 py-2.5 text-right" title={formatInteger(r.prompt_tokens, lang)}>
                  {formatCompact(r.prompt_tokens)}
                </td>
                <td className="pl-4 py-2.5 text-right text-muted-foreground">
                  {hit !== null ? formatPercent(hit, lang) : "—"}
                </td>
                <td className="pl-4 py-2.5 text-right" title={formatInteger(r.completion_tokens, lang)}>
                  {formatCompact(r.completion_tokens)}
                </td>
                <td className="py-2.5 pl-6">
                  <div className="flex items-center gap-2">
                    <div className="h-1.5 w-20 overflow-hidden rounded-full bg-muted">
                      <div className="h-full rounded-full bg-muted-foreground/70" style={{ width: `${share * 100}%` }} />
                    </div>
                    <span className="w-10 text-right text-caption text-muted-foreground">{formatPercent(share, lang)}</span>
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
