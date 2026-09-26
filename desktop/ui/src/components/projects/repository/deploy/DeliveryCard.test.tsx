import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component, ComponentDelivery, ComponentEnvironment } from "@/api";
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

function renderCard(
  component: Component,
  options: { onChanged?: (c: Component) => void; production?: ComponentEnvironment | null; onBindProduction?: (provider?: "vercel" | "gcp" | "aws") => void; coupled?: boolean } = {},
) {
  const onChanged = options.onChanged ?? vi.fn();
  const onBindProduction = options.onBindProduction ?? vi.fn();
  render(
    <I18nProvider>
      <DeliveryCard
        component={component}
        onChanged={onChanged}
        production={options.production ?? null}
        onBindProduction={onBindProduction}
        coupled={options.coupled ?? false}
      />
    </I18nProvider>,
  );
  return { onChanged, onBindProduction };
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

  it("shows the tag-push workflow hint for a batch component on github_actions", () => {
    const batchProfile: ComponentDelivery = { mode: "batch", executor: "github_actions", verify: {}, auto_rollback: true };
    renderCard({ ...baseComponent, delivery: { override: batchProfile, confidence: "exact" } });
    expect(screen.getByText(/release workflow must run on the tag push/)).toBeInTheDocument();
  });

  it("shows no workflow hint for a batch component on a non-github_actions executor", () => {
    const batchProfile: ComponentDelivery = { mode: "batch", executor: "local", local_command: "./release.sh {version}", verify: {}, auto_rollback: true };
    renderCard({ ...baseComponent, delivery: { override: batchProfile, confidence: "exact" } });
    expect(screen.queryByText(/release workflow must run on the tag push/)).not.toBeInTheDocument();
  });

  it("warns and offers to bind PROD to Vercel when the executor is vercel with no confirmed production", () => {
    const vercelProfile: ComponentDelivery = { mode: "on_merge", executor: "vercel", verify: {}, auto_rollback: true };
    const { onBindProduction } = renderCard(
      { ...baseComponent, delivery: { override: vercelProfile, confidence: "exact" } },
      { coupled: true, production: null },
    );
    expect(screen.getByText("PROD environment isn't bound to Vercel")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Bind PROD environment" }));
    expect(onBindProduction).toHaveBeenCalledWith("vercel");
  });

  it("shows the target line with the production resource name when bound", () => {
    const vercelProfile: ComponentDelivery = { mode: "on_merge", executor: "vercel", verify: {}, auto_rollback: true };
    renderCard(
      { ...baseComponent, delivery: { override: vercelProfile, confidence: "exact" } },
      {
        coupled: true,
        production: makeEnv({ provider: "vercel", resource: { kind: "vercel_project", id: "prj_1", name: "pishio-web" } }),
      },
    );
    expect(screen.getByText("Target:")).toBeInTheDocument();
    expect(screen.getByText("pishio-web")).toBeInTheDocument();
    expect(screen.getByText("Vercel · pishio-web")).toBeInTheDocument();
  });

  it("offers 'Deliver with Vercel' when PROD is on Vercel but delivery mode is none, and saves on_merge/vercel", async () => {
    const noneProfile: ComponentDelivery = { mode: "none", verify: {}, auto_rollback: true };
    const { onChanged } = renderCard(
      { ...baseComponent, delivery: { override: noneProfile, confidence: "exact" } },
      { coupled: true, production: makeEnv({ provider: "vercel", resource: { kind: "vercel_project", id: "prj_1", name: "pishio-web" } }) },
    );
    expect(screen.getByText("PROD is on Vercel but delivery is off")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Deliver with Vercel" }));

    await waitFor(() =>
      expect(updateComponentDelivery).toHaveBeenCalledWith("comp-1", {
        ...noneProfile,
        mode: "on_merge",
        executor: "vercel",
        workflow: undefined,
        tag_pattern: undefined,
        local_command: undefined,
      }),
    );
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  it("does not show the coupling warnings when not coupled (mobile)", () => {
    const vercelProfile: ComponentDelivery = { mode: "on_merge", executor: "vercel", verify: {}, auto_rollback: true };
    renderCard({ ...baseComponent, delivery: { override: vercelProfile, confidence: "exact" } }, { coupled: false });
    expect(screen.queryByText("PROD environment isn't bound to Vercel")).not.toBeInTheDocument();
    expect(screen.queryByText("Target:")).not.toBeInTheDocument();
  });

  it("lists up to 3 smoke checks compactly, with a '+N more' line beyond that", () => {
    const withSmoke: ComponentDelivery = {
      ...profile,
      verify: {
        ...profile.verify,
        smoke: [
          { name: "Health", method: "GET", path: "/health", expect_status: 200 },
          { method: "GET", path: "/api/health", expect_status: 200 },
          { method: "HEAD", path: "/" },
          { method: "GET", path: "/extra" },
        ],
      },
    };
    renderCard({ ...baseComponent, delivery: { override: withSmoke, confidence: "exact" } });
    expect(screen.getByText("Health → 200")).toBeInTheDocument();
    expect(screen.getByText("GET /api/health → 200")).toBeInTheDocument();
    expect(screen.getByText("HEAD /")).toBeInTheDocument();
    expect(screen.queryByText(/extra/)).not.toBeInTheDocument();
    expect(screen.getByText("+1 more")).toBeInTheDocument();
  });
});
