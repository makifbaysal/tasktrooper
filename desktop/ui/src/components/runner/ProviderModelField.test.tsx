import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";
import { ProviderModelField } from "./ProviderModelField";

const listModels = vi.fn();

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listModels: (p: string) => listModels(p) } };
});

function renderField(onCommit = vi.fn()) {
  render(
    <I18nProvider>
      <ProviderModelField provider="opencode" value="" onChange={() => {}} onCommit={onCommit} />
    </I18nProvider>,
  );
  return onCommit;
}

describe("ProviderModelField", () => {
  it("offers the provider's models as suggestions", async () => {
    listModels.mockResolvedValue({ object: "list", data: [{ id: "anthropic/claude-sonnet-5", object: "model" }] });
    const { container } = render(
      <I18nProvider>
        <ProviderModelField provider="opencode" value="" onChange={() => {}} />
      </I18nProvider>,
    );
    await waitFor(() => expect(container.querySelector("datalist option")).not.toBeNull());
    expect(container.querySelector("datalist option")?.getAttribute("value")).toBe("anthropic/claude-sonnet-5");
    expect(listModels).toHaveBeenCalledWith("opencode");
  });

  it("falls back to manual entry with a hint when the list cannot be fetched", async () => {
    listModels.mockRejectedValue(new Error("boom"));
    const onCommit = renderField();
    const input = screen.getByRole("combobox");
    await waitFor(() => expect(screen.getByText(/type a model name/i)).toBeInTheDocument());
    fireEvent.change(input, { target: { value: "my/model" } });
    fireEvent.blur(input);
    expect(onCommit).toHaveBeenCalled();
  });
});
