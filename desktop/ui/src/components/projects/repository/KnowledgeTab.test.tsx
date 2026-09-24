import "@testing-library/jest-dom/vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectNote, RepositoryModel } from "@/api";
import { KnowledgeTab } from "@/components/projects/repository/KnowledgeTab";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { saveNote, getRepositoryBrief } = vi.hoisted(() => ({
  saveNote: vi.fn(),
  getRepositoryBrief: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      saveNote,
      getRepositoryBrief,
    },
  };
});

const model: RepositoryModel = {
  repository: {
    id: "repo-1",
    name: "acme-platform",
    description: "",
    root_path: "/repo",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
  },
  shape: "single",
  components: [],
  checks: [],
  links: [],
  incoming_links: [],
  resources: [],
  linked_components: [],
  environments: [],
  notes: [],
  review: [],
};

const savedNote: ProjectNote = {
  id: "note-1",
  repository_id: "repo-1",
  topic: "purpose",
  body_md: "This repo runs the checkout flow.",
  stale: false,
  author: "user",
  locked: false,
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

function renderTab() {
  return render(
    <I18nProvider>
      <KnowledgeTab model={model} repositoryId="repo-1" onReload={vi.fn()} />
    </I18nProvider>,
  );
}

describe("KnowledgeTab", () => {
  beforeEach(() => {
    saveNote.mockReset().mockResolvedValue(savedNote);
    getRepositoryBrief.mockReset().mockResolvedValue({ brief: "# acme-platform" });
  });

  it("adding a note calls api.saveNote with the topic, component and body", async () => {
    renderTab();
    await waitFor(() => expect(getRepositoryBrief).toHaveBeenCalled());

    fireEvent.click(screen.getByText("Add note"));

    // The dialog's own Topic label only exists once it's open.
    await screen.findByText("Topic");

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "This repo runs the checkout flow." } });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() =>
      expect(saveNote).toHaveBeenCalledWith("repo-1", {
        component_id: undefined,
        topic: "purpose",
        body_md: "This repo runs the checkout flow.",
        locked: false,
      }),
    );
  });
});
