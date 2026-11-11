import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";

const { setAudienceRuntime, listLLMProviders, listModels } = vi.hoisted(() => ({
  setAudienceRuntime: vi.fn(),
  listLLMProviders: vi.fn(),
  listModels: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, setAudienceRuntime, listLLMProviders, listModels } };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { AudienceRuntimePicker } from "./AudienceRuntimePicker";

function renderPicker(runtime: import("@/api").AudienceRuntime, onChange = vi.fn()) {
  render(
    <I18nProvider>
      <AudienceRuntimePicker runtime={runtime} onChange={onChange} />
    </I18nProvider>,
  );
  return onChange;
}

beforeEach(() => {
  for (const f of [setAudienceRuntime, listLLMProviders, listModels]) f.mockReset();
  listLLMProviders.mockResolvedValue({ providers: [], endpoints: [] });
  listModels.mockResolvedValue({ data: [] });
});

describe("AudienceRuntimePicker", () => {
  it("says it follows the agent until the person picks one", () => {
    renderPicker({ ready: true, agent: "Product Manager", provider: "claude_code" });
    expect(screen.getByText(/Product Manager/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Use the agent's own" })).not.toBeInTheDocument();
  });

  it("shows the person's own pick and can hand it back to the agent", async () => {
    setAudienceRuntime.mockResolvedValue({ ready: true, agent: "Product Manager", provider: "claude_code" });
    const onChange = renderPicker({ ready: true, agent: "Audience research", provider: "openai", model: "gpt-5", chosen: true });
    expect(screen.getByText("openai · gpt-5")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    fireEvent.click(await screen.findByRole("button", { name: "Use the agent's own" }));

    await waitFor(() => expect(setAudienceRuntime).toHaveBeenCalledWith("", ""));
    expect(onChange).toHaveBeenCalledWith({ ready: true, agent: "Product Manager", provider: "claude_code" });
  });
});
