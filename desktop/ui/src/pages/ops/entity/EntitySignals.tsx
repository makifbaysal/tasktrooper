import { Bot, Database, Gauge, Sparkles } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, dataSourceFieldErrors, type ComponentSignal, type DataSource, type DataSourceOffer, type MetricPoint, type SignalBinding } from "@/api";
import { AIJobCard } from "@/components/operations/ai/AIJobCard";
import { isJobRunning } from "@/components/operations/ai/aiJobs";
import { headlinePoints } from "@/components/operations/charts/signalChart";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { signalLabel } from "@/components/operations/console/signalFormat";
import { useOpsContext, useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import {
  anyBindableSource,
  applyBinding,
  applyDismiss,
  applyRemove,
  signalWriteError,
  suggestionNeedsEditor,
  type BindingPrefill,
} from "@/components/operations/signals/signalBinding";
import { SignalBindingDialog } from "@/components/operations/signals/SignalBindingDialog";
import { SignalsTable } from "@/components/operations/signals/SignalsTable";
import { offerBindings, offerInitial, offerKey, uniqueOffers } from "@/components/operations/signals/sourceOffers";
import { SourceOfferCards } from "@/components/operations/signals/SourceOfferCards";
import { DataSourceForm } from "@/components/operations/sources/DataSourceForm";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useAIJob } from "@/hooks/useAIJob";
import { useDataSources, useRecipeCatalog } from "@/hooks/useDataSources";
import { useI18n } from "@/hooks/useI18n";
import { useComponentSeries, useComponentSignals } from "@/hooks/useOpsSignals";
import { useStoreCredentials } from "@/hooks/useStoreCredentials";
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
  const { kinds, sources, loading: sourcesLoading, upsert } = useDataSources();
  const catalog = useRecipeCatalog();
  const store = useStoreCredentials();
  const [offers, setOffers] = useState<DataSourceOffer[]>([]);
  const [offerForm, setOfferForm] = useState<DataSourceOffer | null>(null);
  const [connecting, setConnecting] = useState<string | undefined>(undefined);
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
  const mapJob = useAIJob(`operations.aiJob.mapSignals.${entity.id}.${env}`, () => {
    void signals.reload();
    void series.reload();
  });

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
      const found = uniqueOffers(next.source_offers);
      setOffers(found);
      const count = (next.signals ?? []).reduce((n, s) => n + (s.suggestions ?? []).length, 0);
      if (count === 0 && found.length === 0) toast.info(p("suggestNone"));
      else if (count === 0) toast.success(t("operations.telemetry.offers.found", { count: found.length }));
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

  // The offered source exists now: bind what it was offered for, the way a
  // person would on this page, then show the list as the server has it.
  const bindOffer = async (offer: DataSourceOffer, source: DataSource) => {
    upsert(source);
    const key = offerKey(offer);
    setConnecting(key);
    const results = await Promise.allSettled(
      offerBindings(offer, source.id).map(({ signal, body }) => api.bindComponentSignal(entity.id, signal, env, body)),
    );
    const bound = results.filter((r) => r.status === "fulfilled").length;
    const failure = results.find((r): r is PromiseRejectedResult => r.status === "rejected");
    setConnecting(undefined);
    setOffers((prev) => prev.filter((o) => offerKey(o) !== key));
    if (bound > 0) toast.success(t("operations.telemetry.offers.bound", { count: bound, name: source.name }));
    if (failure) refused(failure.reason);
    void signals.reload();
    void series.reload();
  };

  const noSources = !sourcesLoading && !anyBindableSource(sources);
  const offerKind = offerForm ? kinds.find((k) => k.kind === offerForm.kind) : undefined;
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
        {rows.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" onClick={() => void findSuggestions()} disabled={suggesting}>
              <Sparkles aria-hidden />
              {suggesting ? p("suggesting") : p("suggest")}
            </Button>
            {!noSources && (
              <Button
                variant="outline"
                onClick={() => void mapJob.start(() => api.suggestComponentSignalsAI(entity.id, env))}
                disabled={mapJob.starting || isJobRunning(mapJob.job)}
              >
                <Bot aria-hidden />
                {t("operations.aiJob.mapSignals.button")}
              </Button>
            )}
          </div>
        )}
      </div>

      <AIJobCard
        job={mapJob.job}
        refusal={mapJob.refusal}
        startError={mapJob.startError}
        pollError={mapJob.pollError}
        onDismiss={mapJob.dismiss}
        actions={
          mapJob.job?.status === "done" && (mapJob.job.created ?? []).length > 0 ? (
            <p className="text-caption text-muted-foreground">{t("operations.aiJob.mapSignals.reviewHint")}</p>
          ) : undefined
        }
      />

      {signals.error && signals.data && (
        <Notice variant="warning" title={p("refreshFailed")}>
          {signals.error}
        </Notice>
      )}

      <SourceOfferCards
        offers={offers}
        kinds={kinds}
        credentials={store.loading && store.credentials.length === 0 ? null : store.credentials}
        busyKey={connecting}
        onConnect={setOfferForm}
        onDismiss={(offer) => setOffers((prev) => prev.filter((o) => offerKey(o) !== offerKey(offer)))}
      />

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
          signalKind={editor.signal.kind}
          sources={sources}
          kinds={kinds}
          recipes={catalog.recipes}
          prefill={editor.prefill}
          previewWindow={range}
          onSaved={saved}
        />
      )}

      {offerForm && (
        <DataSourceForm
          open
          onOpenChange={(open) => !open && setOfferForm(null)}
          kinds={kinds}
          initial={offerInitial(offerForm, offerKind)}
          storeCredentials={store.loading && store.credentials.length === 0 ? null : store.credentials}
          onSaved={(source) => void bindOffer(offerForm, source)}
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
