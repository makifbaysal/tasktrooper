import { describe, expect, it, vi } from "vitest";
import type { Agent, AttachmentMeta, Role } from "@/api";
import { agentInitials, productManagerIds, resolveLeadAgent, startLeadConversation } from "@/lib/leadAgent";

const { createSession } = vi.hoisted(() => ({ createSession: vi.fn() }));
vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return { ...actual, api: { ...actual.api, createSession } };
});

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

describe("startLeadConversation", () => {
  it("carries pending attachments into the new session as router state", async () => {
    createSession.mockReset().mockResolvedValue({ id: "s-1" });
    const navigate = vi.fn();
    const attachments: AttachmentMeta[] = [
      { id: "att-1", filename: "shot.png", content_type: "image/png", size_bytes: 10 } as AttachmentMeta,
    ];

    await startLeadConversation(navigate, "pm", "hello", "title", [], attachments);

    expect(navigate).toHaveBeenCalledWith("/agents/pm/chat/s-1", {
      state: { autoSend: "hello", autoSendMentions: [], autoSendAttachments: attachments },
    });
  });
});
