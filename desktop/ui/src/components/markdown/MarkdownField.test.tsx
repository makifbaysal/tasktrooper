import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState, type ComponentProps } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MentionOption } from "@/components/chat/MentionMenu";
import { MarkdownField } from "@/components/markdown/MarkdownField";
import { I18nProvider } from "@/hooks/useI18n";

const calls = vi.hoisted(() => ({
  uploadAttachment: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, uploadAttachment: calls.uploadAttachment } };
});

const mentionOptions: MentionOption[] = [
  { id: "a-1", name: "Reviewer", kind: "agent" },
  { id: "p-1", name: "Payments", kind: "project" },
];

function Field(props: Partial<ComponentProps<typeof MarkdownField>> & { value?: string }) {
  const [value, setValue] = useState(props.value ?? "");
  return (
    <I18nProvider>
      <MarkdownField value={value} onChange={setValue} {...props} />
    </I18nProvider>
  );
}

describe("MarkdownField without mention/attachment props", () => {
  it("renders a plain textarea with no mention menu and no attach button", () => {
    render(<Field placeholder="Say something" />);
    const field = screen.getByPlaceholderText("Say something");
    fireEvent.change(field, { target: { value: "hello @Rev" } });
    expect(screen.queryByRole("button", { name: /attachment/i })).not.toBeInTheDocument();
    expect(field).toHaveValue("hello @Rev");
  });
});

describe("MarkdownField with mentionOptions", () => {
  beforeEach(() => {
    calls.uploadAttachment.mockReset();
  });

  it("opens the mention menu on '@' and applies the selected option", () => {
    const onMentionSelect = vi.fn();
    render(<Field mentionOptions={mentionOptions} onMentionSelect={onMentionSelect} placeholder="Comment" />);
    const field = screen.getByPlaceholderText("Comment");
    fireEvent.change(field, { target: { value: "hey @Rev" } });
    fireEvent.click(field);

    const option = screen.getByText("Reviewer");
    fireEvent.mouseDown(option);

    expect(onMentionSelect).toHaveBeenCalledWith(mentionOptions[0]);
    expect(field).toHaveValue("hey @Reviewer ");
  });

  it("still inserts a newline on Enter instead of submitting", () => {
    render(<Field mentionOptions={mentionOptions} placeholder="Comment" />);
    const field = screen.getByPlaceholderText("Comment") as HTMLTextAreaElement;
    fireEvent.change(field, { target: { value: "line one" } });
    fireEvent.keyDown(field, { key: "Enter" });
    expect(field).toHaveValue("line one");
  });
});

describe("MarkdownField with attachments enabled", () => {
  beforeEach(() => {
    calls.uploadAttachment.mockReset();
  });

  it("uploads a picked file and shows it as a pending-attachment chip", async () => {
    calls.uploadAttachment.mockResolvedValue({
      id: "att-1",
      filename: "notes.pdf",
      content_type: "application/pdf",
      size_bytes: 10,
      sha256: "x",
      created_by_type: "user",
      created_at: "2026-01-01T00:00:00Z",
    });
    const onPendingAttachmentsChange = vi.fn();
    render(
      <Field
        attachmentsEnabled
        pendingAttachments={[]}
        onPendingAttachmentsChange={onPendingAttachmentsChange}
      />,
    );

    const file = new File(["content"], "notes.pdf", { type: "application/pdf" });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => expect(onPendingAttachmentsChange).toHaveBeenCalled());
    expect(calls.uploadAttachment).toHaveBeenCalledWith(file, undefined);
  });

  it("shows a pending attachment passed in as a removable chip", () => {
    const onPendingAttachmentsChange = vi.fn();
    render(
      <Field
        attachmentsEnabled
        pendingAttachments={[
          {
            id: "att-1",
            filename: "notes.pdf",
            content_type: "application/pdf",
            size_bytes: 10,
            sha256: "x",
            created_by_type: "user",
            created_at: "2026-01-01T00:00:00Z",
          },
        ]}
        onPendingAttachmentsChange={onPendingAttachmentsChange}
      />,
    );

    expect(screen.getByText("notes.pdf")).toBeInTheDocument();
  });
});
