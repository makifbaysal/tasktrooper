import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type DeploymentRun, type MatrixCell, type MatrixView, type ProjectsOverview } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { CACHE_DEPLOY_MATRIX } from "@/hooks/useWorkflowDeploys";
import { writeCache } from "@/lib/uiCache";

const apiMock = vi.hoisted(() => ({
  getDeployMatrix: vi.fn(),
  listDeployRuns: vi.fn(),
  dispatchDeploy: vi.fn(),
  rollbackDeploy: vi.fn(),
  getProjectsOverview: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});

import { WorkflowDeploysSection } from "@/components/operations/changes/WorkflowDeploysSection";

const lastRun = {
  id: "run-1",
  repository_id: "r-api",
  env: "prod",
  run_id: 1,
  run_number: 42,
  workflow_file: "deploy.yml",
  head_sha: "abcdef1234567",
  head_ref: "main",
  status: "completed",
  conclusion: "success",
  html_url: "https://github.com/acme/api/actions/runs/1",
  trigger_source: "ui",
  triggered_by: "system",
  rollback_of_sha: "",
  created_at: "2026-10-16T10:00:00Z",
  updated_at: "2026-10-16T10:05:00Z",
} as DeploymentRun;

const cell = (env: MatrixCell["env"], overrides: Partial<MatrixCell> = {}): MatrixCell => ({
  env,
  configured: true,
  dispatchable: true,
  provider: "github",
  health_url: "",
  auto_rollback: false,
  rollback_sha: "",
  ...overrides,
});

const matrix: MatrixView = {
  envs: ["local", "stage", "preprod", "prod"],
  repos: [{ id: "r-api", name: "api", kind: "service", cells: [cell("prod", { last_run: lastRun, rollback_sha: "1111111aaaa" }), cell("stage")] }],
};

function renderSection(environment: "production" | "staging" = "production") {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <WorkflowDeploysSection environment={environment} />
      </I18nProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  sessionStorage.clear();
  writeCache(CACHE_DEPLOY_MATRIX, undefined);
  writeCache("operations.entities.overview", undefined);
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  apiMock.getDeployMatrix.mockResolvedValue(matrix);
  apiMock.getProjectsOverview.mockResolvedValue({ projects: [], unassigned: [] } as unknown as ProjectsOverview);
  apiMock.listDeployRuns.mockResolvedValue([lastRun]);
  apiMock.dispatchDeploy.mockResolvedValue(undefined);
  apiMock.rollbackDeploy.mockResolvedValue(undefined);
});

describe("WorkflowDeploysSection", () => {
  it("shows the environment's last run per repository", async () => {
    renderSection();
    const table = await screen.findByRole("table", { name: "Workflow deploys" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(1);
    expect(within(rows[0]).getByText("Succeeded")).toBeInTheDocument();
    expect(within(rows[0]).getByRole("link", { name: "#42" })).toHaveAttribute("href", lastRun.html_url);
    expect(within(rows[0]).getByText("abcdef1")).toBeInTheDocument();
  });

  it("renders nothing when no target can be dispatched or has run", async () => {
    apiMock.getDeployMatrix.mockResolvedValue({ envs: matrix.envs, repos: [{ id: "r", name: "x", kind: "", cells: [cell("prod", { dispatchable: false })] }] });
    const { container } = renderSection();
    await waitFor(() => expect(apiMock.getDeployMatrix).toHaveBeenCalled());
    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });

  it("asks the typed name to deploy to prod and sends what was typed with the ref", async () => {
    renderSection();
    const table = await screen.findByRole("table", { name: "Workflow deploys" });
    fireEvent.click(within(table).getByRole("button", { name: "Deploy…" }));
    const dialog = await screen.findByRole("dialog", { name: "Deploy api to Production?" });
    fireEvent.change(within(dialog).getByLabelText("Git ref"), { target: { value: "v1.2.0" } });
    const go = within(dialog).getByRole("button", { name: "Deploy" });
    expect(go).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Type api to confirm"), { target: { value: "api" } });
    await act(async () => {
      fireEvent.click(go);
    });
    expect(apiMock.dispatchDeploy).toHaveBeenCalledWith("r-api", "prod", { ref: "v1.2.0", confirm: "api" });
  });

  it("deploys outside prod with no phrase, and rolls back only with one", async () => {
    apiMock.getDeployMatrix.mockResolvedValue({
      ...matrix,
      repos: [{ id: "r-api", name: "api", kind: "service", cells: [cell("stage", { last_run: { ...lastRun, env: "stage" }, rollback_sha: "1111111aaaa" })] }],
    });
    renderSection("staging");
    const table = await screen.findByRole("table", { name: "Workflow deploys" });
    fireEvent.click(within(table).getByRole("button", { name: "Deploy…" }));
    let dialog = await screen.findByRole("dialog", { name: "Deploy api to Staging?" });
    expect(within(dialog).queryByLabelText("Type api to confirm")).not.toBeInTheDocument();
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Deploy" }));
    });
    expect(apiMock.dispatchDeploy).toHaveBeenCalledWith("r-api", "stage", { ref: undefined, confirm: undefined });

    fireEvent.click(within(table).getByRole("button", { name: "Roll back…" }));
    dialog = await screen.findByRole("dialog", { name: "Roll api back in Staging?" });
    expect(dialog).toHaveTextContent("1111111");
    const go = within(dialog).getByRole("button", { name: "Roll back" });
    expect(go).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Type api to confirm"), { target: { value: "api" } });
    await act(async () => {
      fireEvent.click(go);
    });
    expect(apiMock.rollbackDeploy).toHaveBeenCalledWith("r-api", "stage", "api");
  });

  it("keeps the dialog open with the server's refusal", async () => {
    apiMock.rollbackDeploy.mockRejectedValue(new ApiError("no previous successful deploy to roll back to", 409));
    renderSection();
    const table = await screen.findByRole("table", { name: "Workflow deploys" });
    fireEvent.click(within(table).getByRole("button", { name: "Roll back…" }));
    const dialog = await screen.findByRole("dialog", { name: "Roll api back in Production?" });
    fireEvent.change(within(dialog).getByLabelText("Type api to confirm"), { target: { value: "api" } });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Roll back" }));
    });
    expect(screen.getByRole("dialog", { name: "Roll api back in Production?" })).toHaveTextContent("no previous successful deploy to roll back to");
  });

  it("lists a cell's runs in the side panel", async () => {
    renderSection();
    const table = await screen.findByRole("table", { name: "Workflow deploys" });
    fireEvent.click(within(table).getByRole("button", { name: "Runs of api in Production" }));
    const panel = await screen.findByRole("dialog", { name: "api · Production" });
    expect(apiMock.listDeployRuns).toHaveBeenCalledWith("r-api", "prod", 20);
    expect(await within(panel).findByRole("link", { name: /#42/ })).toHaveAttribute("href", lastRun.html_url);
  });
});
