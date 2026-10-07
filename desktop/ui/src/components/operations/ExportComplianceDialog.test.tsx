import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { StoreTestBuild } from "@/api";
import { ExportComplianceDialog } from "@/components/operations/ExportComplianceDialog";
import { I18nProvider } from "@/hooks/useI18n";

const { answerStoreTestBuildCompliance } = vi.hoisted(() => ({ answerStoreTestBuildCompliance: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, answerStoreTestBuildCompliance } };
});

const build: StoreTestBuild = {
  id: "b1",
  repository_id: "repo-1",
  platform: "ios",
  attempt: 1,
  sequence: 412,
  build_number: "412.54.1",
  status: "action_required",
  failure: "export_compliance",
  has_artifact: false,
  groups: [],
  trigger: "human_uat",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

describe("ExportComplianceDialog", () => {
  beforeEach(() => {
    answerStoreTestBuildCompliance.mockReset();
  });

  it("sends nothing until an answer is picked, then exactly the picked one", async () => {
    answerStoreTestBuildCompliance.mockResolvedValue({ ...build, status: "processing" });
    const onAnswered = vi.fn();
    render(
      <I18nProvider>
        <ExportComplianceDialog
          inline
          repositoryId="repo-1"
          build={build}
          open
          onOpenChange={vi.fn()}
          onAnswered={onAnswered}
        />
      </I18nProvider>,
    );

    const submit = screen.getByRole("button", { name: "Send answer" });
    expect(submit).toBeDisabled();
    for (const radio of screen.getAllByRole("radio")) expect(radio).not.toBeChecked();

    fireEvent.click(screen.getByRole("radio", { name: /No — only exempt encryption/ }));
    expect(submit).toBeEnabled();
    fireEvent.click(submit);

    await waitFor(() => expect(answerStoreTestBuildCompliance).toHaveBeenCalledWith("repo-1", "b1", false));
    expect(onAnswered).toHaveBeenCalledWith(expect.objectContaining({ status: "processing" }));
  });
});
