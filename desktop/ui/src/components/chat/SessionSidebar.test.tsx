import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SessionSidebar } from "@/components/chat/SessionSidebar";
import { I18nProvider } from "@/hooks/useI18n";
import type { Session } from "@/api";

const sessions: Session[] = [
  {
    id: "s1",
    title: "First session",
    model: "claude",
    created_at: "2026-09-17T10:00:00Z",
    updated_at: "2026-09-17T10:05:00Z",
  },
];

function renderSidebar() {
  render(
    <I18nProvider>
      <SessionSidebar
        sessions={sessions}
        activeSessionId={null}
        loading={false}
        onSelect={() => {}}
        onCreate={() => {}}
        onDelete={async () => {}}
      />
    </I18nProvider>,
  );
}

describe("SessionSidebar elevation", () => {
  it("shares the Card Level-1 raised shadow", () => {
    renderSidebar();
    const panel = screen.getByTestId("session-sidebar");
    expect(panel.className).toContain("shadow-[var(--shadow-raised)]");
  });

  it("renders the session's relative date using the caption type scale", () => {
    renderSidebar();
    const dateEl = screen.getByTestId("session-sidebar-date-s1");
    expect(dateEl.className).toContain("text-caption");
    expect(dateEl.className).not.toContain("text-xs");
  });
});
