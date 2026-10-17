import { Inbox } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type QueueTable } from "@/api";
import type { DeployMarker } from "@/components/operations/charts/TimeSeriesChart";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { entityPath, releaseLabel, releaseTime } from "@/components/operations/console/opsEntities";
import { hasTab } from "@/components/operations/console/roleViews";
import { QueueCharts } from "@/components/operations/queues/QueueCharts";
import { QueueHealthStrip, type QueueBindTarget } from "@/components/operations/queues/QueueHealthStrip";
import { QueueTableList } from "@/components/operations/queues/QueueTableList";
import { QueuesEmptyState } from "@/components/operations/queues/QueuesEmptyState";
import {
  QUEUE_SERIES_SIGNALS,
  QUEUE_SIGNALS,
  hasQueueBinding,
  queueSuggestionCount,
  tableSignal,
} from "@/components/operations/queues/queueSignals";
import { SignalBindingEditor } from "@/components/operations/signals/SignalBindingEditor";
import { inRange, useOpsContext, useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useEntityReleases } from "@/hooks/useOpsEntityData";
import { useComponentQueues, useComponentSeries, useComponentSignals } from "@/hooks/useOpsSignals";
import { intlLocale } from "@/lib/languages";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

/** A worker's Queues: the queue golden strip and the backlog and throughput
 * over the range (deploys marked), then one table per queue source as it is
 * now, every 30 s. With no `queue.*` binding, the ways to get one. */
