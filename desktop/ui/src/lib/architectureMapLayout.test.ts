import { describe, expect, it } from "vitest";
import type { MapNode } from "@/api";
import { layoutMapNodes, toFlowEdges, toFlowNodes, visibleTiers } from "@/lib/architectureMapLayout";

function node(overrides: Partial<MapNode> & Pick<MapNode, "id" | "kind" | "tier" | "label">): MapNode {
  return { foreign: false, ...overrides };
}

describe("visibleTiers", () => {
  it("keeps client, service, data, external and drops library by default", () => {
    expect(visibleTiers(false)).toEqual(["client", "service", "data", "external"]);
  });

  it("includes library between service and data when shown", () => {
    expect(visibleTiers(true)).toEqual(["client", "service", "library", "data", "external"]);
  });
});

describe("layoutMapNodes", () => {
  it("orders tiers left to right by x, skipping the hidden library column", () => {
    const nodes = [
      node({ id: "c1", kind: "component", tier: "client", label: "web" }),
      node({ id: "s1", kind: "component", tier: "service", label: "api" }),
      node({ id: "d1", kind: "resource", tier: "data", label: "db" }),
      node({ id: "e1", kind: "resource", tier: "external", label: "stripe" }),
    ];
    const positioned = layoutMapNodes(nodes);
    const xById = Object.fromEntries(positioned.map((p) => [p.node.id, p.x]));
    expect(xById.c1).toBeLessThan(xById.s1);
    expect(xById.s1).toBeLessThan(xById.d1);
    expect(xById.d1).toBeLessThan(xById.e1);
  });

  it("drops library nodes when showLibraries is off", () => {
    const nodes = [
      node({ id: "lib1", kind: "component", tier: "library", label: "ui-kit" }),
      node({ id: "s1", kind: "component", tier: "service", label: "api" }),
    ];
    expect(layoutMapNodes(nodes, { showLibraries: false }).map((p) => p.node.id)).toEqual(["s1"]);
  });

  it("places the library column between service and data when shown, shifting data/external right", () => {
    const nodes = [
      node({ id: "s1", kind: "component", tier: "service", label: "api" }),
      node({ id: "lib1", kind: "component", tier: "library", label: "ui-kit" }),
      node({ id: "d1", kind: "resource", tier: "data", label: "db" }),
    ];
    const withLib = layoutMapNodes(nodes, { showLibraries: true });
    const withoutLib = layoutMapNodes(nodes, { showLibraries: false });
    const colWith = Object.fromEntries(withLib.map((p) => [p.node.id, p.column]));
    const colWithout = Object.fromEntries(withoutLib.map((p) => [p.node.id, p.column]));
    expect(colWith.s1).toBe(1);
    expect(colWith.lib1).toBe(2);
    expect(colWith.d1).toBe(3);
    // Without the library column, "data" shifts left into the vacated slot
    // instead of leaving a gap.
    expect(colWithout.d1).toBe(2);
  });

  it("groups component nodes by repository then path within a tier", () => {
    const nodes = [
      node({ id: "b-root", kind: "component", tier: "service", label: "root", repository_name: "beta", path: "." }),
      node({ id: "a-api", kind: "component", tier: "service", label: "api", repository_name: "alpha", path: "services/api" }),
      node({ id: "a-worker", kind: "component", tier: "service", label: "worker", repository_name: "alpha", path: "services/worker" }),
    ];
    const rows = layoutMapNodes(nodes).map((p) => p.node.id);
    // Both alpha rows are adjacent and precede beta (alphabetical repo grouping).
    expect(rows).toEqual(["a-api", "a-worker", "b-root"]);
  });

  it("groups resource nodes by kind, vendor and name", () => {
    const nodes = [
      node({ id: "r-cache", kind: "resource", tier: "data", label: "cache", resource_kind: "cache", vendor: "gcp" }),
      node({ id: "r-db", kind: "resource", tier: "data", label: "db", resource_kind: "database", vendor: "gcp" }),
    ];
    const rows = layoutMapNodes(nodes).map((p) => p.node.id);
    // "cache" sorts before "database" — grouping is alphabetical by kind, not insertion order.
    expect(rows).toEqual(["r-cache", "r-db"]);
  });

  it("keeps a foreign node in its own tier, distinguishable by node.foreign", () => {
    const nodes = [
      node({ id: "own", kind: "component", tier: "service", label: "api", repository_name: "alpha" }),
      node({
        id: "foreign",
        kind: "component",
        tier: "service",
        label: "billing-svc",
        repository_name: "billing",
        foreign: true,
        project_ids: ["proj-billing"],
      }),
    ];
    const positioned = layoutMapNodes(nodes);
    const foreign = positioned.find((p) => p.node.id === "foreign")!;
    const own = positioned.find((p) => p.node.id === "own")!;
    expect(foreign.tier).toBe("service");
    expect(foreign.column).toBe(own.column);
    expect(foreign.node.foreign).toBe(true);
  });

  it("assigns increasing y within a column, starting at the top padding", () => {
    const nodes = [
      node({ id: "s1", kind: "component", tier: "service", label: "a", repository_name: "a" }),
      node({ id: "s2", kind: "component", tier: "service", label: "b", repository_name: "b" }),
    ];
    const positioned = layoutMapNodes(nodes);
    expect(positioned[0].y).toBeLessThan(positioned[1].y);
    expect(positioned[0].row).toBe(0);
    expect(positioned[1].row).toBe(1);
  });
});

describe("toFlowNodes / toFlowEdges", () => {
  it("maps positioned nodes to non-draggable, non-connectable React Flow nodes typed by kind", () => {
    const positioned = layoutMapNodes([node({ id: "s1", kind: "component", tier: "service", label: "api" })]);
    const flowNodes = toFlowNodes(positioned);
    expect(flowNodes).toHaveLength(1);
    expect(flowNodes[0]).toMatchObject({ id: "s1", type: "component", draggable: false, connectable: false });
    expect(flowNodes[0].data.node.id).toBe("s1");
  });

  it("maps MapEdge to React Flow edges carrying the source edge in data", () => {
    const edges = toFlowEdges([
      { id: "e1", link_id: "l1", from: "s1", to: "d1", protocol: "sql", status: "confirmed", source: "scan", cross_project: false },
    ]);
    expect(edges).toEqual([
      {
        id: "e1",
        source: "s1",
        target: "d1",
        type: "map",
        data: {
          edge: { id: "e1", link_id: "l1", from: "s1", to: "d1", protocol: "sql", status: "confirmed", source: "scan", cross_project: false },
        },
        focusable: true,
      },
    ]);
  });
});

describe("column stagger", () => {
  it("drops odd columns half a row so skipped-column edges pass between nodes", () => {
    const nodes = [
      { id: "c:a", kind: "component", tier: "service", label: "api", foreign: false },
      { id: "r:db", kind: "resource", tier: "data", label: "db", foreign: false },
      { id: "r:ai", kind: "resource", tier: "external", label: "ai", foreign: false },
    ] as MapNode[];
    const ys = Object.fromEntries(layoutMapNodes(nodes).map((p) => [p.node.id, p.y]));
    expect(ys["r:db"]).not.toBe(ys["c:a"]);
    expect(ys["r:ai"]).toBe(ys["c:a"]);
  });
});
