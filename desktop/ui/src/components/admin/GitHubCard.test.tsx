import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { GitHubConnectionStatus } from "@/api";
import { GitHubCard } from "@/components/admin/GitHubCard";
import { I18nProvider } from "@/hooks/useI18n";

const { githubStatus } = vi.hoisted(() => ({
  githubStatus: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, githubStatus },
  };
});

function renderCard() {
  return render(
    <I18nProvider>
      <GitHubCard />
    </I18nProvider>,
  );
}

describe("GitHubCard elevation", () => {
  beforeEach(() => {
    githubStatus.mockReset();
  });

  it("uses the Unit 1 raised Card elevation when disconnected", async () => {
    githubStatus.mockResolvedValue({ connected: false } satisfies GitHubConnectionStatus);
    renderCard();

    await waitFor(() => screen.getByText("Save token"));
    expect(screen.getByText("GitHub").closest(".rounded-xl")?.className).toContain(
      "shadow-[var(--shadow-raised)]",
    );
  });

  it("uses the same raised Card elevation when connected", async () => {
    githubStatus.mockResolvedValue({ connected: true, login: "octocat" } satisfies GitHubConnectionStatus);
    renderCard();

    await waitFor(() => screen.getByText("Disconnect"));
    expect(screen.getByText("GitHub").closest(".rounded-xl")?.className).toContain(
      "shadow-[var(--shadow-raised)]",
    );
  });
});
