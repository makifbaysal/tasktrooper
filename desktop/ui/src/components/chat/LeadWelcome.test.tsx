import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { LeadWelcome } from "@/components/chat/LeadWelcome";
import type { MentionOption } from "@/components/chat/MentionMenu";
import { I18nProvider } from "@/hooks/useI18n";

const { uploadAttachment } = vi.hoisted(() => ({ uploadAttachment: vi.fn() }));
vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return { ...actual, api: { ...actual.api, uploadAttachment } };
});

const lead = { id: "pm", name: "Pia", enabled: true } as Agent;

const repo: MentionOption = { kind: "repository", id: "r-1", name: "acme-web" };

function makeFile(name: string, type: string) {
  return new File(["x"], name, { type });
}

function setup() {
  const onSubmit = vi.fn();
  render(
    <I18nProvider>
      <LeadWelcome agent={lead} onSubmit={onSubmit} mentionOptions={[repo]} />
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
    expect(onSubmit).toHaveBeenCalledWith("ship it", [], []);
  });

  it("does not submit on Shift+Enter", () => {
    const { onSubmit, textarea } = setup();
    fireEvent.change(textarea, { target: { value: "line one" } });
    fireEvent.keyDown(textarea, { key: "Enter", shiftKey: true });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("sends the board suggestion directly", () => {
    const { onSubmit } = setup();
    const chips = screen.getAllByRole("button").filter((b) => b.className.includes("rounded-full"));
    fireEvent.click(chips[0]);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].trim()).not.toBe("");
  });

  it("only prefills the box for the feature suggestion", () => {
    const { onSubmit, textarea } = setup();
    const chips = screen.getAllByRole("button").filter((b) => b.className.includes("rounded-full"));
    fireEvent.click(chips[1]);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(textarea.value).not.toBe("");
    expect(textarea).toHaveFocus();
  });

  it("tags an entity with @ and submits it alongside the text", () => {
    const { onSubmit, textarea } = setup();
    fireEvent.change(textarea, { target: { value: "@acm", selectionStart: 4 } });
    fireEvent.keyDown(textarea, { key: "Enter" });
    expect(onSubmit).not.toHaveBeenCalled();
    expect(textarea.value).toBe("@acme-web ");

    fireEvent.change(textarea, { target: { value: "@acme-web kupon ekleyelim" } });
    fireEvent.keyDown(textarea, { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledWith("@acme-web kupon ekleyelim", [repo], []);
  });

  it("mentions @ tagging in the hint", () => {
    setup();
    expect(screen.getByText(/@/)).toBeInTheDocument();
  });

  it("pastes an image onto the box and sends it as a pending attachment with the message", async () => {
    const meta = { id: "att-1", filename: "shot.png", content_type: "image/png", size_bytes: 10 };
    uploadAttachment.mockReset().mockResolvedValue(meta);
    const { onSubmit, textarea } = setup();

    const file = makeFile("shot.png", "image/png");
    fireEvent.paste(textarea, { clipboardData: { files: [file] } });

    await waitFor(() => expect(screen.getByTitle("shot.png")).toBeInTheDocument());

    fireEvent.change(textarea, { target: { value: "look at this" } });
    fireEvent.keyDown(textarea, { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledWith("look at this", [], [meta]);
  });

  it("adds a file via the attach button", async () => {
    const meta = { id: "att-2", filename: "spec.pdf", content_type: "application/pdf", size_bytes: 20 };
    uploadAttachment.mockReset().mockResolvedValue(meta);
    const { onSubmit, textarea } = setup();

    const file = makeFile("spec.pdf", "application/pdf");
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => expect(screen.getByTitle("spec.pdf")).toBeInTheDocument());

    fireEvent.change(textarea, { target: { value: "see attached" } });
    fireEvent.keyDown(textarea, { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledWith("see attached", [], [meta]);
  });
});
