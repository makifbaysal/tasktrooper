import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import type {
  DesktopRunnerHost,
  DesktopToolchainHost,
  ToolchainId,
  ToolchainItem,
  ToolchainJobState,
  ToolchainReport,
} from "@/lib/desktop-bridge";
import { EnvironmentSettingsPage } from "@/pages/EnvironmentSettingsPage";

const listAgents = vi.fn<() => Promise<{ agents: Agent[] }>>();

// No <Toaster/> is mounted, so a toast never reaches the DOM; assert on the calls.
const { toastSuccess, toastError, toastInfo } = vi.hoisted(() => ({
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  toastInfo: vi.fn(),
}));
vi.mock("sonner", () => ({ toast: { success: toastSuccess, error: toastError, info: toastInfo } }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listAgents: () => listAgents() } };
});

function item(id: ToolchainId, label: string, overrides: Partial<ToolchainItem> = {}): ToolchainItem {
  return { id, label, status: "missing", installKind: "auto", method: "Homebrew", dependsOn: [], ...overrides };
}

const REPORT: ToolchainReport = {
  generatedAt: Date.now(),
  platform: "darwin",
  items: [
    item("homebrew", "Homebrew", { status: "installed", version: "4.4.0" }),
    item("git", "Git", { status: "installed", version: "2.45.0" }),
    item("claude-code", "Claude Code", { method: "npm", command: "npm install -g @anthropic-ai/claude-code" }),
    item("node", "Node.js", { status: "unusable", detail: "node 14 is older than 18", command: "brew install node" }),
    item("chrome", "Google Chrome"),
    item("xcode", "Xcode", { installKind: "manual", method: "App Store" }),
    item("android-sdk", "Android SDK", { method: "Android sdkmanager" }),
    item("docker", "Docker Desktop"),
  ],
};

const IDLE: ToolchainJobState = { phase: "idle", steps: [], percent: 0 };

