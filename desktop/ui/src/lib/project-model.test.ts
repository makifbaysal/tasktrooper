import { describe, expect, it } from "vitest";
import type {
  Component,
  ComponentCheck,
  ComponentLink,
  ComponentRole,
  Fact,
  LinkedComponent,
  RepositoryModel,
  ReviewItem,
  SystemResource,
} from "@/api";
import {
  componentLabel,
  effectiveRole,
  factValue,
  groupLinks,
  isOverridden,
  isScanFinished,
  linkTargetLabel,
  localCommandDisplay,
  localCommandsDisplay,
  projectTypeOf,
  requiredChecks,
  reviewCount,
  scanStagesInOrder,
  stackSummary,
} from "@/lib/project-model";

function fact<T>(detected?: T, override?: T): Fact<T> {
  return { detected, override };
}

function makeComponent(overrides: Partial<Component> = {}): Component {
  return {
    id: "c1",
    repository_id: "r1",
    path: ".",
    name: fact<string>(),
    role: fact<ComponentRole>("backend"),
    stack: fact(),
    commands: [],
    docs: {},
    gates: {},
    status: "active",
    manually_added: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeModel(overrides: Partial<RepositoryModel> = {}): RepositoryModel {
  return {
    repository: {
      id: "r1",
      name: "acme-platform",
      description: "",
      root_path: "/repos/acme-platform",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    shape: "monorepo",
    components: [],
    checks: [],
    links: [],
    incoming_links: [],
    resources: [],
    linked_components: [],
    environments: [],
    notes: [],
    review: [],
    ...overrides,
  };
}

function makeLink(overrides: Partial<ComponentLink> = {}): ComponentLink {
  return {
    id: "l1",
    repository_id: "r1",
    from_component_id: "c1",
    protocol: "http",
    confidence: "high",
    status: "confirmed",
    source: "scan",
    auto_confirmed: false,
    missing: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeCheck(overrides: Partial<ComponentCheck> = {}): ComponentCheck {
  return {
    id: "chk1",
    repository_id: "r1",
    component_id: "c1",
    source: "ci",
    workflow: "ci.yml",
    job_key: "test",
    purpose: fact("test"),
    local_commands: fact([]),
    gate: fact("required"),
    dispatchable: true,
    status: "active",
    missing: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("factValue", () => {
  it("prefers the override over the detected value", () => {
    expect(factValue(fact("go", "rust"))).toBe("rust");
  });
  it("falls back to detected when there is no override", () => {
    expect(factValue(fact("go"))).toBe("go");
  });
  it("is undefined when neither is set", () => {
    expect(factValue(fact<string>())).toBeUndefined();
    expect(factValue(undefined)).toBeUndefined();
  });
});

describe("isOverridden", () => {
  it("is true only when override is present", () => {
    expect(isOverridden(fact("go", "rust"))).toBe(true);
    expect(isOverridden(fact("go"))).toBe(false);
    expect(isOverridden(undefined)).toBe(false);
  });
});

describe("effectiveRole", () => {
  it("reads the component's effective role fact", () => {
    const component = makeComponent({ role: fact("backend", "frontend") });
    expect(effectiveRole(component)).toBe("frontend");
  });
});

describe("componentLabel", () => {
  it("uses the repository name for the root component", () => {
    const component = makeComponent({ path: ".", name: fact<string>() });
    expect(componentLabel(component, "acme-platform")).toBe("acme-platform");
  });

  it("prefers the effective name over the path", () => {
    const component = makeComponent({ path: "services/api", name: fact("Billing API") });
    expect(componentLabel(component, "acme-platform")).toBe("Billing API");
  });

  it("falls back to the last path segment when there is no name", () => {
    const component = makeComponent({ path: "services/api", name: fact<string>() });
    expect(componentLabel(component, "acme-platform")).toBe("api");
  });
});

describe("stackSummary", () => {
  it("orders frameworks, then languages, then libraries, each with its version", () => {
    const summary = stackSummary({
      languages: [{ name: "Go", version: "1.23" }],
      frameworks: [{ name: "chi" }],
      libraries: [{ name: "pgx", version: "5" }],
    });
    expect(summary).toBe("chi · Go 1.23 · pgx 5");
  });

  it("truncates to the limit", () => {
    const summary = stackSummary(
      { frameworks: [{ name: "chi" }], languages: [{ name: "Go" }], libraries: [{ name: "pgx" }] },
      2,
    );
    expect(summary).toBe("chi · Go");
  });

  it("is empty for an unset stack", () => {
    expect(stackSummary(undefined)).toBe("");
  });
});

describe("requiredChecks", () => {
  it("counts only active, non-missing, gate-required checks", () => {
    const checks = [
      makeCheck({ id: "a", gate: fact("required") }),
      makeCheck({ id: "b", gate: fact("info") }),
      makeCheck({ id: "c", gate: fact("required"), missing: true }),
      makeCheck({ id: "d", gate: fact("required"), status: "dismissed" }),
    ];
    expect(requiredChecks(checks)).toBe(1);
  });

  it("narrows to one component when given an id", () => {
    const checks = [
      makeCheck({ id: "a", component_id: "c1", gate: fact("required") }),
      makeCheck({ id: "b", component_id: "c2", gate: fact("required") }),
    ];
    expect(requiredChecks(checks, "c2")).toBe(1);
  });
});

describe("linkTargetLabel", () => {
  it("resolves a component in the same repository to repo/path", () => {
    const model = makeModel({
      components: [makeComponent({ id: "api", path: "services/api" })],
    });
    const link = makeLink({ to_component_id: "api" });
    expect(linkTargetLabel(link, model)).toBe("acme-platform/services/api");
  });

  it("resolves the root component to just the repository name", () => {
    const model = makeModel({ components: [makeComponent({ id: "root", path: "." })] });
    const link = makeLink({ to_component_id: "root" });
    expect(linkTargetLabel(link, model)).toBe("acme-platform");
  });

  it("resolves a linked component from another repository", () => {
    const linked: LinkedComponent = {
      id: "billing",
      repository_id: "r2",
      repository_name: "billing-svc",
      path: ".",
      name: "billing-svc",
      role: "backend",
    };
    const model = makeModel({ linked_components: [linked] });
    const link = makeLink({ to_component_id: "billing" });
    expect(linkTargetLabel(link, model)).toBe("billing-svc");
  });

  it("resolves a resource target to its name", () => {
    const resource: SystemResource = {
      id: "res1",
      kind: "database",
      vendor: "",
      name: "Postgres",
      identity_key: "db:postgres",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const model = makeModel({ resources: [resource] });
    const link = makeLink({ to_resource_id: "res1" });
    expect(linkTargetLabel(link, model)).toBe("Postgres");
  });

  it("falls back to the hint when unresolved", () => {
    const model = makeModel();
    const link = makeLink({ hint: "BILLING_SVC_URL" });
    expect(linkTargetLabel(link, model)).toBe("BILLING_SVC_URL");
  });
});

describe("groupLinks", () => {
  it("splits outgoing and incoming links by component id", () => {
    const model = {
      links: [makeLink({ id: "out1", from_component_id: "c1" }), makeLink({ id: "out2", from_component_id: "c2" })],
      incoming_links: [makeLink({ id: "in1", to_component_id: "c1" }), makeLink({ id: "in2", to_component_id: "c2" })],
    };
    const { outgoing, incoming } = groupLinks(model, "c1");
    expect(outgoing.map((l) => l.id)).toEqual(["out1"]);
    expect(incoming.map((l) => l.id)).toEqual(["in1"]);
  });
});

describe("projectTypeOf", () => {
  it("is empty with no repositories", () => {
    expect(projectTypeOf([])).toBe("empty");
  });
  it("is single_repo for one non-monorepo shape", () => {
    expect(projectTypeOf(["single"])).toBe("single_repo");
  });
  it("is monorepo for one monorepo shape", () => {
    expect(projectTypeOf(["monorepo"])).toBe("monorepo");
  });
  it("is multi_repo for more than one repository", () => {
    expect(projectTypeOf(["single", "single"])).toBe("multi_repo");
  });
});

describe("reviewCount", () => {
  it("counts review items, treating absence as zero", () => {
    const items: ReviewItem[] = [
      { kind: "role", entity_id: "c1", repository_id: "r1", component_id: "c1", confidence: "medium" },
    ];
    expect(reviewCount(items)).toBe(1);
    expect(reviewCount(undefined)).toBe(0);
    expect(reviewCount(null)).toBe(0);
  });
});

describe("scanStagesInOrder", () => {
  it("starts with clone and ends with notes, in the server's fixed pipeline order", () => {
    const stages = scanStagesInOrder();
    expect(stages[0]).toBe("clone");
    expect(stages[stages.length - 1]).toBe("notes");
    expect(stages).toEqual([
      "clone", "inventory", "shape", "components", "stack", "checks", "links", "deploy", "match", "notes",
    ]);
  });
});

describe("isScanFinished", () => {
  it("is finished only on succeeded or failed", () => {
    expect(isScanFinished({ status: "succeeded" })).toBe(true);
    expect(isScanFinished({ status: "failed" })).toBe(true);
    expect(isScanFinished({ status: "running" })).toBe(false);
    expect(isScanFinished({ status: "queued" })).toBe(false);
    expect(isScanFinished(null)).toBe(false);
    expect(isScanFinished(undefined)).toBe(false);
  });
});

describe("localCommandDisplay", () => {
  it("is just the argv when dir is the component root", () => {
    expect(localCommandDisplay({ dir: ".", argv: ["go", "test", "./..."] })).toBe("go test ./...");
    expect(localCommandDisplay({ dir: "", argv: ["npm", "test"] })).toBe("npm test");
  });

  it("cds into the directory first when it is not the root", () => {
    expect(localCommandDisplay({ dir: "services/api", argv: ["golangci-lint", "run"] })).toBe(
      "cd services/api && golangci-lint run",
    );
  });
});

describe("localCommandsDisplay", () => {
  it("runs commands that share a directory under one cd", () => {
    expect(
      localCommandsDisplay([
        { dir: "desktop", argv: ["npm", "run", "lint"] },
        { dir: "desktop", argv: ["npm", "test"] },
        { dir: ".", argv: ["make", "check"] },
      ]),
    ).toBe("cd desktop && npm run lint && npm test; make check");
  });
});
