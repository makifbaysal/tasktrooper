import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type GitHubRunnerStatus } from "@/api";
import { GitHubRunnerSection } from "@/components/admin/GitHubRunnerSection";
import { I18nProvider } from "@/hooks/useI18n";

const { githubRunnerStatus, setGitHubRunner, toastError } = vi.hoisted(() => ({
  githubRunnerStatus: vi.fn(),
  setGitHubRunner: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, githubRunnerStatus, setGitHubRunner } };
});

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: toastError } }));

function status(overrides: Partial<GitHubRunnerStatus> = {}): GitHubRunnerStatus {
  return {
    supported: true,
    enabled: false,
    labels: ["self-hosted", "macOS", "ARM64", "tasktrooper"],
    version: "2.338.0",
    repos: [],
    candidates: ["acme/app", "acme/site"],
    ...overrides,
  };
}

function renderSection() {
  render(
    <I18nProvider>
      <GitHubRunnerSection />
    </I18nProvider>,
  );
}

describe("GitHubRunnerSection", () => {
  beforeEach(() => {
    githubRunnerStatus.mockReset();
    setGitHubRunner.mockReset();
    toastError.mockReset();
  });

  it("offers the projects' repositories and the runs-on line for this computer", async () => {
    githubRunnerStatus.mockResolvedValue(status());
    renderSection();
    expect(await screen.findByLabelText("acme/app")).toBeInTheDocument();
    expect(screen.getByLabelText("acme/site")).toBeInTheDocument();
    expect(screen.getByText("runs-on: [self-hosted, macOS, ARM64]")).toBeInTheDocument();
  });

  it("saves a picked repository and then the switch", async () => {
    githubRunnerStatus.mockResolvedValue(status());
    setGitHubRunner.mockResolvedValueOnce(status({ repos: [{ repo: "acme/app", state: "stopped" }] }));
    renderSection();
    fireEvent.click(await screen.findByLabelText("acme/app"));
    await waitFor(() => expect(setGitHubRunner).toHaveBeenCalledWith({ enabled: false, repos: ["acme/app"] }));

    setGitHubRunner.mockResolvedValueOnce(
      status({ enabled: true, repos: [{ repo: "acme/app", state: "idle" }] }),
    );
    fireEvent.click(screen.getByRole("switch"));
    await waitFor(() => expect(setGitHubRunner).toHaveBeenLastCalledWith({ enabled: true, repos: ["acme/app"] }));
    expect(await screen.findByText("Waiting for a job")).toBeInTheDocument();
  });

  it("words a refused public repository and keeps the list as it was", async () => {
    githubRunnerStatus.mockResolvedValue(status());
    setGitHubRunner.mockRejectedValue(new ApiError("acme/site: public_repo", 400, "github_runner_public_repo"));
    renderSection();
    const box = await screen.findByLabelText("acme/site");
    fireEvent.click(box);
    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        "acme/site is public. A runner there would run code from anyone's pull request on this computer.",
      ),
    );
    await waitFor(() => expect(box).not.toBeChecked());
  });

  it("says when this install cannot run a runner", async () => {
    githubRunnerStatus.mockResolvedValue(status({ supported: false, candidates: [] }));
    renderSection();
    expect(await screen.findByText(/not available here/)).toBeInTheDocument();
    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  });
});
