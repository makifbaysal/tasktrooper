import { Copy, OctagonAlert } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { SpanDetail } from "@/components/operations/traces/SpanDetail";
import { SpanTable } from "@/components/operations/traces/SpanTable";
import { serviceBg, TraceWaterfall } from "@/components/operations/traces/TraceWaterfall";
import { buildWaterfall, formatSpanDuration } from "@/components/operations/traces/waterfallLayout";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { copyText } from "@/lib/clipboard";
import { intlLocale } from "@/lib/languages";
import { toConsoleTrace, type TelemetryTrace } from "@/lib/telemetryHub";
import { cn, formatDate } from "@/lib/utils";

type View = "waterfall" | "table";

/** One trace from the telemetry product on the console's own waterfall,
 * span table and span panel; only the data is the product's. */
export function TelemetryTraceView({ trace }: { trace: TelemetryTrace }) {
  const { t, lang } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.traces.detail.${key}`, params);
  const locale = intlLocale(lang);
  const wf = useMemo(() => buildWaterfall(toConsoleTrace(trace)), [trace]);
  const [view, setView] = useState<View>("waterfall");
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined);
  const selected = wf.rows.find((r) => r.span.id === selectedId);
  const root = wf.rows[0];
  const noService = t("operations.telemetry.traces.waterfall.noService");

  const copyId = async () => {
    if (await copyText(trace.id)) toast.success(d("copied"));
    else toast.error(t("common.actionFailed"));
  };

  return (
    <div className="@container space-y-4">
      <Card className="space-y-3 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 space-y-1">
            <h2 className="font-mono text-heading font-semibold break-all">{root?.span.name || d("untitled")}</h2>
            <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
              <span>{root?.span.service || noService}</span>
              <span className="inline-flex items-center gap-1 font-mono">
                {trace.id}
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
                <span>{svc.name || noService}</span>
                <span className="text-muted-foreground tabular-nums">{d("serviceSpans", { count: svc.spans })}</span>
                {svc.errors > 0 && <span className="text-destructive tabular-nums">{d("serviceErrors", { count: svc.errors })}</span>}
              </li>
            ))}
          </ul>
        )}
      </Card>

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
