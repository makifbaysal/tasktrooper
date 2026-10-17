import {
  ApiError,
  type ComponentSignal,
  type ComponentSignals,
  type DataSource,
  type DataSourceCapability,
  type SignalKind,
  type ObservabilityRecipe,
  type SaveSignalBindingRequest,
  type SignalBinding,
  type SignalPreviewRequest,
} from "@/api";

export type BindingMode = "recipe" | "custom";

export interface BindingDraft {
  sourceId: string;
  mode: BindingMode;
  recipeId: string;
  params: Record<string, string>;
  query: string;
}

export type BindingPrefill = Partial<Pick<SignalBinding, "data_source_id" | "recipe_id" | "params" | "query">>;

export type BindingErrors = Partial<Record<"source" | "recipe" | "query", string>> & { params?: Record<string, string> };

export function metricsSources(sources: DataSource[]): DataSource[] {
  return sources.filter((s) => (s.capabilities ?? []).includes("metrics"));
}

// The capability a source must have to feed a signal of each kind: a log
// signal is read from a logs store (Loki, Elasticsearch), a trace signal from
// a traces store (Tempo), an error signal from an issue tracker (Sentry). A
// kind declares only what it implements, so the capability is the whole test.
const KIND_CAPABILITY: Record<SignalKind, DataSourceCapability> = {
  metric: "metrics",
  log: "logs",
  trace: "traces",
  error: "errors",
};

/** The sources that can feed a signal of this kind, by the capabilities their kind declares. */
export function bindableSources(sources: DataSource[], kind: SignalKind = "metric"): DataSource[] {
  const want = KIND_CAPABILITY[kind] ?? "metrics";
  return sources.filter((s) => (s.capabilities ?? []).includes(want));
}

/** Every source that can feed some signal: the Signals page has something to bind. */
export function anyBindableSource(sources: DataSource[]): boolean {
  return sources.some((s) => (s.capabilities ?? []).some((c) => (Object.values(KIND_CAPABILITY) as string[]).includes(c)));
}

// The catalog signals that are no metric, for a page that has not read the
// catalog yet (catalog/observability/signals.yaml).
const NON_METRIC: Record<string, SignalKind> = { "logs.lines": "log", "traces.spans": "trace", "errors.issues": "error" };

/** A signal's kind: the catalog's answer, else what the console knows of it. */
export function signalKindOf(signal: string, catalogKind?: SignalKind): SignalKind {
  return catalogKind ?? NON_METRIC[signal] ?? "metric";
}

/** Only a metric signal is read as a series: the server refuses to preview a
 * log, trace or error signal (422), which the Logs explorer, Traces and the
 * Errors inbox read instead. */
export function canPreview(kind: SignalKind | undefined): boolean {
  return (kind ?? "metric") === "metric";
}

/** The recipes written for this source's kind that provide this signal. */
export function compatibleRecipes(recipes: ObservabilityRecipe[], sourceKind: string | undefined, signal: string): ObservabilityRecipe[] {
  if (!sourceKind) return [];
  return recipes
    .filter((r) => r.source_kind === sourceKind && Object.prototype.hasOwnProperty.call(r.signals ?? {}, signal))
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
}

export function recipeParams(recipe: ObservabilityRecipe | undefined): string[] {
  return recipe?.params ?? [];
}

/** Where the editor starts: what it was opened with (a binding, a
 * suggestion), else the only metrics source and its only fitting recipe. A
 * source no recipe fits starts on a query of its own, and so does a prefill
 * with a query, recipe or not: the server reads the query when both are set. */
