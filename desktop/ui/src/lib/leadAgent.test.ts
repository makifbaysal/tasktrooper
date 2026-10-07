import { describe, expect, it } from "vitest";
import type { Agent, Role } from "@/api";
import { agentInitials, productManagerIds, resolveLeadAgent } from "@/lib/leadAgent";

const agent = (id: string, extra: Partial<Agent> = {}): Agent =>
  ({ id, name: id, enabled: true, catalog_slug: undefined, ...extra }) as Agent;

const pmRole = (assignments: Role["assignments"]): Role =>
  ({ id: "r-pm", key: "product_manager", name: "Product Manager", assignments, purposes: [] }) as unknown as Role;

describe("resolveLeadAgent", () => {
  it("picks the first enabled any-area assignee of the product_manager role", () => {
    const agents = [agent("a"), agent("b"), agent("c")];
    const roles = [
      pmRole([
        { agent_id: "a", areas: ["mobile"], priority: 0 },
        { agent_id: "b", areas: null, priority: 1 },
      ]),
    ];
    expect(resolveLeadAgent(agents, roles)?.id).toBe("b");
  });

  it("skips a disabled assignee", () => {
    const agents = [agent("a", { enabled: false }), agent("b")];
    const roles = [
      pmRole([
        { agent_id: "a", areas: null, priority: 0 },
        { agent_id: "b", areas: null, priority: 1 },
      ]),
    ];
    expect(resolveLeadAgent(agents, roles)?.id).toBe("b");
  });

  it("falls back to the catalog product-manager when roles are unavailable", () => {
    const agents = [agent("x"), agent("pm", { catalog_slug: "product-manager" })];
    expect(resolveLeadAgent(agents, null)?.id).toBe("pm");
  });

  it("returns null when nobody can lead", () => {
    expect(resolveLeadAgent([agent("x")], [])).toBeNull();
  });
});

describe("agentInitials", () => {
  it("takes the first letter of the first two words", () => {
    expect(agentInitials("product-manager")).toBe("PM");
    expect(agentInitials("qa-agent")).toBe("QA");
    expect(agentInitials("System Architect")).toBe("SA");
  });

  it("uses two letters of a single word", () => {
    expect(agentInitials("ops")).toBe("OP");
  });
});

describe("productManagerIds", () => {
  it("lists every holder of the product_manager role, disabled or area-scoped included", () => {
    const agents = [agent("a", { enabled: false }), agent("b"), agent("c")];
    const roles = [
      pmRole([
        { agent_id: "a", areas: ["mobile"], priority: 0 },
        { agent_id: "b", areas: null, priority: 1 },
        { agent_id: "b", areas: ["web"], priority: 2 },
      ]),
    ];
    expect(productManagerIds(agents, roles)).toEqual(["a", "b"]);
  });

  it("is empty when the role list has no product manager", () => {
    expect(productManagerIds([agent("pm", { catalog_slug: "product-manager" })], [])).toEqual([]);
  });

  it("falls back to the catalog product-manager when roles are unavailable", () => {
    const agents = [agent("x"), agent("pm", { catalog_slug: "product-manager" })];
    expect(productManagerIds(agents, null)).toEqual(["pm"]);
  });
});
