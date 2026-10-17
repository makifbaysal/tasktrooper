import { ArrowLeft, Copy, OctagonAlert, Route } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import type { DeployEnvironment } from "@/api";
import { entityPath } from "@/components/operations/console/roleViews";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { SpanDetail } from "@/components/operations/traces/SpanDetail";
import { SpanTable } from "@/components/operations/traces/SpanTable";
import { buildWaterfall, formatSpanDuration } from "@/components/operations/traces/waterfallLayout";
import { serviceBg, TraceWaterfall } from "@/components/operations/traces/TraceWaterfall";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useTraceView } from "@/hooks/useOpsTraces";
import { copyText } from "@/lib/clipboard";
import { intlLocale } from "@/lib/languages";
import { cn, formatDate } from "@/lib/utils";

interface TraceDetailProps {
  traceId: string;
  /** The component whose binding the trace is read through first. */
  componentId?: string;
  env: DeployEnvironment;
  backHref: string;
  /** Names the component a trace found through another one's binding belongs to. */
  entities?: Map<string, OpsEntity>;
}

type View = "waterfall" | "table";

/** One trace: its facts and services, the span waterfall (or the same as a
 * table) and the span picked, beside it from `@4xl`. */
export function TraceDetail({ traceId, componentId, env, backHref, entities }: TraceDetailProps) {
  const { t, lang } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.traces.detail.${key}`, params);
  const href = useOpsHref();
  const locale = intlLocale(lang);
  const read = useTraceView(componentId, traceId, env);
  const waterfall = useMemo(() => (read.view ? buildWaterfall(read.view.trace) : undefined), [read.view]);
  const [view, setView] = useState<View>("waterfall");
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined);
  const selected = waterfall?.rows.find((r) => r.span.id === selectedId);
  const root = waterfall?.rows[0];
  const owner = read.foundElsewhere && read.view ? entities?.get(read.view.component_id) : undefined;

  const back = (
    <Button variant="ghost" size="sm" className="-ml-2 w-fit" asChild>
      <Link to={backHref}>
        <ArrowLeft aria-hidden />
        {d("back")}
      </Link>
    </Button>
  );

  const copyId = async () => {
    if (await copyText(traceId)) toast.success(d("copied"));
    else toast.error(t("common.actionFailed"));
  };

  if (!read.view) {
    if (read.status === "loading") {
      return (
        <div className="space-y-3" aria-busy="true">
          {back}
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-72 w-full" />
        </div>
      );
    }
    const key = read.status === "notFound" ? "notFound" : read.status === "unbound" ? "unbound" : read.status === "timeout" ? "timeout" : "failed";
    return (
      <div className="space-y-3">
        {back}
        <Card>
          <EmptyState
            icon={Route}
            variant={read.status === "notFound" || read.status === "unbound" ? "default" : "critical"}
            title={d(`errors.${key}`)}
            description={key === "failed" ? read.error : d(`errors.${key}Body`, { id: traceId })}
            action={
              key === "timeout" || key === "failed" ? (
                <Button variant="outline" size="sm" onClick={read.reload}>
                  {t("operations.chart.retry")}
                </Button>
              ) : undefined
            }
          />
        </Card>
      </div>
    );
  }

  const trace = read.view.trace;
  const wf = waterfall!;

  return (
    <div className="@container space-y-4">
      {back}
      <Card className="space-y-3 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 space-y-1">
            <h2 className="font-mono text-heading font-semibold break-all">{root?.span.name || d("untitled")}</h2>
            <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
              <span>{root?.span.service || t("operations.telemetry.traces.waterfall.noService")}</span>
              <span className="inline-flex items-center gap-1 font-mono">
                {traceId}
                <Button variant="ghost" size="icon" className="h-6 w-6 [&_svg]:size-3.5" onClick={() => void copyId()} aria-label={d("copy")} title={d("copy")}>
                  <Copy aria-hidden />
                </Button>
              </span>
              {root && <span className="font-mono tabular-nums">{formatDate(root.span.start)}</span>}
            </p>
          </div>
          <dl className="flex flex-wrap gap-4 text-caption">
            <div>
              <dt className="text-micro tracking-wide text-muted-foreground uppercase">{d("duration")}</dt>
              <dd className="font-mono text-title tabular-nums">{formatSpanDuration(wf.durationMs, locale)}</dd>
            </div>
            <div>
              <dt className="text-micro tracking-wide text-muted-foreground uppercase">{d("spans")}</dt>
              <dd className="font-mono text-title tabular-nums">{wf.rows.length}</dd>
            </div>
            <div>
              <dt className="text-micro tracking-wide text-muted-foreground uppercase">{d("errorCount")}</dt>
              <dd className={cn("font-mono text-title tabular-nums", wf.errors > 0 && "text-destructive")}>
                {wf.errors > 0 && <OctagonAlert className="mr-1 inline h-4 w-4 align-[-2px]" aria-hidden />}
                {wf.errors}
              </dd>
            </div>
          </dl>
        </div>
        {wf.services.length > 0 && (
          <ul className="flex flex-wrap gap-x-3 gap-y-1 text-micro" aria-label={d("services")}>
            {wf.services.map((svc) => (
              <li key={svc.name} className="inline-flex items-center gap-1.5">
                <span className={cn("h-2 w-2 rounded-full", serviceBg(svc.colorIndex))} aria-hidden />
                <span>{svc.name || t("operations.telemetry.traces.waterfall.noService")}</span>
                <span className="text-muted-foreground tabular-nums">{d("serviceSpans", { count: svc.spans })}</span>
                {svc.errors > 0 && <span className="text-destructive tabular-nums">{d("serviceErrors", { count: svc.errors })}</span>}
              </li>
            ))}
          </ul>
        )}
      </Card>

      {trace.truncated && (
        <Notice variant="info" title={d("truncated.title")}>
          {d("truncated.body", { count: wf.rows.length })}
        </Notice>
      )}
      {owner && (
        <Notice variant="info" title={d("elsewhere.title")} role="status">
          <span>{d("elsewhere.body", { name: owner.name })} </span>
          <Link to={href(`${entityPath(owner.id, "traces")}?trace=${encodeURIComponent(traceId)}`)} className="font-medium text-info hover:underline">
            {d("elsewhere.action", { name: owner.name })}
          </Link>
        </Notice>
      )}
      {read.status !== "ready" && read.status !== "loading" && (
        <Notice variant="warning" title={d("refreshFailed")}>
          {read.error}
        </Notice>
      )}

      <div className="flex flex-wrap items-center justify-between gap-2">
        <Tabs value={view} onValueChange={(v) => setView(v as View)} variant="pill" className="w-fit">
          <TabsList aria-label={d("viewLabel")}>
            <TabsTrigger value="waterfall">{d("views.waterfall")}</TabsTrigger>
            <TabsTrigger value="table">{d("views.table")}</TabsTrigger>
          </TabsList>
        </Tabs>
        <p className="text-micro text-muted-foreground">{d("hint")}</p>
      </div>

      {wf.rows.length === 0 ? (
        <Card className="px-4 py-6 text-center text-caption text-muted-foreground">{d("noSpans")}</Card>
      ) : (
        <div className={cn("grid gap-4", selected && "@4xl:grid-cols-[minmax(0,1fr)_22rem]")}>
          <div className="min-w-0">
            {view === "waterfall" ? (
              <TraceWaterfall waterfall={wf} selectedId={selectedId} onSelect={setSelectedId} />
            ) : (
              <SpanTable waterfall={wf} selectedId={selectedId} onSelect={setSelectedId} />
            )}
          </div>
          {selected && (
            <SpanDetail
              row={selected}
              onClose={() => setSelectedId(undefined)}
              className="order-first @4xl:order-none @4xl:sticky @4xl:top-0 @4xl:self-start"
            />
          )}
        </div>
      )}
    </div>
  );
}
