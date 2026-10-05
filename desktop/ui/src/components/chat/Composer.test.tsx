import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Composer } from "@/components/chat/Composer";
import { I18nProvider } from "@/hooks/useI18n";

function renderComposer() {
  render(
    <I18nProvider>
      <Composer
        value=""
        onChange={() => {}}
        onSend={() => {}}
        sending={false}
        files={[]}
        selectedFileIds={[]}
        onToggleFile={() => {}}
      />
    </I18nProvider>,
  );
}

describe("Composer input focus states", () => {
  it("reuses the shared Textarea atom's focus/active ring classes", () => {
    renderComposer();
    const textarea = screen.getByPlaceholderText(/message/i);
    expect(textarea.className).toContain("focus-visible:ring-ring");
    expect(textarea.className).toContain("aria-invalid:border-destructive");
  });
});

describe("Composer queueing", () => {
  function renderQueueing(props: { queueing: boolean }) {
    const onSend = vi.fn();
    const onStop = vi.fn();
    render(
      <I18nProvider>
        <Composer
          value="next thing"
          onChange={() => {}}
          onSend={onSend}
          sending
          onStop={onStop}
          files={[]}
          selectedFileIds={[]}
          onToggleFile={() => {}}
          {...props}
        />
      </I18nProvider>,
    );
    return { onSend, onStop };
  }

  it("keeps the textarea usable while sending and shows both the queue and the stop button", () => {
    const { onSend, onStop } = renderQueueing({ queueing: true });
    const textarea = screen.getByPlaceholderText(/message/i);
    expect(textarea).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Add to queue" }));
    expect(onSend).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(onStop).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(textarea, { key: "Enter" });
    expect(onSend).toHaveBeenCalledTimes(2);
  });

  it("still locks and swaps send for stop when not queueing", () => {
    renderQueueing({ queueing: false });
    expect(screen.getByPlaceholderText(/message/i)).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Add to queue" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Send" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument();
  });
});
