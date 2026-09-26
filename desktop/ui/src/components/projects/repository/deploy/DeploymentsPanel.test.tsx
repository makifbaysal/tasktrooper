import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CloudDeployment } from "@/api";
import { DeploymentsPanel } from "@/components/projects/repository/deploy/DeploymentsPanel";
import { I18nProvider } from "@/hooks/useI18n";

const { getEnvironmentDeployments } = vi.hoisted(() => ({ getEnvironmentDeployments: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getEnvironmentDeployments } };
});

function renderPanel() {
  return render(
    <I18nProvider>
      <DeploymentsPanel envId="env-prev" />
    </I18nProvider>,
  );
}

describe("DeploymentsPanel", () => {
  beforeEach(() => {
    getEnvironmentDeployments.mockReset();
  });

  it("shows a preview deployment's branch, PR, commit, state and both addresses", async () => {
    const preview: CloudDeployment = {
      id: "dpl_1",
      status: "ready",
      environment: "preview",
      commit_sha: "abcdef1234567",
      commit_message: "Add checkout",
      branch: "feat/checkout",
      url: "https://web-app-abc123-acme.vercel.app",
      branch_url: "https://web-app-git-feat-checkout-acme.vercel.app",
      pr_number: 42,
      created_at: "2024-01-01T00:00:00Z",
    };
    getEnvironmentDeployments.mockResolvedValue({ deployments: [preview] });
    renderPanel();

    await screen.findByText("PR #42");
    expect(getEnvironmentDeployments).toHaveBeenCalledWith("env-prev", { limit: 30 });
    expect(screen.getByText("Ready")).toBeInTheDocument();
    expect(screen.getByText("abcdef1")).toBeInTheDocument();
    expect(screen.getByText(/feat\/checkout/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Branch address/ })).toHaveAttribute("href", preview.branch_url);
    expect(screen.getByRole("link", { name: /This commit/ })).toHaveAttribute("href", preview.url);
  });

  it("leaves out the PR badge and branch address when the deployment has neither", async () => {
    const production: CloudDeployment = {
      id: "dpl_2",
      status: "building",
      environment: "production",
      commit_sha: "1234567abcdef",
      branch: "main",
      created_at: "2024-01-01T00:00:00Z",
    };
    getEnvironmentDeployments.mockResolvedValue({ deployments: [production] });
    renderPanel();

    await screen.findByText("Building");
    expect(screen.queryByText(/^PR #/)).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Branch address/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /This commit/ })).not.toBeInTheDocument();
  });
});
