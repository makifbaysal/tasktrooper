import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentDelivery } from "@/api";
import { DeliveryEditDialog } from "@/components/projects/repository/deploy/DeliveryEditDialog";
import { I18nProvider } from "@/hooks/useI18n";

const { updateComponentDelivery } = vi.hoisted(() => ({
  updateComponentDelivery: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, updateComponentDelivery } };
});

const onMergeProfile: ComponentDelivery = {
  mode: "on_merge",
  executor: "github_actions",
  workflow: "deploy.yml",
  verify: { soak_minutes: 10, max_new_errors: 0, smoke: [] },
  auto_rollback: true,
};

function renderDialog(current: ComponentDelivery | null, onSaved = vi.fn()) {
  render(
    <I18nProvider>
      <DeliveryEditDialog open onOpenChange={() => {}} componentId="comp-1" current={current} hasOverride={Boolean(current)} onSaved={onSaved} />
    </I18nProvider>,
  );
  return { onSaved };
}

describe("DeliveryEditDialog", () => {
  beforeEach(() => {
    updateComponentDelivery.mockReset().mockResolvedValue({});
  });

  it("saves a valid on_merge profile as-is", async () => {
    const { onSaved } = renderDialog(onMergeProfile);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(updateComponentDelivery).toHaveBeenCalledWith("comp-1", onMergeProfile));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
  });

  it("rejects dispatch mode with no workflow", async () => {
    renderDialog({ ...onMergeProfile, mode: "dispatch", workflow: "" });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Dispatch mode needs the deploy workflow's file name")).toBeInTheDocument();
    expect(updateComponentDelivery).not.toHaveBeenCalled();
  });

  it("rejects a smoke check whose method is not GET or HEAD", async () => {
    renderDialog({
      // POST is not offered by the method select, so this models a value
      // carried over from a stored profile the select itself cannot produce.
      ...onMergeProfile,
      verify: { ...onMergeProfile.verify, smoke: [{ method: "POST", path: "/health" }] },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Only GET and HEAD may be sent to production")).toBeInTheDocument();
    expect(updateComponentDelivery).not.toHaveBeenCalled();
  });

  it("rejects a relative smoke path with no leading slash", async () => {
    renderDialog({
      ...onMergeProfile,
      verify: { ...onMergeProfile.verify, smoke: [{ method: "GET", path: "health" }] },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Path must start with / (or be an absolute URL)")).toBeInTheDocument();
    expect(updateComponentDelivery).not.toHaveBeenCalled();
  });

  it("shows the server's message when the save is rejected", async () => {
    updateComponentDelivery.mockRejectedValue(new Error("workflow is the file's base name"));
    renderDialog(onMergeProfile);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("workflow is the file's base name")).toBeInTheDocument();
  });

  it("resets to the detected profile via a confirm dialog", async () => {
    const { onSaved } = renderDialog(onMergeProfile);
    fireEvent.click(screen.getByRole("button", { name: "Reset to detected" }));

    // Two buttons now carry this label: the form's own action and the
    // confirm dialog's; the confirm dialog's is the last one rendered.
    const buttons = await screen.findAllByRole("button", { name: "Reset to detected" });
    fireEvent.click(buttons[buttons.length - 1]);

    await waitFor(() => expect(updateComponentDelivery).toHaveBeenCalledWith("comp-1", null));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
  });
});
