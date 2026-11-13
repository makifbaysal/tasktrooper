import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AgentAvatar } from "@/components/agent/AgentAvatar";

const avatar = () => document.querySelector("[aria-hidden]")!;

describe("AgentAvatar", () => {
  it("shows a catalog agent's mascot by its slug", () => {
    render(<AgentAvatar name="Backend" slug="backend-developer" />);
    expect(avatar().getAttribute("data-agent-mascot")).toBe("backend-developer");
    expect(avatar().querySelector("img")).not.toBeNull();
  });

  it("recognises a catalog agent by a name that is its slug", () => {
    render(<AgentAvatar name="qa-agent" />);
    expect(avatar().getAttribute("data-agent-mascot")).toBe("qa-agent");
  });

  it("keeps initials for a custom agent", () => {
    render(<AgentAvatar name="Billing Helper" />);
    expect(avatar().querySelector("img")).toBeNull();
    expect(avatar().textContent).toBe("BH");
  });
});
