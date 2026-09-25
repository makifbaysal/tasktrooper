import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component, ComponentDelivery } from "@/api";
import { DeliveryCard } from "@/components/projects/repository/deploy/DeliveryCard";
import { I18nProvider } from "@/hooks/useI18n";

const { updateComponentDelivery } = vi.hoisted(() => ({
  updateComponentDelivery: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, updateComponentDelivery } };
});

const baseComponent: Component = {
  id: "comp-1",
  repository_id: "repo-1",
  path: "services/api",
  name: { detected: "api" },
  role: { detected: "backend" },
  stack: { detected: {} },
  commands: [],
  docs: {},
  gates: {},
  status: "active",
  manually_added: false,
  needs_review: false,
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

const profile: ComponentDelivery = {
  mode: "on_merge",
  executor: "github_actions",
  workflow: "deploy.yml",
  verify: { soak_minutes: 10, max_new_errors: 0, smoke: [] },
  auto_rollback: true,
};

function renderCard(component: Component, onChanged = vi.fn()) {
  render(
    <I18nProvider>
      <DeliveryCard component={component} onChanged={onChanged} />
    </I18nProvider>,
  );
  return { onChanged };
}

describe("DeliveryCard", () => {
  beforeEach(() => {
    updateComponentDelivery.mockReset().mockResolvedValue({ ...baseComponent, delivery: { override: profile } });
  });

  it("shows 'set by you' and no unconfirmed warning when an override is set", () => {
    renderCard({ ...baseComponent, delivery: { override: profile, confidence: "exact" } });
    expect(screen.getByText("Set by you")).toBeInTheDocument();
    expect(screen.queryByText("Delivery not confirmed")).not.toBeInTheDocument();
    expect(screen.getByText("On merge")).toBeInTheDocument();
  });

  it("shows a confidence badge and no warning for a high-confidence detection", () => {
    renderCard({ ...baseComponent, delivery: { detected: profile, confidence: "high" } });
    expect(screen.getByText("Detected (high)")).toBeInTheDocument();
    expect(screen.queryByText("Delivery not confirmed")).not.toBeInTheDocument();
  });

  it("warns and offers Confirm for a medium-confidence detection", async () => {
    const { onChanged } = renderCard({ ...baseComponent, delivery: { detected: profile, confidence: "medium" } });
    expect(screen.getByText("Delivery not confirmed")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => expect(updateComponentDelivery).toHaveBeenCalledWith("comp-1", profile));
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  it("warns with no Confirm action when nothing was ever detected", () => {
    renderCard({ ...baseComponent, delivery: {} });
    expect(screen.getByText("Delivery not confirmed")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Confirm" })).not.toBeInTheDocument();
    expect(screen.getByText(/Nothing is configured yet/)).toBeInTheDocument();
  });
});
