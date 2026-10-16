import { Database, Gauge, Sparkles } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, dataSourceFieldErrors, type ComponentSignal, type MetricPoint, type SignalBinding } from "@/api";
import { headlinePoints } from "@/components/operations/charts/signalChart";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { signalLabel } from "@/components/operations/console/signalFormat";
import { useOpsContext, useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import {
  applyBinding,
  applyDismiss,
  applyRemove,
  metricsSources,
  signalWriteError,
  suggestionNeedsEditor,
  type BindingPrefill,
} from "@/components/operations/signals/signalBinding";
import { SignalBindingDialog } from "@/components/operations/signals/SignalBindingDialog";
import { SignalsTable } from "@/components/operations/signals/SignalsTable";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useDataSources, useRecipeCatalog } from "@/hooks/useDataSources";
import { useI18n } from "@/hooks/useI18n";
import { useComponentSeries, useComponentSignals } from "@/hooks/useOpsSignals";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

interface EditorTarget {
  signal: ComponentSignal;
  prefill: BindingPrefill | null;
}


/** An entity's Signals: which data source feeds each of its role's signals in
 * this environment, the suggestions TaskTrooper found, and the editor. */
export default function EntitySignals({ entity, env }: EntitySubPageProps) {
  const { t } = useI18n();
  const p = (key: string, params?: Record<string, string | number>) => t(`operations.signalsPage.${key}`, params);
  const { range } = useOpsContext();
  const href = useOpsHref();
  useOpsPageScope({ time: true, compare: false, env: true });

  const signals = useComponentSignals(entity.id, env);
  const { kinds, sources, loading: sourcesLoading } = useDataSources();
  const catalog = useRecipeCatalog();
  const metrics = useMemo(() => metricsSources(sources), [sources]);
  const rows = useMemo(() => signals.data?.signals ?? [], [signals.data]);
  const bound = useMemo(() => rows.filter((s) => s.binding).map((s) => s.signal), [rows]);
  const series = useComponentSeries(entity.id, env, bound, range);
  const sparks = useMemo(() => {
    const out = new Map<string, MetricPoint[]>();
    for (const s of series.data?.signals ?? []) if (!s.error) out.set(s.signal, headlinePoints(s.series));
    return out;
  }, [series.data]);

  const [editor, setEditor] = useState<EditorTarget | null>(null);
  const [removing, setRemoving] = useState<ComponentSignal | null>(null);
  const [busy, setBusy] = useState<ReadonlySet<string>>(() => new Set());
  const [suggesting, setSuggesting] = useState(false);

  const work = async (id: string, fn: () => Promise<void>) => {
    setBusy((prev) => new Set(prev).add(id));
    try {
      await fn();
    } finally {
      setBusy((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  };

  // A coded refusal in the reader's language (and a fresh list when the
  // write met a list that had moved on); the server's own sentence otherwise.
  const refused = (err: unknown) => {
    const coded = signalWriteError(err);
    if (coded?.reload) void signals.reload();
    toast.error(coded ? t(coded.key) : err instanceof Error ? err.message : t("common.actionFailed"));
  };

  const saved = (binding: SignalBinding) => {
    // A first binding adds the signal to the series read, whose new key reads
    // on its own; only a rebound signal needs its window read again.
    const rebound = bound.includes(binding.signal);
    signals.update((prev) => (prev ? applyBinding(prev, binding) : prev));
    toast.success(p("saved", { signal: signalLabel(t, binding.signal) }));
    void signals.reload();
    if (rebound) void series.reload();
  };

  const findSuggestions = async () => {
    setSuggesting(true);
    try {
      const next = await api.suggestComponentSignals(entity.id, env);
      signals.update(() => next);
      const count = (next.signals ?? []).reduce((n, s) => n + (s.suggestions ?? []).length, 0);
      if (count === 0) toast.info(p("suggestNone"));
      else toast.success(p("suggestFound", { count }));
    } catch (err) {
      refused(err);
    } finally {
      setSuggesting(false);
    }
  };

  const accept = (signal: ComponentSignal, suggestion: SignalBinding) => {
    if (suggestionNeedsEditor(suggestion)) {
      setEditor({ signal, prefill: suggestion });
      return;
    }
    void work(suggestion.id, async () => {
      try {
        const binding = await api.confirmSignalBinding(suggestion.id);
        saved(binding);
      } catch (err) {
        const fields = dataSourceFieldErrors(err);
        if (fields && Object.keys(fields).some((key) => key.startsWith("params."))) {
          setEditor({ signal, prefill: suggestion });
          return;
        }
        refused(err);
      }
    });
  };

  const dismiss = (signal: ComponentSignal, suggestion: SignalBinding) =>
    void work(suggestion.id, async () => {
      try {
        await api.dismissSignalBinding(suggestion.id);
        signals.update((prev) => (prev ? applyDismiss(prev, signal.signal, suggestion.id) : prev));
      } catch (err) {
        refused(err);
      }
    });

  const confirmRemove = async () => {
    const target = removing;
    if (!target?.binding) return;
    const id = target.binding.id;
    await work(target.signal, async () => {
      try {
        await api.deleteSignalBinding(id);
        signals.update((prev) => (prev ? applyRemove(prev, target.signal) : prev));
        toast.success(p("removed", { signal: signalLabel(t, target.signal) }));
      } catch (err) {
        refused(err);
      }
    });
    setRemoving(null);
  };

  const noSources = !sourcesLoading && metrics.length === 0;
  const envLabel = t(`cloud.environments.${env}`);

  let body;
  if (signals.loading && rows.length === 0) {
    body = <Skeleton className="h-64 w-full" />;
  } else if (signals.error && !signals.data) {
    body = (
      <Card>
        <EmptyState
          icon={Gauge}
          variant="critical"
          title={p("loadFailed")}
          description={signals.error}
          action={
            <Button variant="outline" size="sm" onClick={() => void signals.reload()}>
              {t("operations.chart.retry")}
            </Button>
          }
        />
      </Card>
    );
  } else if (rows.length === 0) {
    body = (
      <Card>
        <EmptyState icon={Gauge} title={p("empty")} />
      </Card>
    );
  } else {
    body = (
      <SignalsTable
        signals={rows}
        sparks={sparks}
        busy={busy}
        onBind={(s) => setEditor({ signal: s, prefill: s.binding })}
        onRemove={setRemoving}
        onAccept={accept}
        onDismiss={dismiss}
      />
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <h2 className="text-heading font-semibold">{p("title")}</h2>
          <p className="max-w-prose text-caption text-muted-foreground">{p("description", { name: entity.name, env: envLabel })}</p>
        </div>
        {!noSources && (
          <Button variant="outline" onClick={() => void findSuggestions()} disabled={suggesting || rows.length === 0}>
            <Sparkles aria-hidden />
            {suggesting ? p("suggesting") : p("suggest")}
          </Button>
        )}
      </div>

      {signals.error && signals.data && (
        <Notice variant="warning" title={p("refreshFailed")}>
          {signals.error}
        </Notice>
      )}

      {noSources ? (
        <Card>
          <EmptyState
            icon={Database}
            title={p("noSources.title")}
            description={p("noSources.description")}
            action={
              <Button variant="outline" size="sm" asChild>
                <Link to={href(DATA_SOURCES_PATH)}>{p("noSources.action")}</Link>
              </Button>
            }
          />
        </Card>
      ) : (
        body
      )}

      {editor && (
        <SignalBindingDialog
          open
          onOpenChange={(open) => !open && setEditor(null)}
          componentId={entity.id}
          env={env}
          signal={editor.signal.signal}
          unit={editor.signal.unit}
          sources={sources}
          kinds={kinds}
          recipes={catalog.recipes}
          prefill={editor.prefill}
          previewWindow={range}
          onSaved={saved}
        />
      )}

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title={p("removeTitle", { signal: removing ? signalLabel(t, removing.signal) : "" })}
        description={p("removeDescription", { env: envLabel })}
        confirmLabel={p("remove")}
        variant="destructive"
        loading={removing !== null && busy.has(removing.signal)}
        onConfirm={confirmRemove}
      />
    </div>
  );
}
