import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SessionRun } from "@/api";
import { SessionActivityPanel } from "@/components/chat/SessionActivityPanel";
import { I18nProvider } from "@/hooks/useI18n";

vi.mock("@/hooks/useSessionActivity", () => ({
  useSessionActivity: (runs: SessionRun[]) => ({
    inputs: runs.map((run) => ({
      runId: run.id,
      live: false,
      startedAt: run.started_at,
      steps: [
        { id: "1", run_id: run.id, step_type: "user_message", payload: { content: "analyse login" }, created_at: "2026-01-01T10:00:00Z" },
        { id: "2", run_id: run.id, step_type: "assistant_message", payload: { content: "All analysed" }, created_at: "2026-01-01T10:00:05Z" },
      ],
      plan: null,
    })),
    loading: false,
    anyLive: false,
  }),
}));

const runs: SessionRun[] = [
  { id: "r1", session_id: "s1", request_id: "q", status: "completed", started_at: "2026-01-01T10:00:00Z", completed_at: "2026-01-01T10:00:09Z" },
];

function renderPanel(props: Partial<React.ComponentProps<typeof SessionActivityPanel>> = {}) {
  return render(
    <I18nProvider>
      <SessionActivityPanel sessionId="s1" agentName="Product Manager" lead runs={runs} sending={false} {...props} />
    </I18nProvider>,
  );
}

describe("SessionActivityPanel", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("starts as a rail when the stored state is closed and opens on click, remembering it", () => {
    window.localStorage.setItem("tt.chat.activityPanel.open", "0");
    renderPanel();
    expect(screen.queryByRole("complementary")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Open the activity panel" }));

    expect(screen.getByRole("complementary", { name: "Agent activity" })).toBeInTheDocument();
    expect(screen.getByText("analyse login")).toBeInTheDocument();
    expect(screen.getByText("Product Manager")).toBeInTheDocument();
    expect(window.localStorage.getItem("tt.chat.activityPanel.open")).toBe("1");
  });

  it("collapses back to the rail", () => {
    window.localStorage.setItem("tt.chat.activityPanel.open", "1");
    renderPanel();
    fireEvent.click(screen.getByRole("button", { name: "Collapse the activity panel" }));
    expect(screen.queryByRole("complementary")).toBeNull();
    expect(window.localStorage.getItem("tt.chat.activityPanel.open")).toBe("0");
  });

  it("shows the empty hint and a starting header while a first message is being sent", () => {
    window.localStorage.setItem("tt.chat.activityPanel.open", "1");
    renderPanel({ runs: [], sending: true });
    expect(screen.getAllByText("Starting…").length).toBeGreaterThan(0);
  });

  it("offers Stop only while live", () => {
    window.localStorage.setItem("tt.chat.activityPanel.open", "1");
    const onStop = vi.fn();
    renderPanel({ sending: true, onStop });
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(onStop).toHaveBeenCalled();
  });
});
