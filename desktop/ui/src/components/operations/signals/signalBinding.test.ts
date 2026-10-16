import { describe, expect, it } from "vitest";
import type { ComponentSignals, ObservabilityRecipe, SignalBinding } from "@/api";
import {
  applyBinding,
  applyDismiss,
  applyRemove,
  bindingRequest,
  compatibleRecipes,
  initialBindingDraft,
  validateBindingDraft,
  withSource,
} from "@/components/operations/signals/signalBinding";
import { dataSource } from "@/test/dataSourceFixtures";

function recipe(id: string, kind: string, signals: string[], params: string[] | null = null): ObservabilityRecipe {
  return {
    id,
    source_kind: kind,
    applies_to: { technology: "", messaging_system: "", resource_kind: "", resource_params: {} },
    detect: { metric: "" },
    params,
    signals: Object.fromEntries(signals.map((s) => [s, "q"])),
    origin: "catalog",
  };
}

const recipes = [
  recipe("otel-http", "prometheus", ["http.latency", "http.requests"], ["job"]),
  recipe("nginx", "prometheus", ["http.requests"]),
  recipe("cloud-run", "gcp_monitoring", ["http.requests"], ["service"]),
];
const prom = dataSource();
const gcp = dataSource({ id: "ds-gcp", kind: "gcp_monitoring" });

function suggestion(id: string, overrides: Partial<SignalBinding> = {}): SignalBinding {
  return {
    id,
    component_id: "c",
    environment: "production",
    signal: "http.requests",
    data_source_id: "ds-1",
    data_source_name: "Prom",
    data_source_kind: "prometheus",
    recipe_id: "nginx",
    params: {},
    query: "",
    status: "suggested",
    source: "detected",
    rendered_query: "",
    needs_params: [],
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

describe("recipes for a binding", () => {
  it("keeps the recipes of the source's kind that provide the signal, by id", () => {
    expect(compatibleRecipes(recipes, "prometheus", "http.requests").map((r) => r.id)).toEqual(["nginx", "otel-http"]);
    expect(compatibleRecipes(recipes, "prometheus", "http.latency").map((r) => r.id)).toEqual(["otel-http"]);
    expect(compatibleRecipes(recipes, "gcp_monitoring", "http.latency")).toEqual([]);
    expect(compatibleRecipes(recipes, undefined, "http.requests")).toEqual([]);
  });

  it("starts on the only source and its only recipe, and keeps a recipe across a source switch only while it fits", () => {
    const draft = initialBindingDraft("http.latency", [prom], recipes);
    expect(draft).toMatchObject({ sourceId: "ds-1", mode: "recipe", recipeId: "otel-http" });
    expect(withSource(draft, "ds-gcp", [prom, gcp], recipes, "http.latency")).toMatchObject({ mode: "custom", recipeId: "" });
    const requests = initialBindingDraft("http.requests", [prom, gcp], recipes);
    expect(requests).toMatchObject({ sourceId: "", recipeId: "" });
    expect(withSource(requests, "ds-gcp", [prom, gcp], recipes, "http.requests")).toMatchObject({ recipeId: "cloud-run", mode: "recipe" });
  });

  it("checks params before anything is sent, and sends only the recipe's own", () => {
    const otel = recipes[0];
    const draft = { sourceId: "ds-1", mode: "recipe" as const, recipeId: "otel-http", params: { job: " ", other: "x" }, query: "" };
    expect(validateBindingDraft(draft, otel)).toEqual({ params: { job: "required" } });
    expect(bindingRequest({ ...draft, params: { job: " api ", other: "x" } }, otel)).toEqual({
      data_source_id: "ds-1",
      recipe_id: "otel-http",
      params: { job: "api" },
    });
    expect(validateBindingDraft({ ...draft, mode: "custom" }, otel)).toEqual({ query: "required" });
    expect(validateBindingDraft({ ...draft, sourceId: "", recipeId: "" }, undefined)).toEqual({ source: "required", recipe: "required" });
  });
});

describe("the list after a write", () => {
  const list: ComponentSignals = {
    component_id: "c",
    role: "backend",
    environment: "production",
    signals: [
      { signal: "http.requests", kind: "metric", unit: "req/s", golden: true, status: "suggested", binding: null, suggestions: [suggestion("s-1"), suggestion("s-2")] },
    ],
  };

  it("binds a confirmed suggestion and drops the slot's other suggestions, as the server does", () => {
    const next = applyBinding(list, suggestion("s-1", { status: "confirmed" }));
    expect(next.signals[0]).toMatchObject({ status: "bound", binding: { id: "s-1" }, suggestions: [] });
  });

  it("goes back to unbound once the last suggestion is dismissed, and to suggested when a binding goes while one waits", () => {
    const once = applyDismiss(list, "http.requests", "s-1");
    expect(once.signals[0].status).toBe("suggested");
    expect(applyDismiss(once, "http.requests", "s-2").signals[0].status).toBe("unbound");
    const bound = { ...list, signals: [{ ...list.signals[0], status: "bound" as const, binding: suggestion("b-1", { status: "confirmed" }) }] };
    expect(applyRemove(bound, "http.requests").signals[0]).toMatchObject({ status: "suggested", binding: null });
    expect(applyRemove(applyBinding(list, suggestion("s-1")), "http.requests").signals[0].status).toBe("unbound");
  });
});
