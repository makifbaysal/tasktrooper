import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentEnvStatus, EnvVarView } from "@/api";
import { EnvVarsCard } from "@/components/projects/repository/deploy/EnvVarsCard";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { listEnvRequirements, applyEnvRequirements, redeployForEnvRequirements } = vi.hoisted(() => ({
  listEnvRequirements: vi.fn(),
  applyEnvRequirements: vi.fn(),
  redeployForEnvRequirements: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listEnvRequirements, applyEnvRequirements, redeployForEnvRequirements } };
});

function v(name: string, overrides: Partial<EnvVarView> = {}): EnvVarView {
  return { name, production: false, preview: false, action: "human", ...overrides };
}

function target(vars: EnvVarView[], overrides: Partial<ComponentEnvStatus> = {}): ComponentEnvStatus {
  return {
    component_id: "comp-1",
    component_name: "web",
    environment_id: "env-1",
    provider: "vercel",
    resource_name: "site",
    capabilities: { targets: ["production", "preview"], writes_roll_out: false, overwritten_on_deploy: false },
    vars,
    ...overrides,
  };
}

function renderCard(props: Partial<Parameters<typeof EnvVarsCard>[0]> = {}) {
  return render(
    <I18nProvider>
      <EnvVarsCard repositoryId="repo-1" componentId="comp-1" {...props} />
    </I18nProvider>,
  );
}

