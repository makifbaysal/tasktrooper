import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { type DeploymentRun, type MatrixCell } from "@/api";
import { DeploymentStatusCell } from "@/components/operations/DeploymentStatusCell";
import { I18nProvider } from "@/hooks/useI18n";

function cellWithRun(run: Partial<DeploymentRun>): MatrixCell {
  return {
    env: "prod",
    configured: true,
    dispatchable: true,
    provider: "vercel",
    health_url: "",
    auto_rollback: false,
    rollback_sha: "",
    last_run: {
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
      ...run,
    },
  };
}

function renderCell(run: Partial<DeploymentRun>) {
  render(
    <I18nProvider>
      <MemoryRouter>
        <DeploymentStatusCell cell={cellWithRun(run)} repositoryId="repo-1" onSelect={() => {}} />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("DeploymentStatusCell status colors", () => {
  it("uses the success badge variant for a successful run", () => {
    renderCell({ status: "completed", conclusion: "success" });
    expect(screen.getByText("Success").className).toContain("text-success");
  });

  it("uses the destructive badge variant for a failed run", () => {
    renderCell({ status: "completed", conclusion: "failure" });
    expect(screen.getByText("Failed").className).toContain("text-destructive");
  });

  it("uses the info badge variant for a run in progress, not warning", () => {
    renderCell({ status: "in_progress", conclusion: "" });
    const badge = screen.getByText("Running");
    expect(badge.className).toContain("text-info");
    expect(badge.className).not.toContain("text-warning");
  });

  it("uses the warning badge variant for a completed-but-cancelled run", () => {
    renderCell({ status: "completed", conclusion: "cancelled" });
    expect(screen.getByText("Cancelled").className).toContain("text-warning");
  });
});
