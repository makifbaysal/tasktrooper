import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ComponentLink, RepositoryModel } from "@/api";
import { LinkTargetLabel } from "@/components/projects/model/LinkTargetLabel";
import { I18nProvider } from "@/hooks/useI18n";

function baseModel(overrides: Partial<RepositoryModel> = {}): RepositoryModel {
  return {
    repository: {
      id: "r1",
      name: "acme-platform",
      description: "",
      root_path: "/repos/acme-platform",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    shape: "monorepo",
    components: [],
    checks: [],
    links: [],
    incoming_links: [],
    resources: [],
    linked_components: [],
    environments: [],
    notes: [],
    review: [],
    ...overrides,
  };
}

function baseLink(overrides: Partial<ComponentLink> = {}): ComponentLink {
  return {
    id: "l1",
    repository_id: "r1",
    from_component_id: "c1",
    protocol: "http",
    confidence: "high",
    status: "suggested",
    source: "scan",
    auto_confirmed: false,
    missing: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderLabel(link: ComponentLink, model: RepositoryModel) {
  return render(
    <I18nProvider>
      <LinkTargetLabel link={link} model={model} />
    </I18nProvider>,
  );
}

describe("LinkTargetLabel", () => {
  it("resolves a component target to repo/path", () => {
    const model = baseModel({
      components: [
        {
          id: "api",
          repository_id: "r1",
          path: "services/api",
          name: { detected: "" },
          role: { detected: "backend" },
          stack: {},
          commands: [],
          docs: {},
          gates: {},
          status: "active",
          manually_added: false,
          needs_review: false,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
    });
    renderLabel(baseLink({ to_component_id: "api" }), model);
    expect(screen.getByText("acme-platform/services/api")).toBeInTheDocument();
  });

  it("falls back to the localized unknown-target label when nothing resolves and there is no hint", () => {
    renderLabel(baseLink(), baseModel());
    expect(screen.getByText("Unknown target")).toBeInTheDocument();
  });

  it("shows the hint for an unresolved suggestion", () => {
    renderLabel(baseLink({ hint: "BILLING_SVC_URL" }), baseModel());
    expect(screen.getByText("BILLING_SVC_URL")).toBeInTheDocument();
  });
});