describe("EnvVarsCard", () => {
  beforeEach(() => {
    listEnvRequirements.mockReset();
    applyEnvRequirements.mockReset();
    redeployForEnvRequirements.mockReset();
  });

  it("sends an entered secret to the server and clears it from the field", async () => {
    listEnvRequirements.mockResolvedValue({
      targets: [target([v("GITHUB_TOKEN", { kind: "human_secret", description: "Contents PAT" })])],
    });
    applyEnvRequirements.mockResolvedValue(target([v("GITHUB_TOKEN", { kind: "human_secret", production: true, preview: true, action: "none" })]));
    renderCard();

    const input = await screen.findByLabelText(/GITHUB_TOKEN/);
    expect(input).toHaveAttribute("type", "password");
    fireEvent.change(input, { target: { value: "ghp_abc" } });
    fireEvent.click(screen.getByRole("button", { name: "Save and fill the rest" }));

    await waitFor(() =>
      expect(applyEnvRequirements).toHaveBeenCalledWith("repo-1", "comp-1", [{ name: "GITHUB_TOKEN", kind: undefined, value: "ghp_abc" }]),
    );
    await waitFor(() => expect(screen.queryByDisplayValue("ghp_abc")).not.toBeInTheDocument());
  });

  it("shows what TaskTrooper fills itself and what is already set", async () => {
    listEnvRequirements.mockResolvedValue({
      targets: [
        target([
          v("SESSION_SECRET", { kind: "generated", action: "auto" }),
          v("GITHUB_REPO", { kind: "value", value: "o/r", production: true, preview: true, action: "none" }),
        ]),
      ],
    });
    renderCard();

    expect(await screen.findByText("TaskTrooper fills this")).toBeInTheDocument();
    expect(screen.getByText("GITHUB_REPO")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("re-checks with no values", async () => {
    listEnvRequirements.mockResolvedValue({ targets: [target([v("API_KEY", { kind: "human_secret" })])] });
    applyEnvRequirements.mockResolvedValue(target([v("API_KEY", { kind: "human_secret" })]));
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: "Check again" }));

    await waitFor(() => expect(applyEnvRequirements).toHaveBeenCalledWith("repo-1", "comp-1", []));
  });

  it("hides the preview column and the redeploy where the provider has neither", async () => {
    listEnvRequirements.mockResolvedValue({
      targets: [
        target([v("API_KEY", { kind: "human_secret" })], {
          provider: "aws",
          capabilities: { targets: ["production"], writes_roll_out: true, overwritten_on_deploy: true },
        }),
      ],
    });
    renderCard();

    expect(await screen.findByText(/task definition/)).toBeInTheDocument();
    expect(screen.queryByText("Preview")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Redeploy production" })).not.toBeInTheDocument();
  });

  it("in the task drawer shows nothing once nothing waits on a person", async () => {
    listEnvRequirements.mockResolvedValue({ targets: [target([v("GITHUB_REPO", { kind: "value", action: "none", production: true, preview: true })])] });
    const { container } = renderCard({ pendingOnly: true, componentId: undefined });

    await waitFor(() => expect(listEnvRequirements).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it("calls onSettled when the last missing variable is entered", async () => {
    const onSettled = vi.fn();
    listEnvRequirements.mockResolvedValue({ targets: [target([v("GITHUB_TOKEN", { kind: "human_secret" })])] });
    applyEnvRequirements.mockResolvedValue(target([v("GITHUB_TOKEN", { kind: "human_secret", production: true, preview: true, action: "none" })]));
    renderCard({ pendingOnly: true, onSettled });

    fireEvent.change(await screen.findByLabelText(/GITHUB_TOKEN/), { target: { value: "ghp_abc" } });
    fireEvent.click(screen.getByRole("button", { name: "Save and fill the rest" }));

    await waitFor(() => expect(onSettled).toHaveBeenCalled());
  });
});

describe("EnvVarsCard editing", () => {
  beforeEach(() => {
    listEnvRequirements.mockReset();
    applyEnvRequirements.mockReset();
  });

  it("changes a value the provider already has", async () => {
    listEnvRequirements.mockResolvedValue({
      targets: [target([v("GITHUB_BRANCH", { kind: "value", value: "master", production: true, preview: true, action: "none" })])],
    });
    applyEnvRequirements.mockResolvedValue(target([v("GITHUB_BRANCH", { kind: "value", value: "main", production: true, preview: true, action: "none" })]));
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: "Change" }));
    const input = screen.getByDisplayValue("master");
    fireEvent.change(input, { target: { value: "main" } });
    fireEvent.click(screen.getByRole("button", { name: "Save and fill the rest" }));

    await waitFor(() =>
      expect(applyEnvRequirements).toHaveBeenCalledWith("repo-1", "comp-1", [{ name: "GITHUB_BRANCH", kind: undefined, value: "main" }]),
    );
  });

  it("regenerates a generated secret", async () => {
    listEnvRequirements.mockResolvedValue({
      targets: [target([v("SESSION_SECRET", { kind: "generated", production: true, preview: true, action: "none" })])],
    });
    applyEnvRequirements.mockResolvedValue(target([v("SESSION_SECRET", { kind: "generated", production: true, preview: true, action: "none" })]));
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: "Change" }));
    fireEvent.click(screen.getByRole("button", { name: "Generate a new secret" }));
    fireEvent.click(screen.getByRole("button", { name: "Save and fill the rest" }));

    await waitFor(() =>
      expect(applyEnvRequirements).toHaveBeenCalledWith("repo-1", "comp-1", [{ name: "SESSION_SECRET", kind: undefined, regenerate: true }]),
    );
  });

  it("adds a variable nobody declared", async () => {
    listEnvRequirements.mockResolvedValue({ targets: [target([])] });
    applyEnvRequirements.mockResolvedValue(target([v("FEATURE_FLAG", { kind: "value", value: "on", production: true, preview: true, action: "none" })]));
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: "Add variable" }));
    fireEvent.change(screen.getByLabelText("NAME"), { target: { value: "feature-flag" } });
    fireEvent.click(screen.getByRole("combobox", { name: "Kind" }));
    fireEvent.click(await screen.findByRole("option", { name: "Plain value" }));
    fireEvent.change(screen.getByPlaceholderText("Value"), { target: { value: "on" } });
    fireEvent.click(screen.getByRole("button", { name: "Save and fill the rest" }));

    await waitFor(() =>
      expect(applyEnvRequirements).toHaveBeenCalledWith("repo-1", "comp-1", [{ name: "FEATURE_FLAG", kind: "value", value: "on" }]),
    );
  });
});