export function initialBindingDraft(
  signal: string,
  sources: DataSource[],
  recipes: ObservabilityRecipe[],
  prefill?: BindingPrefill | null,
  signalKind: SignalKind = "metric",
): BindingDraft {
  const metrics = bindableSources(sources, signalKind);
  const sourceId = prefill?.data_source_id || (metrics.length === 1 ? metrics[0].id : "");
  const kind = metrics.find((s) => s.id === sourceId)?.kind ?? sources.find((s) => s.id === sourceId)?.kind;
  const fitting = compatibleRecipes(recipes, kind, signal);
  const custom = prefill ? Boolean(prefill.query?.trim()) : sourceId !== "" && fitting.length === 0;
  const recipeId = prefill?.recipe_id || (fitting.length === 1 ? fitting[0].id : "");
  return {
    sourceId,
    mode: custom ? "custom" : "recipe",
    recipeId: custom ? "" : recipeId,
    params: { ...(prefill?.params ?? {}) },
    query: prefill?.query ?? "",
  };
}

/** A source switch keeps a recipe only when it still fits, and falls back to
 * a custom query when nothing does. */
export function withSource(draft: BindingDraft, sourceId: string, sources: DataSource[], recipes: ObservabilityRecipe[], signal: string): BindingDraft {
  const kind = sources.find((s) => s.id === sourceId)?.kind;
  const fitting = compatibleRecipes(recipes, kind, signal);
  if (fitting.length === 0) return { ...draft, sourceId, mode: "custom", recipeId: "" };
  const recipeId = fitting.some((r) => r.id === draft.recipeId) ? draft.recipeId : fitting.length === 1 ? fitting[0].id : "";
  return { ...draft, sourceId, recipeId };
}

/** A recipe param's value as sent: what was typed, else the recipe's default
 * — sent as is, since a save names every param the recipe declares. */
export function paramValue(draft: BindingDraft, recipe: ObservabilityRecipe | undefined, param: string): string {
  return (draft.params[param] ?? "").trim() || (recipe?.defaults?.[param] ?? "").trim();
}

export function validateBindingDraft(draft: BindingDraft, recipe: ObservabilityRecipe | undefined): BindingErrors {
  const errors: BindingErrors = {};
  if (!draft.sourceId) errors.source = "required";
  if (draft.mode === "custom") {
    if (draft.query.trim() === "") errors.query = "required";
    return errors;
  }
  if (!recipe) {
    errors.recipe = "required";
    return errors;
  }
  const missing = recipeParams(recipe).filter((p) => paramValue(draft, recipe, p) === "");
  if (missing.length > 0) errors.params = Object.fromEntries(missing.map((p) => [p, "required"]));
  return errors;
}

export function bindingErrorCount(errors: BindingErrors): number {
  return (errors.source ? 1 : 0) + (errors.recipe ? 1 : 0) + (errors.query ? 1 : 0) + Object.keys(errors.params ?? {}).length;
}

/** The params a custom query keeps: the ones it was opened with (a query may
 * reference `$name`), trimmed, empty ones dropped. */
export function customQueryParams(draft: BindingDraft): Record<string, string> {
  return Object.fromEntries(
    Object.entries(draft.params)
      .map(([k, v]) => [k.trim(), (v ?? "").trim()] as const)
      .filter(([k, v]) => k !== "" && v !== ""),
  );
}

/** What is sent: a recipe with exactly its declared params, or a query with
 * the params it carries. */
export function bindingRequest(draft: BindingDraft, recipe: ObservabilityRecipe | undefined): SaveSignalBindingRequest {
  if (draft.mode === "custom" || !recipe) {
    const params = customQueryParams(draft);
    return { data_source_id: draft.sourceId, query: draft.query.trim(), ...(Object.keys(params).length > 0 ? { params } : {}) };
  }
  const params = Object.fromEntries(recipeParams(recipe).map((p) => [p, paramValue(draft, recipe, p)]));
  return { data_source_id: draft.sourceId, recipe_id: recipe.id, params };
}

export function previewRequest(
  signal: string,
  draft: BindingDraft,
  recipe: ObservabilityRecipe | undefined,
  window: { from: number; to: number },
  stepSeconds?: number,
): SignalPreviewRequest {
  return {
    signal,
    ...bindingRequest(draft, recipe),
    from: new Date(window.from).toISOString(),
    to: new Date(window.to).toISOString(),
    ...(stepSeconds ? { step_seconds: stepSeconds } : {}),
  };
}

