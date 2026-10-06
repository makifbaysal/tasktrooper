import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { GitHubCard } from "@/components/admin/GitHubCard";
import { I18nProvider } from "@/hooks/useI18n";

const { githubStatus } = vi.hoisted(() => ({ githubStatus: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, githubStatus } };
});

function renderCard() {
  render(
    <I18nProvider>
      <GitHubCard />
    </I18nProvider>,
  );
}

describe("GitHubCard", () => {
  beforeEach(() => githubStatus.mockReset());

  it("warns that a token without the workflow scope cannot touch CI", async () => {
    githubStatus.mockResolvedValue({ connected: true, login: "akif", missing_scopes: ["workflow"] });
    renderCard();

    expect(await screen.findByText("This token is missing: workflow")).toBeInTheDocument();
    expect(screen.getByText(/no agent can set up or fix CI/)).toBeInTheDocument();
  });

  it("reminds a fine-grained token of the Workflows permission", async () => {
    githubStatus.mockResolvedValue({ connected: true, login: "akif", fine_grained: true });
    renderCard();

    expect(await screen.findByText(/Workflows: Read and write/)).toBeInTheDocument();
  });

  it("says nothing more when the token has what it needs", async () => {
    githubStatus.mockResolvedValue({ connected: true, login: "akif" });
    renderCard();

    expect(await screen.findByText("akif")).toBeInTheDocument();
    expect(screen.getByText("Connected")).toBeInTheDocument();
    expect(screen.queryByText(/missing/)).not.toBeInTheDocument();
  });
});
