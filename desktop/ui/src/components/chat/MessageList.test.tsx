import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MessageList } from "@/components/chat/MessageList";
import { I18nProvider } from "@/hooks/useI18n";
import type { SessionMessage } from "@/api";

const messages: SessionMessage[] = [
  { id: "m1", role: "user", content: "Hello there", created_at: "2026-09-17T10:00:00Z" },
  { id: "m2", role: "assistant", content: "Hi, how can I help?", created_at: "2026-09-17T10:01:00Z" },
];

function renderList() {
  render(
    <I18nProvider>
      <MessageList messages={messages} />
    </I18nProvider>,
  );
}

describe("MessageList metadata type scale", () => {
  it("renders the role label using the caption type scale", () => {
    renderList();
    const label = screen.getByText("You");
    expect(label.className).toContain("text-caption");
    expect(label.className).not.toContain("text-xs");
  });

  it("renders the timestamp using the micro type scale", () => {
    renderList();
    const label = screen.getByText("You");
    const timestamp = label.nextSibling as HTMLElement;
    expect(timestamp.className).toContain("text-micro");
  });

  it("leaves the user message body text unchanged", () => {
    renderList();
    const body = screen.getByText("Hello there");
    expect(body.className).toContain("text-sm");
  });
});

describe("MessageList queued messages", () => {
  const queued = [
    { id: "q1", content: "then also do this", mentions: [], attachments: [], fileIds: [], createdAt: "2026-09-17T10:02:00Z" },
    { id: "q2", content: "and that", mentions: [], attachments: [], fileIds: [], createdAt: "2026-09-17T10:03:00Z" },
  ];

  it("shows pending bubbles with a badge, one hint, and a working remove button", () => {
    const onRemoveQueued = vi.fn();
    render(
      <I18nProvider>
        <MessageList messages={messages} queued={queued} onRemoveQueued={onRemoveQueued} />
      </I18nProvider>,
    );
    expect(screen.getByText("then also do this")).toBeInTheDocument();
    expect(screen.getAllByText("Queued")).toHaveLength(2);
    expect(screen.getAllByText("Will be sent when the agent finishes its current work.")).toHaveLength(1);
    fireEvent.click(screen.getAllByRole("button", { name: "Remove from queue" })[1]);
    expect(onRemoveQueued).toHaveBeenCalledWith("q2");
  });
});
