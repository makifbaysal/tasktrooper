import { describe, expect, it } from "vitest";
import { COMPONENT_ROLES } from "@/api";
import { BUILTIN_TILES, ROLE_VIEWS, entitySectionPath, hasTab, isBuiltinSignal, tabOrDefault, viewFor } from "@/components/operations/console/roleViews";
import { en } from "@/locales/en";

describe("ROLE_VIEWS", () => {
  it("has a view for every component role, Summary first", () => {
    for (const role of COMPONENT_ROLES) {
      expect(ROLE_VIEWS[role], role).toBeDefined();
      expect(ROLE_VIEWS[role].tabs[0], role).toBe("summary");
    }
    expect(Object.keys(ROLE_VIEWS).sort()).toEqual([...COMPONENT_ROLES].sort());
  });

  it("gives every running role the base sections and its own extra", () => {
    expect(ROLE_VIEWS.backend.tabs).toEqual(["summary", "errors", "logs", "changes", "issues"]);
    expect(ROLE_VIEWS.frontend.tabs).toEqual(["summary", "errors", "logs", "changes", "issues"]);
    expect(ROLE_VIEWS.worker.tabs).toEqual(["summary", "queues", "errors", "logs", "changes", "issues"]);
    expect(ROLE_VIEWS.mobile.tabs).toEqual(["summary", "releases", "errors", "logs", "changes", "issues"]);
    expect(hasTab("backend", "queues")).toBe(false);
    expect(hasTab("worker", "queues")).toBe(true);
    expect(hasTab("mobile", "releases")).toBe(true);
  });

  it("keeps only changes and issues for roles with no runtime to watch", () => {
    for (const role of ["library", "cli", "infra", "data", "game", "other"] as const) {
      expect(ROLE_VIEWS[role].tabs, role).toEqual(["summary", "changes", "issues"]);
      expect(ROLE_VIEWS[role].charts, role).toEqual([]);
    }
  });

  it("draws the 2×2 golden-signal grid from the signal catalog's ids", () => {
    expect(ROLE_VIEWS.backend.charts.map((c) => c.id)).toEqual(["http.latency", "http.requests", "http.error_ratio", "infra.cpu"]);
    expect(ROLE_VIEWS.worker.charts.map((c) => c.id)).toEqual(["queue.backlog", "queue.consume_rate", "queue.backlog_age", "process.errors"]);
    expect(ROLE_VIEWS.frontend.charts.map((c) => c.id)).toEqual(["web.lcp", "web.page_views", "web.inp", "web.js_errors"]);
    expect(ROLE_VIEWS.mobile.charts.map((c) => c.id)).toEqual([
      "mobile.crash_free_users",
      "mobile.cold_start",
      "mobile.anr_rate",
      "mobile.rollout_fraction",
    ]);
    for (const role of COMPONENT_ROLES) {
      const view = ROLE_VIEWS[role];
      for (const g of view.golden) expect(view.charts, `${role} ${g.id}`).toContainEqual(g);
    }
  });

  it("labels every signal and unit from the dictionary", () => {
    for (const role of COMPONENT_ROLES) {
      const view = ROLE_VIEWS[role];
      for (const spec of [...view.golden, ...view.charts, ...BUILTIN_TILES]) {
        expect(en.operations.signals, spec.id).toHaveProperty(spec.label);
        if (spec.short) expect(en.operations.signals, spec.id).toHaveProperty(spec.short);
        if (spec.unit) expect(en.operations.units, spec.id).toHaveProperty(spec.unit);
      }
      for (const tab of view.tabs) expect(en.operations.entity.tabs, tab).toHaveProperty(tab);
      expect(new Set(view.charts.map((c) => c.id)).size).toBe(view.charts.length);
    }
  });

  it("marks only the stored-data tiles as built in", () => {
    expect(BUILTIN_TILES.every((t) => isBuiltinSignal(t.id))).toBe(true);
    expect(ROLE_VIEWS.backend.charts.some((c) => isBuiltinSignal(c.id))).toBe(false);
  });
});

describe("viewFor / tabOrDefault", () => {
  it("falls back to the no-runtime view and the view's first section", () => {
    expect(viewFor(undefined).tabs).toEqual(["summary", "changes", "issues"]);
    expect(tabOrDefault(ROLE_VIEWS.backend, "logs")).toBe("logs");
    expect(tabOrDefault(ROLE_VIEWS.backend, "queues")).toBe("summary");
    expect(tabOrDefault(ROLE_VIEWS.library, "errors")).toBe("summary");
  });
});

describe("entitySectionPath", () => {
  it("links a role's own section, else the console page filtered to the entity", () => {
    expect(entitySectionPath({ id: "c-api", role: "backend" }, "errors")).toBe("/operations/entity/c-api/errors");
    expect(entitySectionPath({ id: "c-etl", role: "data" }, "errors")).toBe("/operations/errors?entity=c-etl");
    expect(entitySectionPath({ id: "c-cli", role: "cli" }, "logs")).toBe("/operations/logs?entity=c-cli");
    expect(entitySectionPath({ id: "c-cli", role: "cli" }, "queues")).toBe("/operations/entity/c-cli/summary");
  });
});
