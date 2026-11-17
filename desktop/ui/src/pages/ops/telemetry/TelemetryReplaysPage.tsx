import { Search } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router-dom";
import { FacetChips } from "@/components/operations/console/FacetChips";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryEnv, useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import type { TelemetryService } from "@/lib/telemetryHub";
import { formatDuration, type TelemetryReplaySummary } from "@/lib/telemetryViews";
import { CLIENT_TYPES } from "@/pages/ops/telemetry/TelemetryAppsPage";

const ALL = "__all__";

export function replayHref(sessionId: string): string {
  return `/operations/telemetry/replays/${encodeURIComponent(sessionId)}`;
}

export function replayUser(r: Pick<TelemetryReplaySummary, "user">, anonymous: string): string {
  return r.user?.id || (r.user?.anonymousId ? `${anonymous} ${r.user.anonymousId.slice(0, 8)}` : anonymous);
}

/** `/operations/telemetry/replays`: recorded sessions, newest first, with the
 * errors and rage clicks that make one worth watching. */
export default function TelemetryReplaysPage() {
  const { t, lang } = useI18n();
  const v = (key: string, params?: Record<string, string | number>) => t(`telemetryViews.replays.${key}`, params);
  const href = useOpsHref();
  const scope = useTelemetryScope();
  const env = useTelemetryEnv();
  const [app, setApp] = useUrlParam("app");
  const [query, setQuery] = useUrlParam("q");
  const [errors, setErrors] = useUrlParam("errors");
  const services = useTelemetryQuery<TelemetryService[]>("services", { range: "24h", env }, { intervalMs: 0 });
  const apps = useMemo(() => [...new Set([...(services.data ?? []).filter((s) => CLIENT_TYPES.includes(s.type)).map((s) => s.name), ...(app ? [app] : [])])].sort(), [services.data, app]);
  const read = useTelemetryQuery<{ replays: TelemetryReplaySummary[] | null }>("replays", {
    range: scope.range,
    env: scope.env,
    app: app || undefined,
    q: query.trim() || undefined,
    hasErrors: errors === "1" ? true : undefined,
    limit: 100,
  });
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "short", timeStyle: "short" });

  return (
    <div className="space-y-4">
      <TelemetryToolbar>
        <div className="relative w-64 max-w-full">
          <Search className="pointer-events-none absolute top-2.5 left-2.5 h-3.5 w-3.5 text-muted-foreground" aria-hidden />
          <Input className="h-8 pl-8" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={v("search")} aria-label={v("search")} />
        </div>
        <Select value={app || ALL} onValueChange={(x) => setApp(x === ALL ? null : x)}>
          <SelectTrigger className="h-8 w-48" aria-label={v("appFilter")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{v("allApps")}</SelectItem>
            {apps.map((a) => (
              <SelectItem key={a} value={a}>
                {a}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <FacetChips
          label={v("errorFilter")}
          items={[
            { value: "all", label: v("allSessions"), count: read.data?.replays?.length ?? 0 },
            { value: "1", label: v("withErrors"), count: (read.data?.replays ?? []).filter((r) => r.errors > 0).length },
          ]}
          active={errors === "1" ? "1" : "all"}
          onSelect={(x) => setErrors(x === "1" ? "1" : null)}
        />
      </TelemetryToolbar>
      <TelemetryQueryView query={read} isEmpty={(d) => (d.replays ?? []).length === 0} emptyTitle={v("empty")} emptyDescription={v("emptyHint")}>
        {(d) => (
          <TelemetryTable
            label={v("listLabel")}
            columns={[
              { key: "started", label: v("columns.started"), className: "w-36" },
              { key: "user", label: v("columns.user") },
              { key: "app", label: v("columns.app") },
              { key: "entry", label: v("columns.entry") },
              { key: "duration", label: v("columns.duration"), numeric: true },
              { key: "views", label: v("columns.views"), numeric: true },
              { key: "signals", label: v("columns.signals") },
            ]}
          >
            {(d.replays ?? []).map((r) => (
              <TelemetryRow key={r.sessionId}>
                <TelemetryCell className="font-mono text-micro whitespace-nowrap">
                  <Link to={href(replayHref(r.sessionId))} className="hover:underline">
                    {time.format(r.startedAt)}
                  </Link>
                </TelemetryCell>
                <TelemetryCell className="max-w-48 truncate">{replayUser(r, v("anonymous"))}</TelemetryCell>
                <TelemetryCell>
                  <span className="font-medium">{r.app}</span>
                  <p className="text-micro text-muted-foreground">{[r.platform, r.device, r.browser || r.os].filter(Boolean).join(" · ")}</p>
                </TelemetryCell>
                <TelemetryCell className="max-w-56 truncate font-mono text-micro">{r.firstView || "-"}</TelemetryCell>
                <TelemetryCell numeric>{formatDuration(r.durationMs / 1000)}</TelemetryCell>
                <TelemetryCell numeric>{r.views}</TelemetryCell>
                <TelemetryCell>
                  <span className="flex flex-wrap gap-1">
                    {r.errors > 0 && <Badge variant="destructive">{v("errors", { count: r.errors })}</Badge>}
                    {r.rageClicks > 0 && <Badge variant="warning">{v("rageClicks", { count: r.rageClicks })}</Badge>}
                  </span>
                </TelemetryCell>
              </TelemetryRow>
            ))}
          </TelemetryTable>
        )}
      </TelemetryQueryView>
    </div>
  );
}
