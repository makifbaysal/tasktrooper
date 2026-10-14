import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, StoreCredentialView } from "@/api";
import { IntegrationsStep } from "@/components/setup/IntegrationsStep";
import { I18nProvider } from "@/hooks/useI18n";
import { FIRST_RUN_INTEGRATIONS_DONE_KEY } from "@/lib/firstRun";

const listAgents = vi.fn<() => Promise<{ agents: Agent[] }>>();
const listStoreCredentials = vi.fn<() => Promise<StoreCredentialView[]>>();

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, listAgents: () => listAgents(), listStoreCredentials: () => listStoreCredentials() },
  };
});

// The real cards are tested on their own; here only which ones render, and
// what the store cards are handed, matters.
vi.mock("@/components/admin/AppStoreConnectCard", () => ({
  AppStoreConnectCard: ({ credential }: { credential?: StoreCredentialView }) => (
    <p>App Store Connect card ({credential?.configured ? "configured" : "empty"})</p>
  ),
}));
vi.mock("@/components/admin/GooglePlayCard", () => ({
  GooglePlayCard: ({ credential }: { credential?: StoreCredentialView }) => (
    <p>Google Play card ({credential?.configured ? "configured" : "empty"})</p>
  ),
}));
vi.mock("@/components/admin/CloudAccountsCard", () => ({ CloudAccountsCard: () => <p>Cloud accounts card</p> }));
vi.mock("@/components/admin/JiraCard", () => ({ JiraCard: () => <p>Jira card</p> }));
vi.mock("@/components/admin/IssueSyncCard", () => ({ IssueSyncCard: () => <p>Issue sync card</p> }));

function agent(slug: string, enabled: boolean): Agent {
  return { id: slug, name: slug, enabled, catalog_slug: slug } as Agent;
}

function withTeam(...enabled: string[]) {
  listAgents.mockResolvedValue({
    agents: ["product-manager", "release-engineer", "mobile-developer", "frontend-developer"].map((slug) =>
      agent(slug, slug === "product-manager" || slug === "release-engineer" || enabled.includes(slug)),
    ),
  });
}

function renderStep() {
  const onDone = vi.fn();
  render(
    <I18nProvider>
      <IntegrationsStep onDone={onDone} />
    </I18nProvider>,
  );
  return onDone;
}

const tile = (name: string) => screen.findByRole("checkbox", { name });
const flagWritten = () => window.localStorage.getItem(FIRST_RUN_INTEGRATIONS_DONE_KEY) === "1";

beforeEach(() => {
  window.localStorage.clear();
  listAgents.mockReset();
  listStoreCredentials.mockReset();
  listStoreCredentials.mockResolvedValue([{ provider: "asc", configured: true } as StoreCredentialView]);
});

describe("IntegrationsStep", () => {
  it("preselects the stores and cloud accounts for a mobile team, leaving issue tracking off", async () => {
    withTeam("mobile-developer");
    renderStep();

    expect(await tile("App Store Connect")).toBeChecked();
    expect(await tile("Google Play Console")).toBeChecked();
    expect(await tile("Cloud accounts")).toBeChecked();
    expect(await tile("Jira")).not.toBeChecked();
    expect(await tile("Issue import")).not.toBeChecked();
    expect(screen.getAllByText("Recommended for your team")).toHaveLength(3);
    expect(screen.queryByRole("checkbox", { name: /github/i })).toBeNull();
  });

  it("recommends only cloud accounts to a team without a mobile developer", async () => {
    withTeam("frontend-developer");
    renderStep();

    expect(await tile("Cloud accounts")).toBeChecked();
    expect(await tile("App Store Connect")).not.toBeChecked();
    expect(screen.getAllByText("Recommended for your team")).toHaveLength(1);
  });

  it("disables Set up selected once nothing is picked", async () => {
    withTeam("frontend-developer");
    renderStep();

    fireEvent.click(await tile("Cloud accounts"));
    expect(screen.getByRole("button", { name: "Set up selected" })).toBeDisabled();
  });

  it("skips from the choice and writes the flag", async () => {
    withTeam("mobile-developer");
    const onDone = renderStep();

    await tile("Jira");
    fireEvent.click(screen.getByRole("button", { name: "Skip for now" }));
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(flagWritten()).toBe(true);
    expect(listStoreCredentials).not.toHaveBeenCalled();
  });

  it("renders only the picked integrations' cards, then continues and writes the flag", async () => {
    withTeam("mobile-developer");
    const onDone = renderStep();

    fireEvent.click(await tile("Google Play Console"));
    fireEvent.click(await tile("Jira"));
    fireEvent.click(screen.getByRole("button", { name: "Set up selected" }));

    expect(await screen.findByText("App Store Connect card (configured)")).toBeInTheDocument();
    expect(screen.getByText("Cloud accounts card")).toBeInTheDocument();
    expect(screen.getByText("Jira card")).toBeInTheDocument();
    expect(screen.queryByText(/Google Play card/)).toBeNull();
    expect(screen.queryByText("Issue sync card")).toBeNull();
    expect(screen.getByText(/also in Settings → Integrations later/)).toBeInTheDocument();
    expect(onDone).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(flagWritten()).toBe(true);
  });

  it("goes back to the choice with the selection kept", async () => {
    withTeam("mobile-developer");
    renderStep();

    fireEvent.click(await tile("Google Play Console"));
    fireEvent.click(await tile("Issue import"));
    fireEvent.click(screen.getByRole("button", { name: "Set up selected" }));
    await screen.findByText("Issue sync card");

    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(await tile("App Store Connect")).toBeChecked();
    expect(await tile("Google Play Console")).not.toBeChecked();
    expect(await tile("Issue import")).toBeChecked();
    expect(screen.getByRole("heading", { name: "Which integrations do you want to set up?" })).toHaveFocus();
  });

  it("skips from the set-up stage and writes the flag", async () => {
    withTeam("frontend-developer");
    const onDone = renderStep();

    await tile("Cloud accounts");
    fireEvent.click(screen.getByRole("button", { name: "Set up selected" }));
    await screen.findByText("Cloud accounts card");
    expect(screen.getByRole("heading", { name: "Set up what you picked" })).toHaveFocus();

    fireEvent.click(screen.getByRole("button", { name: "Skip for now" }));
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(flagWritten()).toBe(true);
  });

  it("still offers the tiles and Skip when the team cannot be read", async () => {
    listAgents.mockRejectedValue(new Error("connection refused"));
    const onDone = renderStep();

    expect(await screen.findByText("connection refused")).toBeInTheDocument();
    expect(await tile("Cloud accounts")).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "Skip for now" }));
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it("shows the store vault's failure with a retry, and still the cards", async () => {
    withTeam("mobile-developer");
    listStoreCredentials.mockRejectedValueOnce(new Error("vault locked"));
    renderStep();

    await tile("App Store Connect");
    fireEvent.click(screen.getByRole("button", { name: "Set up selected" }));
    expect(await screen.findByText("vault locked")).toBeInTheDocument();
    expect(screen.getByText("App Store Connect card (empty)")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(await screen.findByText("App Store Connect card (configured)")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText("vault locked")).toBeNull());
  });
});
