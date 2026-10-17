import { Boxes, ScrollText, Unplug } from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { OPS_LOGS_MAX_ENVIRONMENTS, type LogSeverity } from "@/api";
import { bucketize } from "@/components/operations/charts/histogramData";
import { VolumeHistogram } from "@/components/operations/charts/VolumeHistogram";
import { entityPath, entitySectionPath, hasTab } from "@/components/operations/console/roleViews";
import { LogQueryBar } from "@/components/operations/logs/LogQueryBar";
import { LogTable } from "@/components/operations/logs/LogTable";
import { SEVERITY_STACK, isLogSeverity, severityIndex, severitySeries } from "@/components/operations/logs/severity";
import { EntityMultiSelect } from "@/components/operations/query/EntityMultiSelect";
import { entityIndex, environmentIndex, formatIdList, parseIdList, windowKey } from "@/components/operations/query/opsQuery";
import { UnavailableNotice } from "@/components/operations/query/UnavailableNotice";
import { OPS_AUTO_ADVANCE_MS, useOpsContext, useOpsHref } from "@/components/operations/shell/OpsContext";
import { useOpsEntities, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { useOpsLogs } from "@/hooks/useOpsLogs";
import { formatDate } from "@/lib/utils";

interface LogsExplorerProps {
  /** One entity's logs: no entity filter, its component only. */
  entity?: OpsEntity;
  /** The entity has a confirmed logs.lines binding, which is read with no environment connected. */
  bindingRead?: boolean;
}

function iso(ms: number): string {
  return new Date(ms).toISOString();
}

/** New Relic's Logs: a query line, the volume of what was loaded by
 * severity, and the entries newest first with "load older" and a live tail. */
export function LogsExplorer({ entity: scoped, bindingRead = false }: LogsExplorerProps) {
  const { t } = useI18n();
  const l = (key: string, params?: Record<string, string | number>) => t(`operations.logsExplorer.${key}`, params);
  const { range, env } = useOpsContext();
  const href = useOpsHref();
  const [params, setParams] = useSearchParams();
  const { entities: listed, loading: entitiesLoading } = useOpsEntities();
  const [live, setLive] = useState(false);

  const entities = useMemo(
    () => (scoped && !listed.some((x) => x.id === scoped.id) ? [...listed, scoped] : listed),
    [listed, scoped],
  );
  const byId = useMemo(() => entityIndex(entities), [entities]);
  const environments = useMemo(() => environmentIndex(entities), [entities]);

  const entityParam = params.get("entity");
  const picked = useMemo(() => (scoped ? [scoped.id] : parseIdList(entityParam)), [scoped, entityParam]);
  const text = params.get("q") ?? "";
  const severityParam = params.get("severity");
  const minSeverity: LogSeverity | undefined = isLogSeverity(severityParam) ? severityParam : undefined;
  const envLabel = t(`cloud.environments.${env}`);

  const inScope = scoped ? [scoped] : picked.length > 0 ? entities.filter((x) => picked.includes(x.id)) : entities;
  const connected = inScope.some((x) => x.environments.some((v) => v.environment === env));
  // The console-wide explorer always reads: the server also reads the
  // components that only a `logs.lines` binding (a Loki stream) feeds.
  const enabled = !scoped || bindingRead || connected;
  // A custom range that ended in the past has nothing new to follow.
  const pastRange = range.preset === "custom" && range.to < Date.now() - OPS_AUTO_ADVANCE_MS;
  const tailing = live && !pastRange;

  const logs = useOpsLogs(
    {
      env,
      from: range.from,
      to: range.to,
      preset: range.preset !== "custom",
      window: windowKey(range),
      componentIds: picked,
      minSeverity,
      text,
    },
    { enabled, live: tailing },
  );

  const series = useMemo(() => severitySeries((s) => t(`operations.opsQuery.severity.${s}`)), [t]);
  const buckets = useMemo(
    () =>
      bucketize(
        logs.entries,
        (e) => Date.parse(e.timestamp),
        (e) => severityIndex(e.severity),
        SEVERITY_STACK.length,
        logs.window.from,
        logs.window.to,
      ),
    [logs.entries, logs.window.from, logs.window.to],
  );

  const setParam = (key: string, value: string | null) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value === null || value === "") next.delete(key);
        else next.set(key, value);
        return next;
      },
      { replace: true },
    );
  const pickEntities = (ids: string[]) => setParam("entity", formatIdList(ids));
  const entityHref = scoped ? undefined : (x: OpsEntity) => href(entitySectionPath(x, "logs"));
  const traceHref = (traceId: string, x: OpsEntity | undefined) =>
    x && hasTab(x.role, "traces") ? href(`${entityPath(x.id, "traces")}?trace=${encodeURIComponent(traceId)}`) : undefined;

  if (!scoped && entitiesLoading && listed.length === 0) {
    return (
      <div className="space-y-3" aria-busy>
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-36 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
    );
  }

  const bar = (
    <LogQueryBar
      text={text}
      onText={(next) => setParam("q", next)}
      minSeverity={minSeverity}
      onMinSeverity={(s) => setParam("severity", s ?? null)}
      entityFilter={
        scoped
          ? undefined
          : {
              entities: listed,
              selected: picked,
              onChange: pickEntities,
              limit: OPS_LOGS_MAX_ENVIRONMENTS,
              required: logs.status === "tooMany",
            }
      }
      live={tailing}
      onLive={setLive}
      liveDisabled={logs.status !== "ready" || pastRange}
      liveHint={pastRange ? l("livePast") : undefined}
    />
  );

  const nothingConnected = (
    <Card>
      <EmptyState
        icon={Unplug}
        title={l("noEnvironments.title", { env: envLabel })}
        description={l("noEnvironments.body")}
        action={
          <Button variant="outline" size="sm" asChild>
            <Link to={href("/operations")}>{l("noEnvironments.action")}</Link>
          </Button>
        }
      />
    </Card>
  );
  if (!enabled) return nothingConnected;

  if (logs.status === "tooMany" && logs.tooMany) {
    return (
      <div className="space-y-4">
        {bar}
        <Card>
          <EmptyState
            icon={Boxes}
            title={l("tooMany.title")}
            description={l("tooMany.body", { matched: logs.tooMany.matched, env: envLabel, max: logs.tooMany.max })}
            action={<EntityMultiSelect entities={listed} selected={picked} onChange={pickEntities} limit={logs.tooMany.max} required />}
          />
        </Card>
      </div>
    );
  }

  const filtered = text.trim() !== "" || minSeverity !== undefined;
  let list;
  if (logs.status === "loading") {
    list = (
      <Card className="space-y-1.5 p-3" aria-busy>
        {Array.from({ length: 10 }, (_, i) => (
          <Skeleton key={i} className="h-6 w-full" />
        ))}
      </Card>
    );
  } else if (logs.status === "error") {
    list = (
      <Notice variant="error" title={l("loadFailed")}>
        {logs.error}
      </Notice>
    );
  } else if (logs.entries.length === 0 && !connected && !bindingRead && !filtered && logs.unavailable.length === 0) {
    list = nothingConnected;
  } else if (logs.entries.length === 0) {
    list = (
      <Card>
        <EmptyState
          icon={ScrollText}
          title={l("empty.title")}
          description={
            filtered
              ? l("empty.filtered")
              : l("empty.body", { env: envLabel, from: formatDate(iso(logs.window.from)), to: formatDate(iso(logs.window.to)) })
          }
        />
      </Card>
    );
  } else {
    list = (
      <div className="space-y-2">
        <LogTable
          entries={logs.entries}
          environments={environments}
          entities={byId}
          showEntity={!scoped}
          entityHref={entityHref}
          traceHref={traceHref}
        />
        <div className="flex min-h-9 flex-wrap items-center justify-center gap-3 text-caption text-muted-foreground">
          {logs.older === "more" && (
            <Button variant="outline" size="sm" onClick={() => void logs.loadOlder()} disabled={logs.loadingOlder}>
              {logs.loadingOlder && <Spinner size="sm" />}
              {logs.loadingOlder ? l("loadingOlder") : l("loadOlder")}
            </Button>
          )}
          {logs.older === "none" && <span>{l("startOfRange")}</span>}
          {logs.older === "stuck" && <span>{l("stuck")}</span>}
        </div>
        {logs.olderError && (
          <Notice variant="warning" title={l("olderFailed")}>
            {logs.olderError}
          </Notice>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {bar}
      {logs.error && logs.status === "ready" && (
        <Notice variant="warning" title={l("refreshFailed")}>
          {logs.error}
        </Notice>
      )}
      <UnavailableNotice unavailable={logs.unavailable} entities={entities} />
      {logs.truncated && logs.older !== "none" && (
        <Notice variant="info" title={l("truncated.title")}>
          {l("truncated.body")}
        </Notice>
      )}
      {logs.tailGap && (
        <Notice variant="warning" title={l("tailGap.title")}>
          <span>{l("tailGap.body")} </span>
          <Button variant="link" size="sm" className="h-auto p-0" onClick={() => void logs.reload()}>
            {l("tailGap.action")}
          </Button>
        </Notice>
      )}
      {tailing && logs.tailError && (
        <Notice variant="warning" title={l("tailFailed")}>
          {logs.tailError}
        </Notice>
      )}
      <Card className="space-y-1 p-3">
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
          <h2 className="text-caption font-semibold">{l("histogram.title")}</h2>
          <p className="text-micro text-muted-foreground tabular-nums">
            {l("histogram.note", { count: logs.entries.length })}
          </p>
        </div>
        <VolumeHistogram
          buckets={buckets}
          series={series}
          ariaLabel={l("histogram.aria", { count: logs.entries.length })}
          loading={logs.status === "loading"}
          emptyLabel={l("histogram.empty")}
          showTableToggle
        />
      </Card>
      {list}
    </div>
  );
}
