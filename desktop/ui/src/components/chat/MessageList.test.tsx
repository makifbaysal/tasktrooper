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

describe("MessageList on a long session", () => {
  it("renders only the newest messages until earlier ones are asked for", () => {
    const long: SessionMessage[] = Array.from({ length: 130 }, (_, i) => ({
      id: `m${i}`,
      role: "user",
      content: `message ${i}`,
      created_at: "2026-09-17T10:00:00Z",
    }));
    render(
      <I18nProvider>
        <MessageList messages={long} />
      </I18nProvider>,
    );
    expect(screen.queryByText("message 0")).toBeNull();
    expect(screen.getByText("message 129")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show 10 earlier" }));
    expect(screen.getByText("message 0")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Show \d+ earlier$/ })).toBeNull();
  });
});

describe("MessageList streaming reply", () => {
  it("shows every chunk as it arrives and nothing of it once the reply has landed", () => {
    const { rerender } = render(
      <I18nProvider>
        <MessageList messages={messages} isAwaitingResponse streamingContent="Partial answ" />
      </I18nProvider>,
    );
    expect(screen.getByText("Partial answ")).toBeInTheDocument();

    rerender(
      <I18nProvider>
        <MessageList messages={messages} isAwaitingResponse streamingContent="Partial answer, longer" />
      </I18nProvider>,
    );
    expect(screen.getByText("Partial answer, longer")).toBeInTheDocument();
    expect(screen.queryByText("Partial answ")).toBeNull();

    const landed: SessionMessage[] = [
      ...messages,
      { id: "m3", role: "assistant", content: "Final answer", created_at: "2026-09-17T10:02:00Z" },
    ];
    rerender(
      <I18nProvider>
        <MessageList messages={landed} streamingContent={null} />
      </I18nProvider>,
    );
    expect(screen.getByText("Final answer")).toBeInTheDocument();
    expect(screen.queryByText("Partial answer, longer")).toBeNull();
  });
});
