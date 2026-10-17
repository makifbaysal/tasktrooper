import { Route, SearchX } from "lucide-react";
import { useEffect, useId, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { signalErrorView } from "@/components/operations/charts/signalChart";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { entityPath, hasTab } from "@/components/operations/console/roleViews";
import { entityIndex } from "@/components/operations/query/opsQuery";
import { useOpsContext, useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useOpsEntities } from "@/components/operations/shell/useOpsEntities";
import { SignalBindingEditor } from "@/components/operations/signals/SignalBindingEditor";
import { TraceDetail } from "@/components/operations/traces/TraceDetail";
import { TraceSearchList } from "@/components/operations/traces/TraceSearchList";
import { normalizeTraceId, parseMinDuration, traceWindow } from "@/components/operations/traces/waterfallLayout";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { OPS_TRACES_LIMIT, useComponentTraces } from "@/hooks/useOpsTraces";
import { formatDate } from "@/lib/utils";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

export const TRACES_SIGNAL = "traces.spans";

/** An entity's Traces: a trace search over the range through its
 * traces.spans binding (newest or slowest first, a minimum duration), and
 * one trace's waterfall at `?trace=<id>`. */
export default function EntityTraces({ entity, env }: EntitySubPageProps) {
  const { t } = useI18n();
  const p = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.traces.${key}`, params);
  const { range } = useOpsContext();
  const href = useOpsHref();
  const minId = useId();
  useOpsPageScope({ time: true, compare: false, env: true });
  const [params, setParams] = useSearchParams();
  const traceId = normalizeTraceId(params.get("trace"));
  const min = parseMinDuration(params.get("min"));
  const [minText, setMinText] = useState(min ? String(min) : "");
  const [binding, setBinding] = useState<{ current: boolean } | null>(null);
  const { entities } = useOpsEntities();
  const byId = useMemo(() => entityIndex(entities), [entities]);
  const read = useComponentTraces(entity.id, env, range, { minDurationMs: min, enabled: !traceId });
  const win = traceWindow(range);
  const signalsHref = hasTab(entity.role, "signals") ? href(entityPath(entity.id, "signals")) : undefined;

  useEffect(() => setMinText(min ? String(min) : ""), [min]);

  const withParam = (key: string, value: string | null) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    const qs = next.toString();
    return qs ? `?${qs}` : "?";
  };
  const applyMin = (raw: string) => {
    const parsed = parseMinDuration(raw);
    if (parsed === min) return;
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (parsed) next.set("min", String(parsed));
        else next.delete("min");
        return next;
      },
      { replace: true },
    );
  };

  if (traceId) {
    return <TraceDetail traceId={traceId} componentId={entity.id} env={env} backHref={withParam("trace", null)} entities={byId} />;
  }

  const editor = binding && (
    <SignalBindingEditor
      open
      onOpenChange={(open) => !open && setBinding(null)}
      componentId={entity.id}
      env={env}
      signal={TRACES_SIGNAL}
      prefillCurrent={binding.current}
      previewWindow={range}
      onSaved={() => void read.reload()}
    />
  );

  const filters = (
    <div className="flex flex-wrap items-end gap-3">
      <div className="space-y-1">
        <Label htmlFor={minId} className="text-caption">
          {p("minDuration")}
        </Label>
        <Input
          id={minId}
          inputMode="numeric"
          value={minText}
          onChange={(e) => setMinText(e.target.value)}
          onBlur={() => applyMin(minText)}
          onKeyDown={(e) => {
            if (e.key === "Enter") applyMin(minText);
          }}
          placeholder="0"
          className="h-8 w-32 font-mono text-caption"
          aria-describedby={`${minId}-hint`}
        />
      </div>
      <p id={`${minId}-hint`} className="pb-1.5 text-micro text-muted-foreground">
        {p("minDurationHint")}
      </p>
    </div>
  );

  let body;
  const data = read.data;
  if (read.loading) {
    body = (
      <Card className="space-y-2 p-3" aria-busy>
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </Card>
    );
  } else if (read.error && !data) {
    body = (
      <Card>
        <EmptyState
          icon={Route}
          variant="critical"
          title={p("loadFailed")}
          description={read.error}
          action={
            <Button variant="outline" size="sm" onClick={() => void read.reload()} disabled={read.fetching}>
              {t("operations.chart.retry")}
            </Button>
          }
        />
      </Card>
    );
  } else if (data && !data.bound) {
    body = (
      <Card>
        <EmptyState
          icon={Route}
          title={p("unbound.title")}
          description={p("unbound.body", { name: entity.name })}
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button size="sm" onClick={() => setBinding({ current: false })}>
                {p("unbound.bind")}
              </Button>
              <Button size="sm" variant="outline" asChild>
                <Link to={href(DATA_SOURCES_PATH)}>{p("unbound.addSource")}</Link>
              </Button>
            </div>
          }
        />
      </Card>
    );
  } else if (data?.error) {
    const view = signalErrorView(data.error.code);
    body = (
      <Card>
        <EmptyState
          icon={Route}
          variant={view.action === "none" ? "default" : "critical"}
          title={t(`operations.chart.errors.${view.key}`)}
          description={view.body ? t(`operations.chart.errors.${view.key}Body`) : data.error.message || undefined}
          action={
            view.action === "edit" ? (
              <Button size="sm" onClick={() => setBinding({ current: true })}>
                {t(`operations.chart.errors.${view.actionKey}`)}
              </Button>
            ) : view.action === "retry" ? (
              <Button variant="outline" size="sm" onClick={() => void read.reload()} disabled={read.fetching}>
                {t("operations.chart.retry")}
              </Button>
            ) : undefined
          }
        />
      </Card>
    );
  } else if ((data?.traces ?? []).length === 0) {
    body = (
      <Card>
        <EmptyState
          icon={SearchX}
          variant="search"
          title={p("empty.title")}
          description={min ? p("empty.withMin", { min }) : p("empty.body", { since: formatDate(new Date(win.since).toISOString()) })}
          action={
            min ? (
              <Button variant="outline" size="sm" onClick={() => applyMin("")}>
                {p("empty.clearMin")}
              </Button>
            ) : undefined
          }
        />
      </Card>
    );
  } else {
    const traces = data?.traces ?? [];
    body = (
      <div className="space-y-2">
        <TraceSearchList traces={traces} hrefFor={(tr) => withParam("trace", tr.id)} />
        {traces.length >= OPS_TRACES_LIMIT && <p className="text-micro text-muted-foreground">{p("capped", { count: traces.length })}</p>}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        {filters}
        {signalsHref && data?.bound && (
          <Button variant="link" size="sm" className="h-auto p-0 text-caption" asChild>
            <Link to={signalsHref}>{p("manage")}</Link>
          </Button>
        )}
      </div>
      {win.clamped && (
        <Notice variant="info" title={p("clamped.title")}>
          {p("clamped.body", { since: formatDate(new Date(win.since).toISOString()) })}
        </Notice>
      )}
      {read.error && data && (
        <Notice variant="warning" title={p("refreshFailed")}>
          {read.error}
        </Notice>
      )}
      {body}
      {editor}
    </div>
  );
}
