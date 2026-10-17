import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { AIJob } from "@/api";
import { AIJobCard } from "@/components/operations/ai/AIJobCard";
import { I18nProvider } from "@/hooks/useI18n";

function job(overrides: Partial<AIJob> = {}): AIJob {
  const now = new Date().toISOString();
  return {
    id: "j-1",
    kind: "define_errors",
    repository_id: "r-shop",
    component_id: null,
    environment: "production",
    status: "running",
    session_id: null,
    result_summary: "",
    error: "",
    created: [],
    created_at: now,
    started_at: now,
    updated_at: now,
    finished_at: null,
    ...overrides,
  };
}

function renderCard(value: AIJob) {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <AIJobCard job={value} refusal={null} startError={null} pollError={null} onDismiss={vi.fn()} />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("AIJobCard", () => {
  it("says a job is waiting for a slot until it takes one", () => {
    const view = renderCard(job({ started_at: null }));
    expect(screen.getByText(/At most two jobs run at once/)).toBeInTheDocument();
    expect(screen.getByText("Waiting for", { exact: false })).toBeInTheDocument();
    view.rerender(
      <MemoryRouter>
        <I18nProvider>
          <AIJobCard job={job()} refusal={null} startError={null} pollError={null} onDismiss={vi.fn()} />
        </I18nProvider>
      </MemoryRouter>,
    );
    expect(screen.queryByText(/At most two jobs run at once/)).toBeNull();
    expect(screen.getByText("Running for", { exact: false })).toBeInTheDocument();
  });

  it("shows a failed job's own message, a usage limit's included", () => {
    renderCard(job({ status: "failed", error: "Claude usage limit reached.\nIt resets at 17:00.", finished_at: new Date().toISOString() }));
    expect(screen.getByText(/Claude usage limit reached\./)).toHaveTextContent("It resets at 17:00.");
    expect(screen.getByText("Didn't finish")).toBeInTheDocument();
  });
});
