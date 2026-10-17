import { Database, Play } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import {
  api,
  ApiError,
  dataSourceFieldErrors,
  type DataSource,
  type DataSourceKindInfo,
  type DeployEnvironment,
  type ObservabilityRecipe,
  type SignalBinding,
  type SignalKind,
  type SignalPreview,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { isInstantResult } from "@/components/operations/charts/chartData";
import { LiveValueList, liveValueItems } from "@/components/operations/charts/LiveValueList";
import { autoStepSeconds } from "@/components/operations/charts/signalChart";
import { TimeSeriesChart } from "@/components/operations/charts/TimeSeriesChart";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { signalFormatter, signalLabel } from "@/components/operations/console/signalFormat";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import {
  bindableSources,
  bindingErrorCount,
  bindingRequest,
  canPreview,
  compatibleRecipes,
  initialBindingDraft,
  placeFieldErrors,
  previewRequest,
  recipeParams,
  validateBindingDraft,
  withSource,
  type BindingDraft,
  type BindingErrors,
  type BindingMode,
  type BindingPrefill,
} from "@/components/operations/signals/signalBinding";
import { kindLabel, queryHint, queryPlaceholder, readsLiveOnly, toChartSeries } from "@/components/operations/sources/dataSourceSchema";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";

export interface SignalBindingDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  componentId: string;
  env: DeployEnvironment;
  signal: string;
  /** The signal's catalog unit, for the preview's values. */
  unit?: string;
  /** The signal's catalog kind: which sources can feed it, and whether it previews as a series. */
  signalKind?: SignalKind;
  sources: DataSource[];
  kinds: DataSourceKindInfo[];
  recipes: ObservabilityRecipe[];
  /** A binding or suggestion to start from. */
  prefill?: BindingPrefill | null;
  /** The window the preview reads: the console's range. */
  previewWindow: { from: number; to: number };
  onSaved: (binding: SignalBinding) => void;
}

interface Failure {
  title: string;
  message: string;
}

const QUERY_LANGUAGE: Record<string, "promql" | "cloudwatch"> = {
  prometheus: "promql",
  gcp_monitoring: "promql",
  aws_cloudwatch: "cloudwatch",
};

// Request keys a refusal can name that have no input of their own here.
const FIELD_NAMES: Record<string, string> = {
  signal: "signal",
  from: "from",
  to: "to",
  step_seconds: "step",
};

/** Binds one signal: a metrics source, then a recipe that fits it (with its
 * params) or a query of the source's own, a preview over the console's
 * range, and Save. Mount it only while open — its state starts from the props. */
