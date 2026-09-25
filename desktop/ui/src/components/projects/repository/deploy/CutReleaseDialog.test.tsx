import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Release, ReleaseCutPreview } from "@/api";
import { CutReleaseDialog } from "@/components/projects/repository/deploy/CutReleaseDialog";
import { I18nProvider } from "@/hooks/useI18n";

const { getReleaseCutPreview, cutRelease } = vi.hoisted(() => ({
  getReleaseCutPreview: vi.fn(),
  cutRelease: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getReleaseCutPreview, cutRelease } };
});

const preview: ReleaseCutPreview = {
  suggested_version: "1.2.0",
  previous_version: "1.1.0",
  tag: "v1.2.0",
  commit_sha: "abcdef1234567890",
  notes: "## 1.2.0\n### Features\n- T-1 Thing",
  tasks: [{ id: "task-1", key: "T-1", title: "Thing" }],
};

function renderDialog(onCut = vi.fn()) {
  render(
    <I18nProvider>
      <CutReleaseDialog
        open
        onOpenChange={() => {}}
        releaseId="rel-1"
        repositoryName="acme"
        tagPattern="v{version}"
        onCut={onCut}
      />
    </I18nProvider>,
  );
  return { onCut };
}

describe("CutReleaseDialog", () => {
  beforeEach(() => {
    getReleaseCutPreview.mockReset().mockResolvedValue(preview);
    cutRelease.mockReset();
  });

  it("loads the preview and prefills the version and notes", async () => {
    renderDialog();
    expect(await screen.findByDisplayValue("1.2.0")).toBeInTheDocument();
    expect(screen.getByDisplayValue(/### Features/)).toBeInTheDocument();
    expect(screen.getByText("abcdef123456")).toBeInTheDocument();
    expect(screen.getByText("T-1")).toBeInTheDocument();
  });

  it("updates the tag preview live as the version is edited", async () => {
    renderDialog();
    const versionInput = await screen.findByLabelText("Version");
    fireEvent.change(versionInput, { target: { value: "2.0.0" } });
    expect(await screen.findByText("v2.0.0")).toBeInTheDocument();
  });

  it("rejects a version starting with a dash without calling the API", async () => {
    renderDialog();
    const versionInput = await screen.findByLabelText("Version");
    fireEvent.change(versionInput, { target: { value: "-1.0.0" } });
    expect(await screen.findByText(/cannot start with/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cut release" })).toBeDisabled();
  });

  it("keeps the submit button disabled until the repository name is typed", async () => {
    renderDialog();
    await screen.findByDisplayValue("1.2.0");
    expect(screen.getByRole("button", { name: "Cut release" })).toBeDisabled();

    const confirmInput = screen.getByLabelText('Type acme to confirm');
    fireEvent.change(confirmInput, { target: { value: "acme" } });
    expect(screen.getByRole("button", { name: "Cut release" })).toBeEnabled();
  });

  it("submits the typed version and notes with the repository name as confirm", async () => {
    cutRelease.mockResolvedValue({ id: "rel-1", version: "1.2.0" } as Release);
    const { onCut } = renderDialog();
    await screen.findByDisplayValue("1.2.0");

    fireEvent.change(screen.getByLabelText('Type acme to confirm'), { target: { value: "acme" } });
    fireEvent.click(screen.getByRole("button", { name: "Cut release" }));

    await waitFor(() => expect(cutRelease).toHaveBeenCalledWith("rel-1", "acme", "1.2.0", preview.notes));
    await waitFor(() => expect(onCut).toHaveBeenCalled());
  });

  it("shows the server's error message verbatim on a 409", async () => {
    cutRelease.mockRejectedValue(new Error("the release tag already exists"));
    renderDialog();
    await screen.findByDisplayValue("1.2.0");
    fireEvent.change(screen.getByLabelText('Type acme to confirm'), { target: { value: "acme" } });
    fireEvent.click(screen.getByRole("button", { name: "Cut release" }));
    expect(await screen.findByText("the release tag already exists")).toBeInTheDocument();
  });
});
