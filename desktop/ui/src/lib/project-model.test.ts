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
  findDuplicateResourceHints,
  groupEnvVars,
  groupEvidence,
  groupFirstReason,
  groupHighestConfidence,
  groupIsAuto,
  groupIsMissing,
  groupLinks,
  groupLinksByTarget,
  groupProtocols,
  groupStatus,
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
    needs_review: false,
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
    needs_review: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeResource(overrides: Partial<SystemResource> = {}): SystemResource {
  return {
    id: "res1",
    kind: "database",
    vendor: "",
    name: "Database",
    identity_key: "database:res1",
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

describe("groupLinksByTarget", () => {
  it("groups links sharing a source component and a resolved resource target", () => {
    const links = [
      makeLink({ id: "l1", from_component_id: "c1", to_resource_id: "res1", protocol: "sql" }),
      makeLink({ id: "l2", from_component_id: "c1", to_resource_id: "res1", protocol: "sql" }),
    ];
    const groups = groupLinksByTarget(links);
    expect(groups).toHaveLength(1);
    expect(groups[0].links.map((l) => l.id)).toEqual(["l1", "l2"]);
  });

  it("groups links sharing a source component and a resolved component target", () => {
    const links = [
      makeLink({ id: "l1", from_component_id: "c1", to_component_id: "comp-x" }),
      makeLink({ id: "l2", from_component_id: "c1", to_component_id: "comp-x" }),
    ];
    expect(groupLinksByTarget(links)).toHaveLength(1);
  });

  it("never groups unresolved links, even when they share a source component", () => {
    const links = [
      makeLink({ id: "l1", from_component_id: "c1", hint: "billing-svc" }),
      makeLink({ id: "l2", from_component_id: "c1", hint: "billing-svc" }),
    ];
    const groups = groupLinksByTarget(links);
    expect(groups).toHaveLength(2);
  });

  it("keeps different targets and different source components separate", () => {
    const links = [
      makeLink({ id: "l1", from_component_id: "c1", to_resource_id: "res1" }),
      makeLink({ id: "l2", from_component_id: "c1", to_resource_id: "res2" }),
      makeLink({ id: "l3", from_component_id: "c2", to_resource_id: "res1" }),
    ];
    expect(groupLinksByTarget(links)).toHaveLength(3);
  });

  it("orders groups by first appearance", () => {
    const links = [
      makeLink({ id: "l1", from_component_id: "c1", to_resource_id: "res1" }),
      makeLink({ id: "l2", from_component_id: "c1", to_resource_id: "res2" }),
      makeLink({ id: "l3", from_component_id: "c1", to_resource_id: "res1" }),
    ];
    const groups = groupLinksByTarget(links);
    expect(groups.map((g) => g.links.map((l) => l.id))).toEqual([["l1", "l3"], ["l2"]]);
  });
});

describe("group status/badge/evidence helpers", () => {
  it("groupStatus prefers suggested, then confirmed, else dismissed", () => {
    expect(groupStatus([makeLink({ status: "dismissed" }), makeLink({ status: "suggested" })])).toBe("suggested");
    expect(groupStatus([makeLink({ status: "dismissed" }), makeLink({ status: "confirmed" })])).toBe("confirmed");
    expect(groupStatus([makeLink({ status: "dismissed" })])).toBe("dismissed");
  });

  it("groupProtocols de-duplicates in first-appearance order", () => {
    expect(groupProtocols([makeLink({ protocol: "sql" }), makeLink({ protocol: "http" }), makeLink({ protocol: "sql" })])).toEqual([
      "sql",
      "http",
    ]);
  });

  it("groupEnvVars unions without duplicates", () => {
    expect(
      groupEnvVars([makeLink({ env_vars: ["DATABASE_URL"] }), makeLink({ env_vars: ["DATABASE_URL", "PG_HOST"] })]),
    ).toEqual(["DATABASE_URL", "PG_HOST"]);
  });

  it("groupIsAuto is true only when every link is auto_confirmed", () => {
    expect(groupIsAuto([makeLink({ auto_confirmed: true }), makeLink({ auto_confirmed: true })])).toBe(true);
    expect(groupIsAuto([makeLink({ auto_confirmed: true }), makeLink({ auto_confirmed: false })])).toBe(false);
  });

  it("groupIsMissing is true only when every link is missing", () => {
    expect(groupIsMissing([makeLink({ missing: true }), makeLink({ missing: true })])).toBe(true);
    expect(groupIsMissing([makeLink({ missing: true }), makeLink({ missing: false })])).toBe(false);
  });

  it("groupHighestConfidence picks the strongest confidence in the group", () => {
    expect(groupHighestConfidence([makeLink({ confidence: "low" }), makeLink({ confidence: "high" })])).toBe("high");
  });

  it("groupFirstReason returns the first non-empty reason", () => {
    expect(groupFirstReason([makeLink({ reason: "" }), makeLink({ reason: "imports pg" })])).toBe("imports pg");
    expect(groupFirstReason([makeLink({})])).toBeUndefined();
  });

  it("groupEvidence concatenates every link's evidence", () => {
    const evidence = groupEvidence([
      makeLink({ evidence: [{ path: "a.go", line: 1 }] }),
      makeLink({ evidence: [{ path: "b.go" }] }),
    ]);
    expect(evidence).toEqual([{ path: "a.go", line: 1 }, { path: "b.go" }]);
  });
});

describe("findDuplicateResourceHints", () => {
  it("flags a component whose links resolve to ≥2 different resources of the same duplicate-prone kind", () => {
    const resources = [makeResource({ id: "res-db", kind: "database", name: "Database" }), makeResource({ id: "res-pg", kind: "database", name: "PostgreSQL" })];
    const links = [
      makeLink({ id: "l1", from_component_id: "c1", to_resource_id: "res-db", status: "confirmed" }),
      makeLink({ id: "l2", from_component_id: "c1", to_resource_id: "res-pg", status: "confirmed" }),
    ];
    const hints = findDuplicateResourceHints(links, resources);
    expect(hints).toEqual([{ componentId: "c1", kind: "database", resourceIds: expect.arrayContaining(["res-db", "res-pg"]) }]);
  });

  it("ignores dismissed links", () => {
    const resources = [makeResource({ id: "res-db" }), makeResource({ id: "res-pg" })];
    const links = [
      makeLink({ id: "l1", from_component_id: "c1", to_resource_id: "res-db", status: "confirmed" }),
      makeLink({ id: "l2", from_component_id: "c1", to_resource_id: "res-pg", status: "dismissed" }),
    ];
    expect(findDuplicateResourceHints(links, resources)).toEqual([]);
  });

  it("ignores kinds outside the duplicate-prone set", () => {
    const resources = [makeResource({ id: "res-a", kind: "api" }), makeResource({ id: "res-b", kind: "api" })];
    const links = [
      makeLink({ id: "l1", from_component_id: "c1", to_resource_id: "res-a", status: "confirmed" }),
      makeLink({ id: "l2", from_component_id: "c1", to_resource_id: "res-b", status: "confirmed" }),
    ];
    expect(findDuplicateResourceHints(links, resources)).toEqual([]);
  });

  it("narrows to the given component ids when provided", () => {
    const resources = [makeResource({ id: "res-db" }), makeResource({ id: "res-pg" })];
    const links = [
      makeLink({ id: "l1", from_component_id: "c1", to_resource_id: "res-db", status: "confirmed" }),
      makeLink({ id: "l2", from_component_id: "c1", to_resource_id: "res-pg", status: "confirmed" }),
    ];
    expect(findDuplicateResourceHints(links, resources, ["c2"])).toEqual([]);
    expect(findDuplicateResourceHints(links, resources, ["c1"])).toHaveLength(1);
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
