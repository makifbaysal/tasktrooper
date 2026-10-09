import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { IssueSyncSettings, Repository } from "@/api";
import { IssueSyncCard } from "@/components/admin/IssueSyncCard";
import { I18nProvider } from "@/hooks/useI18n";

// jsdom has no layout, so Radix Select's scroll-into-view-on-open crashes without it.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { getIssueSyncSettings, updateIssueSyncSettings, listRepositories, listJiraProjects } = vi.hoisted(() => ({
  getIssueSyncSettings: vi.fn(),
  updateIssueSyncSettings: vi.fn(),
  listRepositories: vi.fn(),
  listJiraProjects: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getIssueSyncSettings, updateIssueSyncSettings, listRepositories, listJiraProjects },
  };
});

const SETTINGS: IssueSyncSettings = {
  label: "tasktrooper",
  github_auto_import: false,
  jira_auto_import: false,
  write_back: true,
  convert_with_pm: true,
  jira_projects: [],
};

const REPOS: Repository[] = [
  {
    id: "repo-1",
    name: "web",
    description: "",
    root_path: "/repos/web",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
];

function renderCard() {
  return render(
    <I18nProvider>
      <IssueSyncCard />
    </I18nProvider>,
  );
}

describe("IssueSyncCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getIssueSyncSettings.mockResolvedValue(SETTINGS);
    listRepositories.mockResolvedValue({ repositories: REPOS });
    listJiraProjects.mockResolvedValue({ projects: [{ key: "ACME", name: "Acme" }] });
    updateIssueSyncSettings.mockImplementation((next: IssueSyncSettings) => Promise.resolve(next));
  });

  it("sends the whole settings object with a flipped switch", async () => {
    renderCard();

    const toggle = await screen.findByLabelText("Import labelled GitHub issues automatically");
    expect(toggle).toHaveAttribute("data-state", "unchecked");
    fireEvent.click(toggle);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateIssueSyncSettings).toHaveBeenCalledWith({
        ...SETTINGS,
        github_auto_import: true,
      }),
    );
  });

  it("turns the product manager's conversion off", async () => {
    renderCard();

    const toggle = await screen.findByLabelText("Let the product manager turn each issue into tasks");
    expect(toggle).toHaveAttribute("data-state", "checked");
    fireEvent.click(toggle);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateIssueSyncSettings).toHaveBeenCalledWith({ ...SETTINGS, convert_with_pm: false }),
    );
  });

  it("sends an edited label", async () => {
    renderCard();

    const label = await screen.findByLabelText("Label");
    fireEvent.change(label, { target: { value: "from-issues" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateIssueSyncSettings).toHaveBeenCalledWith({ ...SETTINGS, label: "from-issues" }),
    );
  });

  it("refuses to save an empty label", async () => {
    renderCard();

    const label = await screen.findByLabelText("Label");
    fireEvent.change(label, { target: { value: "  " } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Issues with this label become tasks automatically."),
    ).toBeInTheDocument();
    expect(updateIssueSyncSettings).not.toHaveBeenCalled();
  });

  it("falls back to a free-text project key when the project list is unavailable", async () => {
    listJiraProjects.mockRejectedValue(new Error("not connected"));
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: "Add mapping" }));

    expect(await screen.findByPlaceholderText("PROJECT")).toBeInTheDocument();
  });
});
