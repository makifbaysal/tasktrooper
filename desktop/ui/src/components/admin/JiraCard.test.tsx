import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { JiraCard } from "@/components/admin/JiraCard";
import { I18nProvider } from "@/hooks/useI18n";

const { getJiraStatus, setJira, disconnectJira } = vi.hoisted(() => ({
  getJiraStatus: vi.fn(),
  setJira: vi.fn(),
  disconnectJira: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getJiraStatus, setJira, disconnectJira } };
});

function renderCard() {
  return render(
    <I18nProvider>
      <JiraCard />
    </I18nProvider>,
  );
}

function fillForm() {
  fireEvent.change(screen.getByLabelText("Site URL"), {
    target: { value: "https://acme.atlassian.net" },
  });
  fireEvent.change(screen.getByLabelText("Email"), { target: { value: "dev@acme.com" } });
  fireEvent.change(screen.getByLabelText("API token"), { target: { value: "secret-token" } });
}

describe("JiraCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getJiraStatus.mockResolvedValue({ connected: false, site_url: "", email: "" });
  });

  it("sends the site, the account and the token it was given", async () => {
    setJira.mockResolvedValue({
      connected: true,
      site_url: "https://acme.atlassian.net",
      email: "dev@acme.com",
      display_name: "Dev Person",
    });
    renderCard();

    await screen.findByRole("button", { name: "Connect" });
    fillForm();
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() =>
      expect(setJira).toHaveBeenCalledWith({
        site_url: "https://acme.atlassian.net",
        email: "dev@acme.com",
        api_token: "secret-token",
      }),
    );
  });

  it("shows the site and the account, and a Disconnect button, once connected", async () => {
    getJiraStatus.mockResolvedValue({
      connected: true,
      site_url: "https://acme.atlassian.net",
      email: "dev@acme.com",
    });
    disconnectJira.mockResolvedValue(undefined);
    renderCard();

    expect(await screen.findByText("https://acme.atlassian.net")).toBeInTheDocument();
    expect(screen.getByText("Connected as dev@acme.com")).toBeInTheDocument();
    expect(screen.queryByLabelText("API token")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    await waitFor(() => expect(disconnectJira).toHaveBeenCalled());
  });

  it("keeps Connect disabled until the form is filled", async () => {
    renderCard();

    const connect = await screen.findByRole("button", { name: "Connect" });
    expect(connect).toBeDisabled();
    fillForm();
    expect(connect).toBeEnabled();
  });
});
