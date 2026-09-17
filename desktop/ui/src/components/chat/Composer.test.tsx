import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
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
