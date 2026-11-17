import { Link, useParams } from "react-router-dom";
import { DetailFacts } from "@/components/operations/console/DetailPanel";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { TelemetryBackLink } from "@/components/operations/telemetry/TelemetryBackLink";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { intlLocale } from "@/lib/languages";
import { formatMs } from "@/lib/telemetryHub";
import { formatDuration, type TelemetryReplayDetail } from "@/lib/telemetryViews";
import { appHref } from "@/pages/ops/telemetry/TelemetryAppsPage";
import { replayUser } from "@/pages/ops/telemetry/TelemetryReplaysPage";

const KINDS = ["view", "action", "error", "resource", "vital"];
const KIND_VARIANT: Record<string, BadgeProps["variant"]> = { view: "info", action: "secondary", error: "destructive", resource: "outline", vital: "warning" };

/** `/operations/telemetry/replays/:sessionId`: one recorded session as its
 * timeline of views, actions, errors and requests. The visual player is not
 * part of this bundle; every step links to its trace where there is one. */
export default function TelemetryReplayPage() {
  const { sessionId = "" } = useParams();
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.replays.${key}`, params);
  const locale = intlLocale(lang);
  const href = useOpsHref();
  useTelemetryScope({ time: false });
  const read = useTelemetryQuery<TelemetryReplayDetail>(`replays/${encodeURIComponent(sessionId)}`, undefined, { intervalMs: 0 });
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "medium", timeStyle: "medium" });

  return (
    <div className="space-y-4">
      <TelemetryBackLink to="/operations/telemetry/replays" label={v("back")} />
      <TelemetryQueryView query={read}>
        {({ summary: s, timeline }) => {
          const start = s.startedAt;
          const steps = [...(timeline ?? [])].sort((a, b) => a.ts - b.ts);
          return (
            <div className="space-y-4">
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="text-heading font-semibold">{replayUser(s, v("anonymous"))}</h2>
                {s.errors > 0 && <Badge variant="destructive">{v("errors", { count: s.errors })}</Badge>}
                {s.rageClicks > 0 && <Badge variant="warning">{v("rageClicks", { count: s.rageClicks })}</Badge>}
              </div>
              <Card className="p-4">
                <DetailFacts
                  items={[
                    { label: v("columns.app"), value: <Link className="hover:underline" to={href(appHref(s.app))}>{s.app}</Link> },
                    { label: v("columns.started"), value: time.format(s.startedAt) },
                    { label: v("columns.duration"), value: formatDuration(s.durationMs / 1000) },
                    { label: v("facts.device"), value: [s.platform, s.device, s.os, s.browser].filter(Boolean).join(" · ") || "-" },
                    { label: v("facts.location"), value: s.country || "-" },
                    { label: v("facts.version"), value: s.appVersion || "-" },
                    { label: v("facts.activity"), value: v("activity", { views: s.views, clicks: s.clicks }) },
                  ]}
                />
              </Card>
              <Notice variant="info" title={v("playerTitle")}>
                {v("playerBody")}
              </Notice>
              {steps.length === 0 ? (
                <p className="py-8 text-center text-caption text-muted-foreground">{v("noTimeline")}</p>
              ) : (
                <TelemetryTable
                  label={v("timeline")}
                  columns={[
                    { key: "at", label: v("timelineColumns.at"), numeric: true, className: "w-20" },
                    { key: "kind", label: v("timelineColumns.kind"), className: "w-28" },
                    { key: "what", label: v("timelineColumns.what") },
                    { key: "detail", label: v("timelineColumns.detail") },
                  ]}
                >
                  {steps.map((e, i) => (
                    <TelemetryRow key={`${e.ts}:${i}`}>
                      <TelemetryCell numeric>{formatDuration(Math.max(0, e.ts - start) / 1000)}</TelemetryCell>
                      <TelemetryCell>
                        <Badge variant={KIND_VARIANT[e.kind] ?? "outline"}>{KINDS.includes(e.kind) ? v(`kinds.${e.kind}`) : e.kind}</Badge>
                      </TelemetryCell>
                      <TelemetryCell className="max-w-md">
                        {e.traceId ? (
                          <Link to={href(`/operations/telemetry/traces/${encodeURIComponent(e.traceId)}`)} className="block truncate hover:underline">
                            {e.label}
                          </Link>
                        ) : (
                          <span className="block truncate">{e.label}</span>
                        )}
                        {e.viewName && <span className="block truncate text-micro text-muted-foreground">{e.viewName}</span>}
                      </TelemetryCell>
                      <TelemetryCell className="max-w-md text-micro text-muted-foreground">
                        <span className="line-clamp-2 break-words">
                          {[e.status ? String(e.status) : "", e.durationMs ? formatMs(e.durationMs, locale) : "", e.detail ?? ""].filter(Boolean).join(" · ")}
                        </span>
                      </TelemetryCell>
                    </TelemetryRow>
                  ))}
                </TelemetryTable>
              )}
            </div>
          );
        }}
      </TelemetryQueryView>
    </div>
  );
}