function stubShell(
  state: ToolchainJobState = IDLE,
  overrides: Partial<DesktopToolchainHost> = {},
  extra: { runner?: DesktopRunnerHost; mode?: "local" | "account" } = {},
) {
  const toolchain: DesktopToolchainHost = {
    report: vi.fn().mockResolvedValue(REPORT),
    plan: vi.fn(async (ids: ToolchainId[]) => ids),
    state: vi.fn().mockResolvedValue(state),
    subscribe: vi.fn(() => () => {}),
    install: vi.fn().mockResolvedValue(state),
    cancel: vi.fn().mockResolvedValue(IDLE),
    dismiss: vi.fn().mockResolvedValue(IDLE),
    openPage: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  window.__tasktrooperDesktop = { toolchain, ...extra };
  return toolchain;
}

function renderPage() {
  return render(
    <I18nProvider>
      <EnvironmentSettingsPage />
    </I18nProvider>,
  );
}

beforeEach(() => {
  window.localStorage.clear();
  toastSuccess.mockReset();
  listAgents.mockReset();
  listAgents.mockResolvedValue({
    agents: [
      { id: "fe", name: "Frontend Developer", enabled: true, catalog_slug: "frontend-developer" } as Agent,
      { id: "mob", name: "Mobile Developer", enabled: false, catalog_slug: "mobile-developer" } as Agent,
    ],
  });
});

afterEach(() => {
  delete window.__tasktrooperDesktop;
});

describe("EnvironmentSettingsPage", () => {
  it("says it needs the desktop app in a browser", () => {
    renderPage();
    expect(screen.getByText("This page needs the TaskTrooper desktop app")).toBeInTheDocument();
  });

  it("lists every tool under its group with what was detected", async () => {
    stubShell();
    renderPage();
    const inventory = await screen.findByTestId("toolchain-inventory-card");

    for (const heading of ["Basics", "Web", "Mobile", "Infra & security"]) {
      expect(await within(inventory).findByRole("heading", { name: heading })).toBeInTheDocument();
    }
    expect(within(inventory).queryByRole("heading", { name: "Games" })).toBeNull();

    const git = within(inventory).getByTestId("toolchain-item-git");
    expect(within(git).getByText("Installed · 2.45.0")).toBeInTheDocument();
    expect(within(git).queryByRole("button")).toBeNull();

    const node = within(inventory).getByTestId("toolchain-item-node");
    expect(within(node).getByText("Not usable")).toBeInTheDocument();
    expect(within(node).getByText("node 14 is older than 18")).toBeInTheDocument();
    expect(within(node).getByText("brew install node")).toBeInTheDocument();

    expect(within(within(inventory).getByTestId("toolchain-item-docker")).getByText("Missing")).toBeInTheDocument();
  });

  it("installs one tool from its row", async () => {
    const toolchain = stubShell();
    renderPage();
    const inventory = await screen.findByTestId("toolchain-inventory-card");
    fireEvent.click(await within(inventory).findByRole("button", { name: "Install Docker Desktop" }));
    await waitFor(() => expect(toolchain.install).toHaveBeenCalledWith(["docker"]));
  });

  it("opens the page of a tool installed by hand", async () => {
    const toolchain = stubShell();
    renderPage();
    const inventory = await screen.findByTestId("toolchain-inventory-card");
    const xcode = await within(inventory).findByTestId("toolchain-item-xcode");
    fireEvent.click(within(xcode).getByRole("button", { name: "Open page" }));
    expect(toolchain.openPage).toHaveBeenCalledWith("xcode");
  });

  it("checks again with a forced sweep", async () => {
    const toolchain = stubShell();
    renderPage();
    await screen.findByTestId("toolchain-item-git");
    fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    await waitFor(() => expect(toolchain.report).toHaveBeenCalledWith(true));
  });

  it("installs what the team is missing in one click", async () => {
    const toolchain = stubShell();
    renderPage();
    const card = await screen.findByTestId("toolchain-recommended-card");
    expect(await within(card).findAllByText("For Frontend Developer")).toHaveLength(2);
    expect(within(card).queryByTestId("toolchain-item-android-sdk")).toBeNull();

    fireEvent.click(within(card).getByRole("button", { name: "Install recommended" }));
    await waitFor(() => expect(toolchain.install).toHaveBeenCalledWith(["claude-code", "node", "chrome"]));
  });

  it("shows the running job and keeps its tools from being installed twice", async () => {
    const running: ToolchainJobState = {
      phase: "running",
      percent: 30,
      steps: [
        { id: "docker", label: "Docker Desktop", state: "running", installKind: "auto" },
        { id: "chrome", label: "Google Chrome", state: "queued", installKind: "auto" },
      ],
    };
    const toolchain = stubShell(running);
    renderPage();

    const job = await screen.findByTestId("toolchain-job-card");
    expect(within(job).getByText("Setting up this computer")).toBeInTheDocument();
    expect(within(job).getByRole("progressbar")).toHaveAttribute("aria-valuenow", "30");
    expect(within(job).getByText("Setting up · 30%")).toBeInTheDocument();

    const inventory = screen.getByTestId("toolchain-inventory-card");
    const docker = await within(inventory).findByTestId("toolchain-item-docker");
    expect(within(docker).getByText("Installing")).toBeInTheDocument();
    expect(within(docker).getByRole("button", { name: "Install Docker Desktop" })).toBeDisabled();

    fireEvent.click(within(job).getByRole("button", { name: "Stop" }));
    await waitFor(() => expect(toolchain.cancel).toHaveBeenCalledTimes(1));
  });

  it("keeps a failed tool installable while a job runs, and says it was queued", async () => {
    const running: ToolchainJobState = {
      phase: "running",
      percent: 50,
      steps: [
        { id: "docker", label: "Docker Desktop", state: "failed", installKind: "auto", error: "download failed" },
        { id: "chrome", label: "Google Chrome", state: "running", installKind: "auto" },
      ],
    };
    const toolchain = stubShell(running);
    renderPage();
    const inventory = await screen.findByTestId("toolchain-inventory-card");
    const install = await within(inventory).findByRole("button", { name: "Install Docker Desktop" });
    expect(install).toBeEnabled();

    fireEvent.click(install);
    await waitFor(() => expect(toolchain.install).toHaveBeenCalledWith(["docker"]));
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Queued after the current steps"));
  });

  it("will not install what waits on a tool installed by hand", async () => {
    listAgents.mockResolvedValue({
      agents: [{ id: "mob", name: "Mobile Developer", enabled: true, catalog_slug: "mobile-developer" } as Agent],
    });
    const toolchain = stubShell(IDLE, {
      report: vi.fn().mockResolvedValue({
        generatedAt: Date.now(),
        platform: "win32",
        items: [
          item("git", "Git", { status: "installed" }),
          item("claude-code", "Claude Code", { status: "installed" }),
          item("node", "Node.js", { installKind: "manual", method: undefined }),
          item("jdk", "JDK 17", { method: "winget" }),
          item("appium", "Appium", { method: "npm", dependsOn: ["node"] }),
        ],
      } satisfies ToolchainReport),
    });
    renderPage();

    const inventory = await screen.findByTestId("toolchain-inventory-card");
    const appium = await within(inventory).findByTestId("toolchain-item-appium");
    expect(within(appium).getByRole("button", { name: "Install Appium" })).toBeDisabled();
    expect(within(appium).getByText("Needs Node.js. Install it from its page first.")).toBeInTheDocument();

    const card = screen.getByTestId("toolchain-recommended-card");
    const recommendedAppium = await within(card).findByTestId("toolchain-item-appium");
    fireEvent.click(within(recommendedAppium).getByRole("button", { name: "Open Node.js page" }));
    expect(toolchain.openPage).toHaveBeenCalledWith("node");

    fireEvent.click(within(card).getByRole("button", { name: "Install recommended" }));
    await waitFor(() => expect(toolchain.install).toHaveBeenCalledWith(["jdk"]));
  });

  it("stops offering the restart once the agents were restarted for that job", async () => {
    const done: ToolchainJobState = {
      phase: "done",
      jobId: "job-7",
      percent: 100,
      restartRecommended: true,
      steps: [{ id: "node", label: "Node.js", state: "done", installKind: "auto" }],
    };
    const runner = { restartChild: vi.fn().mockResolvedValue(null), restart: vi.fn() } as unknown as DesktopRunnerHost;
    stubShell(done, {}, { runner, mode: "local" });
    const { unmount } = renderPage();

    const job = await screen.findByTestId("toolchain-job-card");
    expect(within(job).getByText("Setup complete")).toBeInTheDocument();
    expect(within(job).getByText(/reaches the agents only after they restart/)).toBeInTheDocument();
    fireEvent.click(within(job).getByRole("button", { name: "Restart agents" }));
    fireEvent.click(await screen.findByRole("button", { name: "Restart" }));
    await waitFor(() => expect(runner.restartChild).toHaveBeenCalledWith("agent-server"));
    await waitFor(() => expect(within(job).queryByRole("button", { name: "Restart agents" })).toBeNull());
    expect(within(job).queryByText(/reaches the agents only after they restart/)).toBeNull();

    // A local-mode restart reloads the window; the memory has to outlive it.
    unmount();
    renderPage();
    const again = await screen.findByTestId("toolchain-job-card");
    expect(within(again).queryByRole("button", { name: "Restart agents" })).toBeNull();
  });
});
