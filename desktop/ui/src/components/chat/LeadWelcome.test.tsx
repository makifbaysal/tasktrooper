import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { LeadWelcome } from "@/components/chat/LeadWelcome";
import { I18nProvider } from "@/hooks/useI18n";

const lead = { id: "pm", name: "Pia", enabled: true } as Agent;

function setup() {
  const onSubmit = vi.fn();
  render(
    <I18nProvider>
      <LeadWelcome agent={lead} onSubmit={onSubmit} />
    </I18nProvider>,
  );
  const textarea = screen.getByRole("textbox") as HTMLTextAreaElement;
  return { onSubmit, textarea };
}

describe("LeadWelcome", () => {
  it("submits the trimmed text on Enter", () => {
    const { onSubmit, textarea } = setup();
    fireEvent.change(textarea, { target: { value: "  ship it  " } });
    fireEvent.keyDown(textarea, { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledWith("ship it");
  });

  it("does not submit on Shift+Enter", () => {
    const { onSubmit, textarea } = setup();
    fireEvent.change(textarea, { target: { value: "line one" } });
    fireEvent.keyDown(textarea, { key: "Enter", shiftKey: true });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("sends the board suggestion directly", () => {
    const { onSubmit } = setup();
    const chips = screen.getAllByRole("button").filter((b) => b.getAttribute("type") === "button");
    fireEvent.click(chips[0]);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].trim()).not.toBe("");
  });

  it("only prefills the box for the feature suggestion", () => {
    const { onSubmit, textarea } = setup();
    const chips = screen.getAllByRole("button").filter((b) => b.getAttribute("type") === "button");
    fireEvent.click(chips[1]);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(textarea.value).not.toBe("");
    expect(textarea).toHaveFocus();
  });
});
