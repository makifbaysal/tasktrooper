import { Film } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { SeriesChartCard } from "@/components/operations/telemetry/SeriesChartCard";
import { TelemetryBackLink } from "@/components/operations/telemetry/TelemetryBackLink";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { VitalTile } from "@/components/operations/telemetry/VitalTile";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { StatTile } from "@/components/ui/stat-tile";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatMs, formatRatio, type TelemetrySeriesSet, type TelemetryService } from "@/lib/telemetryHub";
import { formatDuration, type TelemetryAppStart, type TelemetryAppSummary, type TelemetryRumError, type TelemetryRumPage, type TelemetrySession } from "@/lib/telemetryViews";
import { replayHref } from "@/pages/ops/telemetry/TelemetryReplaysPage";

const TABS = ["pages", "sessions", "errors", "starts"] as const;
type Tab = (typeof TABS)[number];

/** `/operations/telemetry/apps/:app`: one web or mobile app's users, Web
 * Vitals or start-up timings, its pages or screens, sessions, errors and crashes. */
export default function TelemetryAppPage() {
  const { app = "" } = useParams();
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.apps.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const [tabParam, setTab] = useUrlParam("tab");
  const tab: Tab = TABS.find((x) => x === tabParam) ?? "pages";
  const base = `apps/${encodeURIComponent(app)}`;
  const q = { range: scope.range, env: scope.env };
  const service = useTelemetryQuery<TelemetryService>(`services/${encodeURIComponent(app)}`, q, { intervalMs: 0 });
  const mobile = service.data?.type === "mobile";
  const summary = useTelemetryQuery<TelemetryAppSummary>(`${base}/summary`, q);
  const series = useTelemetryQuery<TelemetrySeriesSet>(`${base}/series`, q);
  const pages = useTelemetryQuery<TelemetryRumPage[]>(`${base}/pages`, q, { enabled: tab === "pages" });
  const sessions = useTelemetryQuery<TelemetrySession[]>(`${base}/sessions`, { ...q, limit: 50 }, { enabled: tab === "sessions" });
  const errors = useTelemetryQuery<TelemetryRumError[]>(`${base}/errors`, q, { enabled: tab === "errors" });
  const starts = useTelemetryQuery<TelemetryAppStart[]>(`${base}/app-starts`, q, { enabled: tab === "starts" });
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "short", timeStyle: "short" });
  const tabs = TABS.filter((x) => x !== "starts" || mobile);

  return (
    <div className="space-y-4">
      <TelemetryBackLink to="/operations/telemetry/apps" label={v("back")}>
        <Button asChild variant="outline" size="sm">
          <Link to={href(`/operations/telemetry/replays?app=${encodeURIComponent(app)}`)}>
            <Film aria-hidden />
            {v("replays")}
          </Link>
        </Button>
      </TelemetryBackLink>
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-heading font-semibold">{app}</h2>
        {service.data && <Badge variant="secondary">{v(`platforms.${mobile ? "mobile" : "web"}`)}</Badge>}
        <div className="ml-auto">
          <TelemetryToolbar />
        </div>
      </div>
      <TelemetryQueryView query={summary} skeletonHeight="h-24">
        {(s) => (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
              <StatTile label={v("activeUsers")} value={formatCount(s.activeUsers, locale)} foot={v("previous", { value: formatCount(s.prevActiveUsers, locale) })} />
              <StatTile label={v("sessions")} value={formatCount(s.sessions, locale)} />
              <StatTile label={mobile ? v("screenViews") : v("pageViews")} value={formatCount(s.pageViewsPerMin, locale)} foot={v("perMinute")} />
              <StatTile label={v("avgSession")} value={formatDuration(s.avgSessionSec)} />
              <StatTile
                label={mobile ? v("crashRate") : v("errorRate")}
                value={formatRatio(s.errorRate, locale)}
                tone={s.errorRate > s.prevErrorRate && s.errorRate > 0.01 ? "warning" : "default"}
                foot={v("previous", { value: formatRatio(s.prevErrorRate, locale) })}
              />
            </div>
            {(s.vitals ?? []).length > 0 && (
              <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                {(s.vitals ?? []).map((vital) => (
                  <VitalTile key={vital.id} vital={vital} />
                ))}
              </div>
            )}
          </div>
        )}
      </TelemetryQueryView>
      <div className="grid gap-4 lg:grid-cols-2">
        <SeriesChartCard
          title={v("trafficChart")}
          set={series.data}
          ids={["views", "users", "sessions"]}
          labels={{ views: mobile ? v("screenViews") : v("pageViews"), users: v("activeUsers"), sessions: v("sessions") }}
          scope={scope}
          loading={series.data === undefined && !series.error}
          syncKey="tel:app"
        />
        <SeriesChartCard
          title={mobile ? v("crashRate") : v("errorRate")}
          set={series.data}
          ids={["errorRate"]}
          labels={{ errorRate: mobile ? v("crashRate") : v("errorRate") }}
          scale={100}
          unit="%"
          scope={scope}
          loading={series.data === undefined && !series.error}
          syncKey="tel:app"
        />
      </div>
      <Tabs value={tab} onValueChange={(x) => setTab(x === "pages" ? null : x)}>
        <TabsList aria-label={v("sections")}>
          {tabs.map((x) => (
            <TabsTrigger key={x} value={x}>
              {x === "pages" && mobile ? v("tabs.screens") : v(`tabs.${x}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value="pages" className="pt-4">
          <TelemetryQueryView query={pages} isEmpty={(d) => d.length === 0} emptyTitle={v("noPages")}>
            {(rows) => (
              <TelemetryTable
                label={mobile ? v("tabs.screens") : v("tabs.pages")}
                columns={[
                  { key: "path", label: mobile ? v("columns.screen") : v("columns.page") },
                  { key: "views", label: v("columns.views"), numeric: true },
                  { key: "users", label: v("columns.users"), numeric: true },
                  ...(mobile
                    ? []
                    : [
                        { key: "lcp", label: "LCP", numeric: true },
                        { key: "inp", label: "INP", numeric: true },
                        { key: "cls", label: "CLS", numeric: true },
                      ]),
                  { key: "err", label: v("columns.errorRate"), numeric: true },
                  { key: "bounce", label: v("columns.bounce"), numeric: true },
                ]}
              >
                {[...rows]
                  .sort((a, b) => b.views - a.views)
                  .map((p) => (
                    <TelemetryRow key={p.id || p.path}>
                      <TelemetryCell className="max-w-sm truncate font-mono text-micro">{p.path}</TelemetryCell>
                      <TelemetryCell numeric>{formatCount(p.views, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatCount(p.users, locale)}</TelemetryCell>
                      {!mobile && (
                        <>
                          <TelemetryCell numeric>{formatMs(p.lcp, locale)}</TelemetryCell>
                          <TelemetryCell numeric>{formatMs(p.inp, locale)}</TelemetryCell>
                          <TelemetryCell numeric>{p.cls.toFixed(2)}</TelemetryCell>
                        </>
                      )}
                      <TelemetryCell numeric>{formatRatio(p.errorRate, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatRatio(p.bounce, locale)}</TelemetryCell>
                    </TelemetryRow>
                  ))}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
        </TabsContent>
        <TabsContent value="sessions" className="pt-4">
          <TelemetryQueryView query={sessions} isEmpty={(d) => d.length === 0} emptyTitle={v("noSessions")}>
            {(rows) => (
              <TelemetryTable
                label={v("tabs.sessions")}
                columns={[
                  { key: "started", label: v("columns.started"), className: "w-36" },
                  { key: "user", label: v("columns.user") },
                  { key: "device", label: v("columns.device") },
                  { key: "duration", label: v("columns.duration"), numeric: true },
                  { key: "pages", label: mobile ? v("columns.screens") : v("columns.pages"), numeric: true },
                  { key: "errors", label: v("columns.errors"), numeric: true },
                  { key: "replay", label: v("columns.replay") },
                ]}
              >
                {rows.map((s) => (
                  <TelemetryRow key={s.id}>
                    <TelemetryCell className="font-mono text-micro whitespace-nowrap">{time.format(s.startedAt)}</TelemetryCell>
                    <TelemetryCell className="max-w-48 truncate">{s.identified ? s.user : v("anonymous")}</TelemetryCell>
                    <TelemetryCell className="max-w-48 truncate text-muted-foreground">{[s.device, s.country].filter(Boolean).join(" · ")}</TelemetryCell>
                    <TelemetryCell numeric>{formatDuration(s.duration)}</TelemetryCell>
                    <TelemetryCell numeric>{s.pages}</TelemetryCell>
                    <TelemetryCell numeric className={s.errors > 0 ? "text-destructive" : undefined}>
                      {s.errors}
                    </TelemetryCell>
                    <TelemetryCell>
                      {s.hasReplay ? (
                        <Link to={href(replayHref(s.id))} className="hover:underline">
                          {v("watch")}
                        </Link>
                      ) : (
                        "-"
                      )}
                    </TelemetryCell>
                  </TelemetryRow>
                ))}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
        </TabsContent>
        <TabsContent value="errors" className="pt-4">
          <TelemetryQueryView query={errors} isEmpty={(d) => d.length === 0} emptyTitle={v("noErrors")}>
            {(rows) => (
              <TelemetryTable
                label={v("tabs.errors")}
                columns={[
                  { key: "error", label: v("columns.error") },
                  { key: "view", label: mobile ? v("columns.screen") : v("columns.page") },
                  { key: "count", label: v("columns.count"), numeric: true },
                  { key: "sessions", label: v("columns.sessionsAffected"), numeric: true },
                  { key: "version", label: v("columns.version") },
                  { key: "last", label: v("columns.lastSeen"), className: "w-36" },
                ]}
              >
                {[...rows]
                  .sort((a, b) => Number(b.crash) - Number(a.crash) || b.count - a.count)
                  .map((e) => (
                    <TelemetryRow key={e.fingerprint}>
                      <TelemetryCell className="max-w-md">
                        <span className="flex items-center gap-2">
                          {e.crash && <Badge variant="destructive">{v("crash")}</Badge>}
                          <span className="truncate font-medium">{e.type}</span>
                        </span>
                        <span className="block truncate text-micro text-muted-foreground">{e.message}</span>
                      </TelemetryCell>
                      <TelemetryCell className="max-w-48 truncate font-mono text-micro">{e.view || "-"}</TelemetryCell>
                      <TelemetryCell numeric>{formatCount(e.count, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatCount(e.sessions, locale)}</TelemetryCell>
                      <TelemetryCell className="font-mono text-micro">{e.appVersion || "-"}</TelemetryCell>
                      <TelemetryCell className="font-mono text-micro whitespace-nowrap">{time.format(e.lastSeen)}</TelemetryCell>
                    </TelemetryRow>
                  ))}
              </TelemetryTable>
            )}
          </TelemetryQueryView>
        </TabsContent>
        {mobile && (
          <TabsContent value="starts" className="pt-4">
            <TelemetryQueryView query={starts} isEmpty={(d) => d.length === 0} emptyTitle={v("noStarts")}>
              {(rows) => (
                <TelemetryTable
                  label={v("tabs.starts")}
                  columns={[
                    { key: "kind", label: v("columns.startKind") },
                    { key: "count", label: v("columns.count"), numeric: true },
                    { key: "p50", label: "p50", numeric: true },
                    { key: "p75", label: "p75", numeric: true },
                    { key: "p95", label: "p95", numeric: true },
                  ]}
                >
                  {rows.map((s) => (
                    <TelemetryRow key={s.kind}>
                      <TelemetryCell className="font-medium">{s.kind}</TelemetryCell>
                      <TelemetryCell numeric>{formatCount(s.count, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatMs(s.p50, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatMs(s.p75, locale)}</TelemetryCell>
                      <TelemetryCell numeric>{formatMs(s.p95, locale)}</TelemetryCell>
                    </TelemetryRow>
                  ))}
                </TelemetryTable>
              )}
            </TelemetryQueryView>
          </TabsContent>
        )}
      </Tabs>
    </div>
  );
}
