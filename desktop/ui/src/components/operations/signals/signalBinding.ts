import type {
  ComponentSignal,
  ComponentSignals,
  DataSource,
  ObservabilityRecipe,
  SaveSignalBindingRequest,
  SignalBinding,
  SignalPreviewRequest,
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
 * source no recipe fits starts on a query of its own. */
export function initialBindingDraft(
  signal: string,
  sources: DataSource[],
  recipes: ObservabilityRecipe[],
  prefill?: BindingPrefill | null,
): BindingDraft {
  const metrics = metricsSources(sources);
  const sourceId = prefill?.data_source_id || (metrics.length === 1 ? metrics[0].id : "");
  const kind = metrics.find((s) => s.id === sourceId)?.kind;
  const fitting = compatibleRecipes(recipes, kind, signal);
  const custom = prefill ? !prefill.recipe_id && Boolean(prefill.query) : sourceId !== "" && fitting.length === 0;
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
  const missing = recipeParams(recipe).filter((p) => (draft.params[p] ?? "").trim() === "");
  if (missing.length > 0) errors.params = Object.fromEntries(missing.map((p) => [p, "required"]));
  return errors;
}

export function bindingErrorCount(errors: BindingErrors): number {
  return (errors.source ? 1 : 0) + (errors.recipe ? 1 : 0) + (errors.query ? 1 : 0) + Object.keys(errors.params ?? {}).length;
}

/** What is sent: a recipe with exactly its declared params, or a query. */
export function bindingRequest(draft: BindingDraft, recipe: ObservabilityRecipe | undefined): SaveSignalBindingRequest {
  if (draft.mode === "custom" || !recipe) return { data_source_id: draft.sourceId, query: draft.query.trim() };
  const params = Object.fromEntries(recipeParams(recipe).map((p) => [p, (draft.params[p] ?? "").trim()]));
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
