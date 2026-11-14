import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentDelivery, ComponentEnvironment } from "@/api";
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

function makeEnv(overrides: Partial<ComponentEnvironment>): ComponentEnvironment {
  return {
    id: "env-1",
    repository_id: "repo-1",
    component_id: "comp-1",
    environment: "production",
    status: "confirmed",
    source: "user",
    confidence: "exact",
    auto_confirmed: false,
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderDialog(
  current: ComponentDelivery | null,
  options: { onSaved?: (c: unknown) => void; production?: ComponentEnvironment | null } = {},
) {
  const onSaved = options.onSaved ?? vi.fn();
  render(
    <I18nProvider>
      <DeliveryEditDialog
        open
        onOpenChange={() => {}}
        componentId="comp-1"
        current={current}
        hasOverride={Boolean(current)}
        production={options.production ?? null}
        onSaved={onSaved}
      />
    </I18nProvider>,
  );
  return { onSaved };
}

function advance(times: number) {
  for (let i = 0; i < times; i++) fireEvent.click(screen.getByRole("button", { name: "Next" }));
}

/** Walks an already-answered profile to the last step and saves it. */
function saveFromStart() {
  advance(3);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
}

describe("DeliveryEditDialog", () => {
  beforeEach(() => {
    updateComponentDelivery.mockReset().mockResolvedValue({});
  });

  it("saves a valid on_merge profile as-is", async () => {
    const { onSaved } = renderDialog(onMergeProfile);
    saveFromStart();
    await waitFor(() => expect(updateComponentDelivery).toHaveBeenCalledWith("comp-1", onMergeProfile));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
  });

  it("sends the impact thresholds only when they are set, within the server's limits", async () => {
    renderDialog(onMergeProfile);
    advance(2);
    fireEvent.change(screen.getByLabelText("Max error rate rise (percentage points)"), { target: { value: "150" } });
    advance(1);
    expect(await screen.findByText("Max error rate rise must be between 0 and 100 percentage points")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Max error rate rise (percentage points)"), { target: { value: "2.5" } });
    fireEvent.change(screen.getByLabelText("Max p95 latency rise (%)"), { target: { value: "30" } });
    advance(1);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(updateComponentDelivery).toHaveBeenCalledWith("comp-1", {
        ...onMergeProfile,
        verify: { ...onMergeProfile.verify, max_error_rate_delta: 2.5, max_p95_delta_pct: 30 },
      }),
    );
  });

  it("rejects dispatch mode with no workflow", async () => {
    renderDialog({ ...onMergeProfile, mode: "dispatch", workflow: "" });
    advance(2);
    expect(await screen.findByText("Dispatch mode needs the deploy workflow's file name")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /TaskTrooper starts a GitHub Actions workflow/ })).toBeChecked();
  });

  it("rejects a smoke check whose method is not GET or HEAD", async () => {
    renderDialog({
      // POST is not offered by the method select, so this models a value
      // carried over from a stored profile the select itself cannot produce.
      ...onMergeProfile,
      verify: { ...onMergeProfile.verify, smoke: [{ method: "POST", path: "/health" }] },
    });
    advance(3);
    expect(await screen.findByText("Only GET and HEAD may be sent to production")).toBeInTheDocument();
    expect(updateComponentDelivery).not.toHaveBeenCalled();
  });

  it("rejects a relative smoke path with no leading slash", async () => {
    renderDialog({
      ...onMergeProfile,
      verify: { ...onMergeProfile.verify, smoke: [{ method: "GET", path: "health" }] },
    });
    advance(3);
    // Shown twice now: the dialog's aggregate error list, and inline under
    // the smoke check's own path field.
    expect((await screen.findAllByText("Path must start with / (or be an absolute URL)")).length).toBeGreaterThan(0);
    expect(updateComponentDelivery).not.toHaveBeenCalled();
  });

  it("shows the server's message when the save is rejected", async () => {
    updateComponentDelivery.mockRejectedValue(new Error("workflow is the file's base name"));
    renderDialog(onMergeProfile);
    saveFromStart();
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

  it("offers Vercel first for a fresh profile when production is on Vercel", () => {
    renderDialog(null, { production: makeEnv({ provider: "vercel", resource: { kind: "vercel_project", id: "prj_1", name: "pishio-web" } }) });
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    fireEvent.click(screen.getByRole("radio", { name: /Every time a task is merged/ }));
    advance(1);
    expect(screen.getByRole("radio", { name: /Vercel deploys it/ })).toBeChecked();
    expect(screen.getByText("Vercel project: pishio-web (from the PROD environment)")).toBeInTheDocument();
  });

  it("warns, without blocking save, when executor is vercel and production isn't a Vercel project", async () => {
    const { onSaved } = renderDialog({ ...onMergeProfile, executor: "vercel", workflow: undefined });
    advance(1);
    expect(screen.getByText("Bind PROD to a Vercel project, or the deploy can't be tracked.")).toBeInTheDocument();

    advance(2);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(updateComponentDelivery).toHaveBeenCalled());
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
  });

  it("saves 'never' as mode none in two steps, with no deploy fields left over", async () => {
    renderDialog(onMergeProfile);
    fireEvent.click(screen.getByRole("radio", { name: /Never/ }));
    advance(1);
    expect(screen.getByText("Merging does not deploy anything. A merged task goes straight to Released.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(updateComponentDelivery).toHaveBeenCalledWith("comp-1", {
        mode: "none",
        executor: undefined,
        workflow: undefined,
        tag_pattern: undefined,
        local_command: undefined,
        verify: onMergeProfile.verify,
        auto_rollback: true,
      }),
    );
  });

  it("saves 'only when I ask' as a batch release cut from a version tag", async () => {
    renderDialog(null);
    fireEvent.click(screen.getByRole("radio", { name: /Only when I ask/ }));
    advance(1);
    expect(screen.getByRole("radio", { name: /A version tag starts a GitHub Actions workflow/ })).toBeChecked();
    fireEvent.change(screen.getByLabelText("Tag pattern"), { target: { value: "v{version}" } });
    advance(2);
    expect(screen.getByText(/Press "Cut release" there, or ask the release engineer/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(updateComponentDelivery).toHaveBeenCalledWith(
        "comp-1",
        expect.objectContaining({ mode: "batch", executor: "github_actions", tag_pattern: "v{version}" }),
      ),
    );
  });

  it("warns that on-push delivery fails when no workflow deploys on push", () => {
    renderDialog(onMergeProfile);
    advance(1);
    expect(screen.getByText("Only pick this if a workflow really deploys on push")).toBeInTheDocument();
  });
});