/** A suggestion that names every param it needs can be accepted as it is;
 * one that does not opens the editor with what it has. */
export function suggestionNeedsEditor(suggestion: SignalBinding): boolean {
  return (suggestion.needs_params ?? []).length > 0;
}

function replaceSignal(data: ComponentSignals, signal: string, next: (s: ComponentSignal) => ComponentSignal): ComponentSignals {
  return { ...data, signals: data.signals.map((s) => (s.signal === signal ? next(s) : s)) };
}

/** The list after a binding was saved or a suggestion confirmed: either
 * drops the slot's pending suggestions on the server too. */
export function applyBinding(data: ComponentSignals, binding: SignalBinding): ComponentSignals {
  return replaceSignal(data, binding.signal, (s) => ({ ...s, status: "bound", binding, suggestions: [] }));
}

export function applyDismiss(data: ComponentSignals, signal: string, suggestionId: string): ComponentSignals {
  return replaceSignal(data, signal, (s) => {
    const suggestions = (s.suggestions ?? []).filter((x) => x.id !== suggestionId);
    return { ...s, suggestions, status: s.binding ? "bound" : suggestions.length > 0 ? "suggested" : "unbound" };
  });
}

export function applyRemove(data: ComponentSignals, signal: string): ComponentSignals {
  return replaceSignal(data, signal, (s) => ({
    ...s,
    binding: null,
    status: (s.suggestions ?? []).length > 0 ? "suggested" : "unbound",
  }));
}

const WRITE_ERRORS = "operations.signalsPage.writeErrors";

// Codes the data source routes share (a suggest reads the sources).
const SOURCE_ERRORS: Record<string, string> = {
  upstream: "operations.chart.errors.upstream",
  bad_query: "operations.chart.errors.badQuery",
  unsupported: "operations.chart.errors.unsupported",
};

/** The message key for a refused suggest, accept, dismiss or remove, or null
 * when only the server's own sentence fits. A write the list may no longer
 * reflect (`confirmed`, `conflict`, a 404) also asks for a fresh read. */
export function signalWriteError(err: unknown): { key: string; reload: boolean } | null {
  if (!(err instanceof ApiError)) return null;
  if (err.status === 409 && err.type === "confirmed") return { key: `${WRITE_ERRORS}.confirmed`, reload: true };
  if (err.status === 409) return { key: `${WRITE_ERRORS}.conflict`, reload: true };
  if (err.status === 404) return { key: `${WRITE_ERRORS}.notFound`, reload: true };
  if (err.status === 408 || err.type === "timeout") return { key: `${WRITE_ERRORS}.timeout`, reload: false };
  if (SOURCE_ERRORS[err.type]) return { key: SOURCE_ERRORS[err.type], reload: false };
  return null;
}

export type BindingField = keyof Omit<BindingErrors, "params">;

const SERVER_FIELD: Record<string, BindingField> = {
  data_source_id: "source",
  recipe_id: "recipe",
  query: "query",
};

/** A refused save or preview's `fields`, split into what is shown on the
 * form's own inputs and the rest (signal, from, to, step_seconds, a param the
 * recipe does not declare), as [field, code] pairs. */
export function placeFieldErrors(fields: Record<string, string>, params: string[]): { inline: BindingErrors; rest: Array<[string, string]> } {
  const inline: BindingErrors = {};
  const rest: Array<[string, string]> = [];
  for (const [key, code] of Object.entries(fields)) {
    const at = SERVER_FIELD[key];
    if (at) inline[at] = code;
    else if (key.startsWith("params.") && params.includes(key.slice(7))) inline.params = { ...inline.params, [key.slice(7)]: code };
    else rest.push([key, code]);
  }
  return { inline, rest };
}
