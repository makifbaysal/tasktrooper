import { describe, expect, it } from "vitest";
import type { WorkspaceMap, WorkspaceMapProject } from "@/api";
import { buildWorkspaceMapGraph, isProjectLinked } from "@/lib/workspaceMapLayout";

function project(overrides: Partial<WorkspaceMapProject> & Pick<WorkspaceMapProject, "id" | "name">): WorkspaceMapProject {
  return { type: "single_repo", repositories: [], ...overrides };
}

function baseMap(overrides: Partial<WorkspaceMap> = {}): WorkspaceMap {
  return {
    projects: [project({ id: "proj-shop", name: "Acme Shop" }), project({ id: "proj-billing", name: "Acme Billing" }), project({ id: "proj-tt", name: "TaskTrooper" })],
    unassigned: [],
    edges: [],
    shared_resources: [],
    ...overrides,
  };
}

describe("isProjectLinked", () => {
  it("is true for either end of a workspace edge", () => {
    const map = baseMap({
      edges: [{ from_project_id: "proj-shop", to_project_id: "proj-billing", links: 1, suggested: 0, protocols: ["http"], examples: [] }],
    });
    expect(isProjectLinked("proj-shop", map)).toBe(true);
    expect(isProjectLinked("proj-billing", map)).toBe(true);
    expect(isProjectLinked("proj-tt", map)).toBe(false);
  });

  it("is true for a project referenced by a shared resource, even with no direct edge", () => {
    const map = baseMap({
      shared_resources: [{ resource: { id: "res-1", kind: "api", name: "Stripe" }, project_ids: ["proj-shop", "proj-billing"] }],
    });
    expect(isProjectLinked("proj-shop", map)).toBe(true);
    expect(isProjectLinked("proj-tt", map)).toBe(false);
  });
});

describe("buildWorkspaceMapGraph", () => {
  it("creates one edge per workspace edge, connecting the two project group nodes", () => {
    const map = baseMap({
      edges: [{ from_project_id: "proj-shop", to_project_id: "proj-billing", links: 3, suggested: 1, protocols: ["http"], examples: [] }],
    });
    const { edges } = buildWorkspaceMapGraph(map);
    expect(edges).toHaveLength(1);
    expect(edges[0]).toMatchObject({ source: "p:proj-shop", target: "p:proj-billing", label: "3 · +1" });
  });

  it("renders no cross-project edge at all for an independent project", () => {
    const map = baseMap({
      edges: [{ from_project_id: "proj-shop", to_project_id: "proj-billing", links: 1, suggested: 0, protocols: ["http"], examples: [] }],
    });
    const { edges } = buildWorkspaceMapGraph(map);
    expect(edges.some((e) => e.source === "p:proj-tt" || e.target === "p:proj-tt")).toBe(false);
  });

  it("marks every project not in edges/shared_resources as independent in its node data", () => {
    const map = baseMap({
      edges: [{ from_project_id: "proj-shop", to_project_id: "proj-billing", links: 1, suggested: 0, protocols: ["http"], examples: [] }],
    });
    const { nodes } = buildWorkspaceMapGraph(map);
    const byId = new Map(nodes.map((n) => [n.id, n]));
    expect((byId.get("p:proj-shop")?.data as { independent: boolean }).independent).toBe(false);
    expect((byId.get("p:proj-billing")?.data as { independent: boolean }).independent).toBe(false);
    expect((byId.get("p:proj-tt")?.data as { independent: boolean }).independent).toBe(true);
  });

  it("connects a shared resource node only to the linked projects that use it", () => {
    const map = baseMap({
      shared_resources: [{ resource: { id: "res-1", kind: "api", name: "Stripe" }, project_ids: ["proj-shop", "proj-billing"] }],
    });
    const { nodes, edges } = buildWorkspaceMapGraph(map);
    expect(nodes.some((n) => n.id === "res:res-1")).toBe(true);
    const resourceEdges = edges.filter((e) => e.source === "res:res-1");
    expect(resourceEdges.map((e) => e.target).sort()).toEqual(["p:proj-billing", "p:proj-shop"]);
  });

  it("places every child repository node after its parent project node", () => {
    const map = baseMap({
      projects: [
        project({
          id: "proj-shop",
          name: "Acme Shop",
          repositories: [
            { id: "repo-1", name: "acme-platform", shape: "monorepo", components: [] },
            { id: "repo-2", name: "acme-mobile", shape: "single", components: [] },
          ],
        }),
      ],
    });
    const { nodes } = buildWorkspaceMapGraph(map);
    const parentIndex = nodes.findIndex((n) => n.id === "p:proj-shop");
    const childIndices = nodes
      .map((n, i) => (n.parentId === "p:proj-shop" ? i : -1))
      .filter((i) => i >= 0);
    expect(childIndices.every((i) => i > parentIndex)).toBe(true);
    expect(childIndices).toHaveLength(2);
  });
});
