import "@testing-library/jest-dom/vitest";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { DeploymentRun } from "@/api";
import { RunHistoryList } from "@/components/operations/RunHistoryList";
import { I18nProvider } from "@/hooks/useI18n";

function makeRun(overrides: Partial<DeploymentRun> = {}): DeploymentRun {
  return {
    id: "run-1",
    repository_id: "repo-1",
    env: "prod",
    run_id: 1,
    run_number: 1,
    workflow_file: "deploy.yml",
    head_sha: "abcdef1234",
    head_ref: "main",
    status: "completed",
    conclusion: "success",
    html_url: "",
    trigger_source: "ui",
    triggered_by: "someone",
    rollback_of_sha: "",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderList(run: Partial<DeploymentRun>) {
  return render(
    <I18nProvider>
      <RunHistoryList runs={[makeRun(run)]} />
    </I18nProvider>,
  );
}

describe("RunHistoryList status colors", () => {
  it("uses the info token, not warning, for an in-progress run", () => {
    const { container } = renderList({ status: "in_progress", conclusion: "" });
    const icon = container.querySelector("svg");
    expect(icon?.getAttribute("class")).toContain("text-info");
    expect(icon?.getAttribute("class")).not.toContain("text-warning");
  });

  it("uses the warning token for a completed-but-cancelled run, consistent with the matrix cell", () => {
    const { container } = renderList({ status: "completed", conclusion: "cancelled" });
    const icon = container.querySelector("svg");
    expect(icon?.getAttribute("class")).toContain("text-warning");
  });
});
