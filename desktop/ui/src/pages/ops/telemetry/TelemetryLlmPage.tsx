import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { SeriesChartCard } from "@/components/operations/telemetry/SeriesChartCard";
import { ServicePicker } from "@/components/operations/telemetry/ServicePicker";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Badge } from "@/components/ui/badge";
import { StatTile } from "@/components/ui/stat-tile";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio } from "@/lib/telemetryHub";
import { formatUsd, type TelemetryAiCall, type TelemetryAiOverview } from "@/lib/telemetryViews";

const TABS = ["models", "services", "endpoints", "calls"] as const;
type Tab = (typeof TABS)[number];

/** `/operations/telemetry/llm`: the LLM calls your own services make, seen in
 * their spans: tokens, estimated cost, latency and errors by model, service and
 * endpoint. Not TaskTrooper's own agent usage, which has a page of its own. */
export default function TelemetryLlmPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.llm.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const [service, setService] = useUrlParam("service");
  const [tabParam, setTab] = useUrlParam("tab");
  const tab: Tab = TABS.find((x) => x === tabParam) ?? "models";
  const q = { range: scope.range, env: scope.env, service: service || undefined };
  const read = useTelemetryQuery<TelemetryAiOverview>("ai/overview", q);
  const calls = useTelemetryQuery<{ calls: TelemetryAiCall[] | null }>("ai/calls", { ...q, limit: 100 }, { enabled: tab === "calls" });
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "short", timeStyle: "medium" });
  const tokens = (input: number, output: number) => v("tokensInOut", { input: formatCount(input, locale), output: formatCount(output, locale) });

  return (
    <div className="space-y-4">
      <TelemetryToolbar>
        <ServicePicker value={service} onChange={setService} label={v("service")} />
      </TelemetryToolbar>
      <TelemetryQueryView query={read} isEmpty={(d) => d.totals.calls === 0} emptyTitle={v("empty")} emptyDescription={v("emptyHint")}>
        {(d) => {
          return (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                <StatTile label={v("calls")} value={formatCount(d.totals.calls, locale)} foot={v("errorRateFoot", { value: formatRatio(d.totals.errorRate, locale) })} />
                <StatTile
                  label={v("tokens")}
                  value={formatCount(d.totals.inputTokens + d.totals.outputTokens, locale)}
                  foot={tokens(d.totals.inputTokens, d.totals.outputTokens)}
                />
                <StatTile label={v("cost")} value={formatUsd(d.totals.costUsd, locale)} foot={d.totals.costPartial ? v("costPartial") : v("costEstimate")} />
                <StatTile
                  label={v("latency")}
                  value={formatMs(d.totals.p95Ms, locale)}
                  foot={d.totals.ttftP95Ms ? v("ttft", { value: formatMs(d.totals.ttftP95Ms, locale) }) : v("p50", { value: formatMs(d.totals.p50Ms, locale) })}
                />
              </div>
              <div className="grid gap-4 lg:grid-cols-2">
                <SeriesChartCard title={v("charts.calls")} set={d.series?.calls} scope={scope} syncKey="tel:llm" />
                <SeriesChartCard title={v("charts.tokens")} set={d.series?.tokens} scope={scope} formatValue={(n) => formatCount(n, locale)} syncKey="tel:llm" />
                <SeriesChartCard title={v("charts.cost")} set={d.series?.cost} scope={scope} formatValue={(n) => formatUsd(n, locale)} syncKey="tel:llm" />
                <SeriesChartCard title={v("charts.latency")} set={d.series?.latency} scope={scope} formatValue={(n) => formatMs(n, locale)} syncKey="tel:llm" />
              </div>
              <Tabs value={tab} onValueChange={(x) => setTab(x === "models" ? null : x)}>
                <TabsList aria-label={v("sections")}>
                  {TABS.map((x) => (
                    <TabsTrigger key={x} value={x}>
                      {v(`tabs.${x}`)}
                    </TabsTrigger>
                  ))}
                </TabsList>
                <TabsContent value="models" className="pt-4">
                  <TelemetryTable
                    label={v("tabs.models")}
                    columns={[
                      { key: "model", label: v("columns.model") },
                      { key: "calls", label: v("columns.calls"), numeric: true },
                      { key: "errors", label: v("columns.errors"), numeric: true },
                      { key: "tokens", label: v("columns.tokens"), numeric: true },
                      { key: "cost", label: v("columns.cost"), numeric: true },
                      { key: "p95", label: v("columns.p95"), numeric: true },
                    ]}
                  >
                    {(d.byModel ?? []).map((m) => (
                      <TelemetryRow key={`${m.provider}/${m.model}`}>
                        <TelemetryCell>
                          <span className="font-medium">{m.model}</span>
                          <p className="text-micro text-muted-foreground">{m.provider}</p>
                        </TelemetryCell>
                        <TelemetryCell numeric>{formatCount(m.calls, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{formatCount(m.errors, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{tokens(m.inputTokens, m.outputTokens)}</TelemetryCell>
                        <TelemetryCell numeric>{m.priceKnown ? formatUsd(m.costUsd, locale) : <Badge variant="outline">{v("noPrice")}</Badge>}</TelemetryCell>
                        <TelemetryCell numeric>{formatMs(m.p95Ms, locale)}</TelemetryCell>
                      </TelemetryRow>
                    ))}
                  </TelemetryTable>
                </TabsContent>
                <TabsContent value="services" className="pt-4">
                  <TelemetryTable
                    label={v("tabs.services")}
                    columns={[
                      { key: "service", label: v("columns.service") },
                      { key: "models", label: v("columns.models") },
                      { key: "calls", label: v("columns.calls"), numeric: true },
                      { key: "tokens", label: v("columns.tokens"), numeric: true },
                      { key: "cost", label: v("columns.cost"), numeric: true },
                    ]}
                  >
                    {(d.byService ?? []).map((s) => (
                      <TelemetryRow key={s.service}>
                        <TelemetryCell className="font-medium">{s.service}</TelemetryCell>
                        <TelemetryCell className="max-w-xs truncate text-muted-foreground">{(s.models ?? []).join(", ")}</TelemetryCell>
                        <TelemetryCell numeric>{formatCount(s.calls, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{tokens(s.inputTokens, s.outputTokens)}</TelemetryCell>
                        <TelemetryCell numeric>{formatUsd(s.costUsd, locale)}</TelemetryCell>
                      </TelemetryRow>
                    ))}
                  </TelemetryTable>
                </TabsContent>
                <TabsContent value="endpoints" className="pt-4">
                  <TelemetryTable
                    label={v("tabs.endpoints")}
                    columns={[
                      { key: "endpoint", label: v("columns.endpoint") },
                      { key: "calls", label: v("columns.calls"), numeric: true },
                      { key: "avgTokens", label: v("columns.avgTokens"), numeric: true },
                      { key: "perCall", label: v("columns.costPerCall"), numeric: true },
                      { key: "cost", label: v("columns.cost"), numeric: true },
                      { key: "p95", label: v("columns.p95"), numeric: true },
                    ]}
                  >
                    {(d.byEndpoint ?? []).map((e) => (
                      <TelemetryRow key={`${e.service}:${e.method}:${e.route}`}>
                        <TelemetryCell className="max-w-sm">
                          <span className="block truncate font-medium">{[e.method, e.route].filter(Boolean).join(" ") || "-"}</span>
                          <span className="block truncate text-micro text-muted-foreground">{e.service}</span>
                        </TelemetryCell>
                        <TelemetryCell numeric>{formatCount(e.calls, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{tokens(e.avgInputTokens, e.avgOutputTokens)}</TelemetryCell>
                        <TelemetryCell numeric>{formatUsd(e.costPerCallUsd, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{formatUsd(e.costUsd, locale)}</TelemetryCell>
                        <TelemetryCell numeric>{formatMs(e.p95Ms, locale)}</TelemetryCell>
                      </TelemetryRow>
                    ))}
                  </TelemetryTable>
                </TabsContent>
                <TabsContent value="calls" className="pt-4">
                  <TelemetryQueryView query={calls} isEmpty={(c) => (c.calls ?? []).length === 0} emptyTitle={v("noCalls")}>
                    {(c) => (
                      <TelemetryTable
                        label={v("tabs.calls")}
                        columns={[
                          { key: "time", label: v("columns.time"), className: "w-40" },
                          { key: "model", label: v("columns.model") },
                          { key: "where", label: v("columns.endpoint") },
                          { key: "tokens", label: v("columns.tokens"), numeric: true },
                          { key: "cost", label: v("columns.cost"), numeric: true },
                          { key: "duration", label: v("columns.duration"), numeric: true },
                        ]}
                      >
                        {(c.calls ?? []).map((call) => (
                          <TelemetryRow key={`${call.traceId}:${call.spanId}`}>
                            <TelemetryCell className="font-mono text-micro whitespace-nowrap">
                              <Link to={href(`/operations/telemetry/traces/${encodeURIComponent(call.traceId)}`)} className="hover:underline">
                                {time.format(call.ts)}
                              </Link>
                            </TelemetryCell>
                            <TelemetryCell>
                              <span className="font-medium">{call.model}</span>
                              {call.error && (
                                <Badge variant="destructive" className="ml-2">
                                  {v("failed")}
                                </Badge>
                              )}
                              <p className="text-micro text-muted-foreground">{[call.provider, call.operation].filter(Boolean).join(" · ")}</p>
                            </TelemetryCell>
                            <TelemetryCell className="max-w-xs">
                              <span className="block truncate">{call.route || "-"}</span>
                              <span className="block truncate text-micro text-muted-foreground">{call.service}</span>
                            </TelemetryCell>
                            <TelemetryCell numeric>{tokens(call.inputTokens, call.outputTokens)}</TelemetryCell>
                            <TelemetryCell numeric>{formatUsd(call.costUsd, locale)}</TelemetryCell>
                            <TelemetryCell numeric>{formatMs(call.durationMs, locale)}</TelemetryCell>
                          </TelemetryRow>
                        ))}
                      </TelemetryTable>
                    )}
                  </TelemetryQueryView>
                </TabsContent>
              </Tabs>
            </div>
          );
        }}
      </TelemetryQueryView>
    </div>
  );
}