export default function EntityQueues({ entity, env }: EntitySubPageProps) {
  const { t, lang } = useI18n();
  const q = (key: string, params?: Record<string, string | number>) => t(`operations.queues.${key}`, params);
  const { range, compare } = useOpsContext();
  const href = useOpsHref();
  useOpsPageScope({ time: true, compare: true, env: true });

  const locale = intlLocale(lang);
  const formatCtx = useMemo(() => ({ locale, t }), [locale, t]);
  const signals = useComponentSignals(entity.id, env);
  const bound = signals.data ? hasQueueBinding(signals.data) : signals.error ? true : undefined;
  const series = useComponentSeries(entity.id, env, QUEUE_SERIES_SIGNALS, range, { compare, enabled: bound === true });
  const queues = useComponentQueues(entity.id, env, bound === true);
  const { releases } = useEntityReleases(entity, env);
  const [binding, setBinding] = useState<QueueBindTarget | null>(null);
  const [suggesting, setSuggesting] = useState(false);
  const [searchedNoneIn, setSearchedNoneIn] = useState<string | null>(null);

  const changesPath = entityPath(entity.id, "changes");
  const signalsHref = hasTab(entity.role, "signals") ? href(entityPath(entity.id, "signals")) : undefined;
  const markers = useMemo<DeployMarker[]>(
    () =>
      releases
        .filter((r) => inRange(Date.parse(releaseTime(r)), range))
        .map((r) => ({
          at: Date.parse(releaseTime(r)),
          label: releaseLabel(r),
          href: href(`${changesPath}?release=${encodeURIComponent(r.id)}`),
        })),
    [releases, range, href, changesPath],
  );
  const xRange = useMemo<[number, number]>(() => [range.from, range.to], [range]);
  const syncKey = `ops:queues:${entity.id}`;

  const reloadAll = () => {
    void signals.reload();
    void series.reload();
    void queues.reload();
  };

  const findSuggestions = async () => {
    setSuggesting(true);
    try {
      const next = await api.suggestComponentSignals(entity.id, env);
      signals.update(() => next);
      setSearchedNoneIn(queueSuggestionCount(next) === 0 ? env : null);
    } catch (err) {
      toast.error(q("empty.failed"), { description: err instanceof Error ? err.message : undefined });
    } finally {
      setSuggesting(false);
    }
  };

  const editTable = (table: QueueTable) =>
    setBinding({ signal: tableSignal(signals.data, table) ?? QUEUE_SIGNALS.backlog, current: true });

  const editor = binding && (
    <SignalBindingEditor
      open
      onOpenChange={(open) => !open && setBinding(null)}
      componentId={entity.id}
      env={env}
      signal={binding.signal}
      unit={binding.unit}
      prefillCurrent={binding.current}
      previewWindow={range}
      onSaved={reloadAll}
    />
  );

  if (bound === undefined) {
    return (
      <div className="space-y-3" aria-busy="true">
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
        <Skeleton className="h-56 w-full" />
      </div>
    );
  }

  if (!bound) {
    return (
      <>
        <QueuesEmptyState
          name={entity.name}
          suggestions={queueSuggestionCount(signals.data)}
          searchedNone={searchedNoneIn === env}
          suggesting={suggesting}
          onFindSuggestions={() => void findSuggestions()}
          onBind={() => setBinding({ signal: QUEUE_SIGNALS.backlog })}
          sourcesHref={href(DATA_SOURCES_PATH)}
          signalsHref={signalsHref}
        />
        {editor}
      </>
    );
  }

  const tables = queues.data?.queues ?? [];
  let tablesBody;
  if (queues.loading) {
    tablesBody = <Skeleton className="h-40 w-full" />;
  } else if (queues.error && !queues.data) {
    tablesBody = (
      <Card>
        <EmptyState
          icon={Inbox}
          variant="critical"
          title={q("loadFailed")}
          description={queues.error}
          action={
            <Button variant="outline" size="sm" onClick={() => void queues.reload()} disabled={queues.fetching}>
              {t("operations.chart.retry")}
            </Button>
          }
        />
      </Card>
    );
  } else if (tables.length === 0) {
    tablesBody = (
      <Card>
        <EmptyState
          icon={Inbox}
          title={q("noTables.title")}
          description={q("noTables.description", { name: entity.name })}
          action={
            signalsHref && (
              <Button variant="outline" size="sm" asChild>
                <Link to={signalsHref}>{q("noTables.action")}</Link>
              </Button>
            )
          }
        />
      </Card>
    );
  } else {
    tablesBody = (
      <QueueTableList
        componentId={entity.id}
        env={env}
        tables={tables}
        formatCtx={formatCtx}
        onEditBinding={editTable}
        onRetry={() => void queues.reload()}
        retrying={queues.fetching}
        readAt={queues.data?.read_at}
      />
    );
  }

  return (
    <div className="@container space-y-5">
      <section aria-labelledby={`${syncKey}-health`} className="space-y-2">
        <h2 id={`${syncKey}-health`} className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
          {q("health")}
        </h2>
        <QueueHealthStrip
          series={series}
          compare={compare}
          formatCtx={formatCtx}
          onBind={setBinding}
          onRetry={() => void series.reload()}
        />
      </section>

      <section aria-labelledby={`${syncKey}-charts`} className="space-y-2">
        <h2 id={`${syncKey}-charts`} className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
          {q("charts")}
        </h2>
        <QueueCharts
          series={series}
          compare={compare}
          formatCtx={formatCtx}
          deployMarkers={markers}
          xRange={xRange}
          syncKey={syncKey}
          manageHref={signalsHref}
          onBind={setBinding}
          onRetry={() => void series.reload()}
        />
      </section>

      <section aria-labelledby={`${syncKey}-tables`} className="space-y-2">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id={`${syncKey}-tables`} className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
            {q("tables")}
          </h2>
          <p className="text-micro text-muted-foreground">{q("tablesHint")}</p>
        </div>
        {queues.error && queues.data && (
          <Notice variant="warning" title={q("refreshFailed")} role="status">
            <Button variant="link" size="sm" className="h-auto p-0" onClick={() => void queues.reload()} disabled={queues.fetching}>
              {t("operations.chart.retry")}
            </Button>
          </Notice>
        )}
        {tablesBody}
      </section>
      {editor}
    </div>
  );
}