export function SignalBindingDialog({
  open,
  onOpenChange,
  componentId,
  env,
  signal,
  unit,
  signalKind = "metric",
  sources,
  kinds,
  recipes,
  prefill,
  previewWindow,
  onSaved,
}: SignalBindingDialogProps) {
  const { t, lang } = useI18n();
  const b = (key: string, params?: Record<string, string | number>) => t(`operations.signalBinding.${key}`, params);
  const href = useOpsHref();
  const metrics = useMemo(() => bindableSources(sources, signalKind), [sources, signalKind]);
  const previewable = canPreview(signalKind);
  const [draft, setDraft] = useState<BindingDraft>(() => initialBindingDraft(signal, sources, recipes, prefill, signalKind));
  const [errors, setErrors] = useState<BindingErrors>({});
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [previewing, setPreviewing] = useState(false);
  const [preview, setPreview] = useState<SignalPreview | null>(null);
  const [failure, setFailure] = useState<Failure | null>(null);
  const seq = useRef(0);

  const source = metrics.find((s) => s.id === draft.sourceId);
  const kind = kinds.find((k) => k.kind === source?.kind);
  const fitting = compatibleRecipes(recipes, source?.kind, signal);
  const recipe = fitting.find((r) => r.id === draft.recipeId);
  const params = recipeParams(recipe);
  const customParams = draft.mode === "custom" ? Object.keys(draft.params).sort() : [];
  const name = signalLabel(t, signal);
  const envLabel = t(`cloud.environments.${env}`);
  const format = useMemo(() => signalFormatter(unit, { locale: intlLocale(lang), t }), [unit, lang, t]);
  const series = useMemo(() => toChartSeries(preview?.series ?? []), [preview]);
  const valid = bindingErrorCount(validateBindingDraft(draft, recipe)) === 0;
  const liveSource = readsLiveOnly(source);

  const change = (next: BindingDraft) => {
    setDraft(next);
    setErrors({});
    setSaveError("");
  };

  const errorText = (code: string) => {
    const key = `operations.signalBinding.errors.${code}`;
    const text = t(key);
    return text === key ? code : text;
  };

  const fieldName = (key: string) => {
    if (key.startsWith("params.")) return key.slice(7);
    return FIELD_NAMES[key] ? b(`fieldNames.${FIELD_NAMES[key]}`) : key;
  };

  // Field errors on their inputs; the ones with no input here, worded, for a notice.
  const showFieldErrors = (fields: Record<string, string>): string => {
    const { inline, rest } = placeFieldErrors(fields, draft.mode === "custom" ? customParams : params);
    setErrors(inline);
    return rest.map(([key, code]) => `${fieldName(key)}: ${errorText(code)}`).join(" ");
  };

  const describe = (err: unknown): Failure => {
    const message = err instanceof Error ? err.message : t("common.actionFailed");
    if (err instanceof ApiError) {
      if (err.type === "bad_query") return { title: b("preview.badQuery"), message };
      if (err.type === "upstream") return { title: b("preview.upstream"), message };
      if (err.type === "unsupported") return { title: b("preview.unsupported"), message: "" };
      if (err.status === 408 || err.type === "timeout") return { title: b("preview.timeout"), message: "" };
    }
    const fields = dataSourceFieldErrors(err);
    if (fields) return { title: b("preview.invalid"), message: showFieldErrors(fields) };
    return { title: b("preview.failed"), message };
  };

  const saveFailure = (err: unknown): string => {
    const fields = dataSourceFieldErrors(err);
    if (fields) return showFieldErrors(fields);
    if (err instanceof ApiError) {
      if (err.status === 404) return b("saveErrors.notFound");
      if (err.status === 408 || err.type === "timeout") return b("saveErrors.timeout");
    }
    return err instanceof Error ? err.message : t("common.actionFailed");
  };

  const runPreview = async () => {
    const found = validateBindingDraft(draft, recipe);
    setErrors(found);
    if (bindingErrorCount(found) > 0) return;
    const mine = ++seq.current;
    setPreviewing(true);
    setFailure(null);
    try {
      const res = await api.previewComponentSignal(
        componentId,
        previewRequest(signal, draft, recipe, previewWindow, autoStepSeconds(previewWindow.to - previewWindow.from)),
      );
      if (mine !== seq.current) return;
      setPreview(res);
    } catch (err) {
      if (mine !== seq.current) return;
      setPreview(null);
      setFailure(describe(err));
    } finally {
      if (mine === seq.current) setPreviewing(false);
    }
  };

  const save = async () => {
    const found = validateBindingDraft(draft, recipe);
    setErrors(found);
    setSaveError("");
    if (bindingErrorCount(found) > 0) return;
    setSaving(true);
    try {
      const saved = await api.bindComponentSignal(componentId, signal, env, bindingRequest(draft, recipe));
      onSaved(saved);
      onOpenChange(false);
    } catch (err) {
      setSaveError(saveFailure(err));
    } finally {
      setSaving(false);
    }
  };

  const close = () => onOpenChange(false);
  const title = b("title", { signal: name });

  if (metrics.length === 0) {
    return (
      <FormDialog
        open={open}
        onOpenChange={onOpenChange}
        title={title}
        footer={
          <Button variant="outline" onClick={close}>
            {t("common.cancel")}
          </Button>
        }
      >
        <EmptyState
          icon={Database}
          title={b("noSources.title")}
          description={b("noSources.description")}
          action={
            <Button variant="outline" size="sm" asChild>
              <Link to={href(DATA_SOURCES_PATH)}>{b("noSources.action")}</Link>
            </Button>
          }
        />
      </FormDialog>
    );
  }

  return (
    <FormDialog
      open={open}
      onOpenChange={(next) => !saving && onOpenChange(next)}
      title={title}
      description={b("description", { signal: name, env: envLabel })}
      className="sm:max-w-2xl"
      footer={
        <>
          <Button variant="outline" onClick={close} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void save()} disabled={saving}>
            {saving ? b("saving") : b("save")}
          </Button>
        </>
      }
    >
      {saveError && (
        <Notice variant="error" title={b("saveFailed")}>
          <p className="break-words">{saveError}</p>
        </Notice>
      )}

      <div className="space-y-1.5">
        <Label htmlFor="sb-source">{b("source")}</Label>
        <Select value={draft.sourceId} onValueChange={(id) => change(withSource(draft, id, metrics, recipes, signal))} disabled={saving}>
          <SelectTrigger id="sb-source" aria-invalid={errors.source ? true : undefined}>
            <SelectValue placeholder={b("sourcePlaceholder")} />
          </SelectTrigger>
          <SelectContent>
            {metrics.map((s) => {
              const k = kinds.find((x) => x.kind === s.kind);
              return (
                <SelectItem key={s.id} value={s.id}>
                  {s.name}
                  <span className="ml-2 text-muted-foreground">{k ? kindLabel(t, k) : s.kind}</span>
                </SelectItem>
              );
            })}
          </SelectContent>
        </Select>
        {errors.source && <p className="text-caption text-destructive">{errorText(errors.source)}</p>}
      </div>

      {source && (
        <>
          <Tabs value={draft.mode} onValueChange={(mode) => change({ ...draft, mode: mode as BindingMode })} variant="pill" className="w-fit">
            <TabsList aria-label={b("mode")}>
              <TabsTrigger value="recipe">{b("modes.recipe")}</TabsTrigger>
              <TabsTrigger value="custom">{b("modes.custom")}</TabsTrigger>
            </TabsList>
          </Tabs>

          {draft.mode === "recipe" ? (
            fitting.length === 0 ? (
              <Notice variant="info" title={b("noRecipe.title")}>
                <span>{b("noRecipe.body", { kind: kind ? kindLabel(t, kind) : source.kind, signal: name })} </span>
                <Button variant="link" size="sm" className="h-auto p-0" onClick={() => change({ ...draft, mode: "custom" })}>
                  {b("noRecipe.action")}
                </Button>
              </Notice>
            ) : (
              <div className="space-y-4">
                <div className="space-y-1.5">
                  <Label htmlFor="sb-recipe">{b("recipe")}</Label>
                  <Select
                    value={draft.recipeId}
                    onValueChange={(id) => change({ ...draft, recipeId: id })}
                    disabled={saving}
                  >
                    <SelectTrigger id="sb-recipe" aria-invalid={errors.recipe ? true : undefined}>
                      <SelectValue placeholder={b("recipePlaceholder")} />
                    </SelectTrigger>
                    <SelectContent>
                      {fitting.map((r) => (
                        <SelectItem key={r.id} value={r.id}>
                          <span className="font-mono">{r.id}</span>
                          {r.applies_to?.technology && <span className="ml-2 text-muted-foreground">{r.applies_to.technology}</span>}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {errors.recipe && <p className="text-caption text-destructive">{errorText(errors.recipe)}</p>}
                </div>
                {recipe && (
                  <>
                    <div className="space-y-1">
                      <p className="text-caption font-medium text-muted-foreground">{b("template")}</p>
                      <pre className="max-h-28 overflow-auto rounded-md bg-muted/50 px-3 py-2 font-mono text-micro whitespace-pre-wrap break-all">
                        {recipe.signals[signal]}
                      </pre>
                    </div>
                    {params.length > 0 && (
                      <fieldset className="space-y-3">
                        <legend className="mb-1 text-body font-medium">{b("params")}</legend>
                        {params.map((p) => {
                          const error = errors.params?.[p];
                          return (
                            <div key={p} className="space-y-1.5">
                              <Label htmlFor={`sb-param-${p}`} className="font-mono">
                                {p}
                              </Label>
                              <Input
                                id={`sb-param-${p}`}
                                value={draft.params[p] ?? ""}
                                placeholder={recipe.defaults?.[p] || undefined}
                                onChange={(e) => change({ ...draft, params: { ...draft.params, [p]: e.target.value } })}
                                aria-invalid={error ? true : undefined}
                                autoComplete="off"
                                spellCheck={false}
                                className="font-mono text-caption"
                                disabled={saving}
                              />
                              {error && <p className="text-caption text-destructive">{errorText(error)}</p>}
                            </div>
                          );
                        })}
                      </fieldset>
                    )}
                  </>
                )}
              </div>
            )
          ) : (
            <div className="space-y-1.5">
              <Label htmlFor="sb-query">{b("query")}</Label>
              <Textarea
                id="sb-query"
                value={draft.query}
                onChange={(e) => change({ ...draft, query: e.target.value })}
                onKeyDown={(e) => {
                  if (previewable && e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                    e.preventDefault();
                    void runPreview();
                  }
                }}
                placeholder={queryPlaceholder(t, source.kind)}
                rows={4}
                spellCheck={false}
                className="font-mono text-caption"
                aria-invalid={errors.query ? true : undefined}
                aria-describedby="sb-query-hint"
                disabled={saving}
              />
              <p id="sb-query-hint" className="text-caption text-muted-foreground">
                {QUERY_LANGUAGE[source.kind]
                  ? b(`queryHints.${QUERY_LANGUAGE[source.kind]}`)
                  : queryHint(t, source.kind)
                    ? previewable
                      ? `${queryHint(t, source.kind)} ${b("previewShortcut")}`
                      : queryHint(t, source.kind)
                    : b("queryHint")}
              </p>
              {errors.query && <p className="text-caption text-destructive">{errorText(errors.query)}</p>}
              {customParams.length > 0 && (
                <fieldset className="space-y-3 pt-2">
                  <legend className="mb-1 text-body font-medium">{b("params")}</legend>
                  <p className="text-caption text-muted-foreground">{b("customParamsHint")}</p>
                  {customParams.map((p) => {
                    const error = errors.params?.[p];
                    return (
                      <div key={p} className="space-y-1.5">
                        <Label htmlFor={`sb-param-${p}`} className="font-mono">
                          {p}
                        </Label>
                        <Input
                          id={`sb-param-${p}`}
                          value={draft.params[p] ?? ""}
                          onChange={(e) => change({ ...draft, params: { ...draft.params, [p]: e.target.value } })}
                          aria-invalid={error ? true : undefined}
                          autoComplete="off"
                          spellCheck={false}
                          className="font-mono text-caption"
                          disabled={saving}
                        />
                        {error && <p className="text-caption text-destructive">{errorText(error)}</p>}
                      </div>
                    );
                  })}
                </fieldset>
              )}
            </div>
          )}

          {!previewable ? (
            <Notice variant="info" title={t("operations.telemetry.binding.noPreviewTitle")} role="status">
              {t("operations.telemetry.binding.noPreview", { signal: name })}
            </Notice>
          ) : (
            <section className="space-y-3" aria-labelledby="sb-preview-title">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h3 id="sb-preview-title" className="text-body font-semibold">
                    {b("preview.title")}
                  </h3>
                  <p className="text-caption text-muted-foreground">{b("preview.description")}</p>
                </div>
                <Button variant="outline" size="sm" onClick={() => void runPreview()} disabled={previewing || !valid}>
                  <Play aria-hidden />
                  {previewing ? b("preview.running") : b("preview.run")}
                </Button>
              </div>
              {failure && (
                <Notice variant="error" title={failure.title}>
                  {failure.message && <p className="break-words font-mono text-caption">{failure.message}</p>}
                </Notice>
              )}
              {preview && (preview.warnings ?? []).length > 0 && (
                <Notice variant="warning" title={b("preview.warnings")}>
                  <ul className="list-disc space-y-0.5 pl-4">
                    {preview.warnings.map((w) => (
                      <li key={w} className="break-words">
                        {w}
                      </li>
                    ))}
                  </ul>
                </Notice>
              )}
              {preview?.rendered_query && (
                <div className="space-y-1">
                  <p className="text-caption font-medium text-muted-foreground">{b("preview.rendered")}</p>
                  <pre className="max-h-28 overflow-auto rounded-md bg-muted/50 px-3 py-2 font-mono text-micro whitespace-pre-wrap break-all">
                    {preview.rendered_query}
                  </pre>
                </div>
              )}
              {preview && liveSource && isInstantResult(series) ? (
                <div className="space-y-1.5">
                  <LiveValueList items={liveValueItems(series)} formatValue={format} ariaLabel={b("preview.chartLabel", { signal: name })} height={180} />
                  <p className="text-caption text-muted-foreground">{b("preview.liveNote")}</p>
                </div>
              ) : (
                (preview || previewing) && (
                  <TimeSeriesChart
                    series={series}
                    ariaLabel={b("preview.chartLabel", { signal: name })}
                    formatValue={format}
                    height={180}
                    loading={previewing && !preview}
                    xRange={[previewWindow.from, previewWindow.to]}
                    emptyLabel={b("preview.noSeries")}
                    showTableToggle
                  />
                )
              )}
            </section>
          )}
        </>
      )}
    </FormDialog>
  );
}
