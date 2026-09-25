import "@testing-library/jest-dom/vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component, ComponentCheck, RepositoryModel } from "@/api";
import { ChecksTab } from "@/components/projects/repository/ChecksTab";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { updateCheck } = vi.hoisted(() => ({ updateCheck: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      updateCheck,
    },
  };
});

const component: Component = {
  id: "comp-api",
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

const requiredCheck: ComponentCheck = {
  id: "chk-lint",
  repository_id: "repo-1",
  component_id: "comp-api",
  source: "ci",
  workflow: ".github/workflows/ci.yml",
  job_key: "api-lint",
  job_name: "api-lint",
  purpose: { detected: "lint" },
  local_commands: { detected: [{ dir: "", argv: ["golangci-lint", "run"] }] },
  gate: { detected: "required" },
  dispatchable: false,
  status: "active",
  missing: false,
  needs_review: false,
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

const infoCheck: ComponentCheck = {
  ...requiredCheck,
  id: "chk-build",
  job_key: "api-image",
  job_name: "api-image",
  purpose: { detected: "build" },
  local_commands: { detected: [{ dir: "", argv: ["docker", "build", "."] }] },
  gate: { detected: "info" },
};

const model: RepositoryModel = {
  repository: {
    id: "repo-1",
    name: "acme-platform",
    description: "",
    root_path: "/repo",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
  },
  shape: "monorepo",
  components: [component],
  checks: [requiredCheck, infoCheck],
  links: [],
  incoming_links: [],
  resources: [],
  linked_components: [],
  environments: [],
  review: [],
};

function renderTab() {
  return render(
    <I18nProvider>
      <ChecksTab model={model} selectedComponentId="comp-api" onSelectComponent={vi.fn()} onReload={vi.fn()} />
    </I18nProvider>,
  );
}

describe("ChecksTab", () => {
  beforeEach(() => {
    updateCheck.mockReset().mockResolvedValue(requiredCheck);
  });

  it("lists the required check's local command in the hand-off callout", () => {
    const { container } = renderTab();
    expect(screen.getByText("Before handing off, the agent runs:")).toBeInTheDocument();

    // The callout is the page's only <code> block; the checks table below it
    // uses a plain span for the same text, including the non-required check's.
    const callout = container.querySelector("code");
    expect(callout?.textContent).toBe("golangci-lint run");
    expect(callout?.textContent).not.toMatch(/docker build/);
  });

  it("clicking a gate pill calls updateCheck with the new gate", async () => {
    renderTab();

    // Each check row renders Required/Info/Off; the first "Off" pill belongs
    // to chk-lint (rendered first, currently gated Required).
    const offPills = screen.getAllByText("Off");
    fireEvent.click(offPills[0]);

    await waitFor(() => expect(updateCheck).toHaveBeenCalledWith("chk-lint", { gate: "off" }));
  });
});
