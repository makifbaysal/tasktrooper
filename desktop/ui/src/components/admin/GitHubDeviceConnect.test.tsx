import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GitHubDeviceFlow } from "@/api";
import { GitHubCard } from "@/components/admin/GitHubCard";
import { GitHubDeviceConnect } from "@/components/admin/GitHubDeviceConnect";
import { I18nProvider } from "@/hooks/useI18n";

const { startGitHubDeviceFlow, pollGitHubDeviceFlow, githubStatus } = vi.hoisted(() => ({
  startGitHubDeviceFlow: vi.fn(),
  pollGitHubDeviceFlow: vi.fn(),
  githubStatus: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, startGitHubDeviceFlow, pollGitHubDeviceFlow, githubStatus } };
});

function flow(overrides: Partial<GitHubDeviceFlow> = {}): GitHubDeviceFlow {
  return {
    id: "flow-1",
    user_code: "WDJB-MJHT",
    verification_uri: "https://github.com/login/device",
    expires_at: "2026-10-06T00:15:00Z",
    interval_seconds: 5,
    state: "pending",
    ...overrides,
  };
}

describe("GitHubDeviceConnect", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    startGitHubDeviceFlow.mockReset();
    pollGitHubDeviceFlow.mockReset();
  });
  afterEach(() => vi.useRealTimers());

  it("shows the code, polls at GitHub's interval and reports the connection", async () => {
    const onConnected = vi.fn();
    startGitHubDeviceFlow.mockResolvedValue(flow());
    pollGitHubDeviceFlow
      .mockResolvedValueOnce(flow())
      .mockResolvedValueOnce(flow({ state: "authorized", login: "akif" }));
    render(
      <I18nProvider>
        <GitHubDeviceConnect onConnected={onConnected} />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Connect with GitHub" }));
    expect(await screen.findByTestId("github-user-code")).toHaveTextContent("WDJB-MJHT");
    expect(screen.getByRole("link", { name: "Open GitHub" })).toHaveAttribute("href", "https://github.com/login/device");
    expect(pollGitHubDeviceFlow).not.toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(pollGitHubDeviceFlow).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(pollGitHubDeviceFlow).toHaveBeenCalledTimes(2);
    expect(onConnected).toHaveBeenCalledTimes(1);
  });

  it("offers a retry once the code expired", async () => {
    startGitHubDeviceFlow.mockResolvedValue(flow());
    pollGitHubDeviceFlow.mockResolvedValue(flow({ state: "expired" }));
    render(
      <I18nProvider>
        <GitHubDeviceConnect onConnected={vi.fn()} />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Connect with GitHub" }));
    await screen.findByTestId("github-user-code");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });

    expect(await screen.findByText("The code expired before it was approved.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });
});

describe("GitHubCard with a GitHub App", () => {
  beforeEach(() => githubStatus.mockReset());

  it("leads with Connect with GitHub and keeps the token as a fallback", async () => {
    githubStatus.mockResolvedValue({ connected: false, app_available: true });
    render(
      <I18nProvider>
        <GitHubCard />
      </I18nProvider>,
    );

    expect(await screen.findByRole("button", { name: "Connect with GitHub" })).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("ghp_… or github_pat_…")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Use a personal access token instead" }));
    expect(screen.getByPlaceholderText("ghp_… or github_pat_…")).toBeInTheDocument();
  });

  it("asks to install the app when it reaches no repository", async () => {
    githubStatus.mockResolvedValue({
      connected: true,
      login: "akif",
      mode: "app",
      app_available: true,
      needs_install: true,
      install_url: "https://github.com/apps/tasktrooper/installations/new",
    });
    render(
      <I18nProvider>
        <GitHubCard />
      </I18nProvider>,
    );

    expect(await screen.findByText("Install the app on your repositories")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Install on GitHub" })).toHaveAttribute(
      "href",
      "https://github.com/apps/tasktrooper/installations/new",
    );
    expect(screen.getByText(/via GitHub sign-in/)).toBeInTheDocument();
  });

  it("says when the app connection lapsed", async () => {
    githubStatus.mockResolvedValue({ connected: false, mode: "app", app_available: true, expired: true });
    render(
      <I18nProvider>
        <GitHubCard />
      </I18nProvider>,
    );

    expect(await screen.findByText("The GitHub connection expired")).toBeInTheDocument();
  });
});
