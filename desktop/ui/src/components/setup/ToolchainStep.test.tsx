import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { ToolchainStep } from "@/components/setup/ToolchainStep";
import { I18nProvider } from "@/hooks/useI18n";
import type {
  DesktopToolchainHost,
  ToolchainId,
  ToolchainItem,
  ToolchainJobState,
  ToolchainReport,
} from "@/lib/desktop-bridge";
import { FIRST_RUN_TOOLCHAIN_DONE_KEY } from "@/lib/firstRun";

const listAgents = vi.fn<() => Promise<{ agents: Agent[] }>>();

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listAgents: () => listAgents() } };
});

function agent(slug: string, name: string, enabled: boolean): Agent {
  return { id: slug, name, enabled, catalog_slug: slug } as Agent;
}

function item(id: ToolchainId, label: string, overrides: Partial<ToolchainItem> = {}): ToolchainItem {
  return { id, label, status: "missing", installKind: "auto", method: "Homebrew", dependsOn: [], ...overrides };
}

const WEB_REPORT: ToolchainReport = {
  generatedAt: 1,
  platform: "darwin",
  items: [
    item("homebrew", "Homebrew", { installKind: "interactive", method: "Terminal" }),
    item("git", "Git", { status: "installed", version: "2.45.0" }),
    item("claude-code", "Claude Code", { method: "npm" }),
    item("node", "Node.js", { dependsOn: ["homebrew"] }),
    item("chrome", "Google Chrome"),
    item("go", "Go"),
    item("docker", "Docker Desktop"),
    item("xcode", "Xcode", { installKind: "manual", method: "App Store", url: "https://apps.apple.com/app/xcode" }),
  ],
};

const RUNNING: ToolchainJobState = { phase: "running", percent: 0, steps: [] };

function stubShell(report: ToolchainReport, overrides: Partial<DesktopToolchainHost> = {}) {
  const toolchain: DesktopToolchainHost = {
    report: vi.fn().mockResolvedValue(report),
    plan: vi.fn(async (ids: ToolchainId[]) => ids),
    state: vi.fn().mockResolvedValue({ phase: "idle", steps: [], percent: 0 }),
    subscribe: vi.fn(() => () => {}),
    install: vi.fn().mockResolvedValue(RUNNING),
    cancel: vi.fn(),
    dismiss: vi.fn(),
    openPage: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  window.__tasktrooperDesktop = { toolchain };
  return toolchain;
}

function renderStep() {
  const onDone = vi.fn();
  render(
    <I18nProvider>
      <ToolchainStep onDone={onDone} />
    </I18nProvider>,
  );
  return onDone;
}

const flagWritten = () => window.localStorage.getItem(FIRST_RUN_TOOLCHAIN_DONE_KEY) === "1";

beforeEach(() => {
  window.localStorage.clear();
  listAgents.mockReset();
  listAgents.mockResolvedValue({
    agents: [
      agent("product-manager", "Product Manager", true),
      agent("frontend-developer", "Frontend Developer", true),
      agent("backend-developer", "Backend Developer", true),
      agent("qa-agent", "QA Agent", true),
      agent("mobile-developer", "Mobile Developer", false),
    ],
  });
});

afterEach(() => {
  delete window.__tasktrooperDesktop;
});

describe("ToolchainStep", () => {
  it("checks the recommended tools, leaves optional ones off and lists what is already here", async () => {
    stubShell(WEB_REPORT);
    renderStep();

    expect(await screen.findByRole("checkbox", { name: "Claude Code" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Node.js" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Google Chrome" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Go" })).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Docker Desktop" })).not.toBeChecked();
    expect(screen.queryByRole("checkbox", { name: "Homebrew" })).toBeNull();
    expect(screen.queryByText("Xcode")).toBeNull();

    expect(screen.getByText("For Frontend Developer, QA Agent")).toBeInTheDocument();
    expect(screen.getAllByText("Every agent uses it").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "Already on this computer (1)" })).toBeInTheDocument();
  });

  it("previews the dependencies the install adds in front", async () => {
    const toolchain = stubShell(WEB_REPORT, {
      plan: vi.fn(async (ids: ToolchainId[]) => ["homebrew", ...ids] as ToolchainId[]),
    });
    renderStep();
    expect(await screen.findByText("Installed first: Homebrew")).toBeInTheDocument();
    expect(toolchain.plan).toHaveBeenCalledWith(["claude-code", "node", "chrome"]);
  });

  it("installs the selection without waiting, writes the flag and moves on", async () => {
    const toolchain = stubShell(WEB_REPORT);
    const onDone = renderStep();

    fireEvent.click(await screen.findByRole("checkbox", { name: "Google Chrome" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Go" }));
    fireEvent.click(screen.getByRole("button", { name: "Install selected and continue" }));

    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
    expect(toolchain.install).toHaveBeenCalledWith(["claude-code", "node", "go"]);
    expect(flagWritten()).toBe(true);
  });

  it("skips without installing anything", async () => {
    const toolchain = stubShell(WEB_REPORT);
    const onDone = renderStep();

    fireEvent.click(await screen.findByRole("button", { name: "Skip for now" }));
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(toolchain.install).not.toHaveBeenCalled();
    expect(flagWritten()).toBe(true);
  });

  it("stays on the screen when the person declines the shell's confirmation", async () => {
    stubShell(WEB_REPORT, {
      install: vi.fn().mockRejectedValue(new Error("Error invoking remote method 'cloud:toolchain:install': Error: declined")),
    });
    const onDone = renderStep();

    fireEvent.click(await screen.findByRole("button", { name: "Install selected and continue" }));
    expect(await screen.findByText("Nothing was installed")).toBeInTheDocument();
    expect(onDone).not.toHaveBeenCalled();
    expect(flagWritten()).toBe(false);
    expect(screen.getByRole("button", { name: "Install selected and continue" })).toBeEnabled();
  });

  it("offers Open page instead of a checkbox for a manual install", async () => {
    listAgents.mockResolvedValue({ agents: [agent("mobile-developer", "Mobile Developer", true)] });
    const toolchain = stubShell(WEB_REPORT);
    renderStep();

    expect(await screen.findByText("Xcode")).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Xcode" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Open page" }));
    expect(toolchain.openPage).toHaveBeenCalledWith("xcode");
  });

  it("does not offer what waits on a tool installed by hand, and points at that tool's page", async () => {
    listAgents.mockResolvedValue({ agents: [agent("mobile-developer", "Mobile Developer", true)] });
    const toolchain = stubShell({
      generatedAt: 1,
      platform: "win32",
      items: [
        item("git", "Git", { status: "installed" }),
        item("claude-code", "Claude Code", { method: "npm", dependsOn: ["node"] }),
        item("node", "Node.js", { installKind: "manual", method: undefined }),
        item("jdk", "JDK 17", { method: "winget" }),
        item("appium", "Appium", { method: "npm", dependsOn: ["node"] }),
      ],
    });
    const onDone = renderStep();

    expect(await screen.findByRole("checkbox", { name: "JDK 17" })).toBeChecked();
    expect(screen.queryByRole("checkbox", { name: "Appium" })).toBeNull();
    expect(screen.queryByRole("checkbox", { name: "Claude Code" })).toBeNull();
    const appium = screen.getByTestId("toolchain-item-appium");
    expect(within(appium).getByText("Needs Node.js. Install it from its page first.")).toBeInTheDocument();
    fireEvent.click(within(appium).getByRole("button", { name: "Open Node.js page" }));
    expect(toolchain.openPage).toHaveBeenCalledWith("node");

    fireEvent.click(screen.getByRole("button", { name: "Install selected and continue" }));
    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(toolchain.install).toHaveBeenCalledWith(["jdk"]);
  });

  it("continues, rather than only skipping, when everything left is installed by hand", async () => {
    stubShell({
      generatedAt: 1,
      platform: "linux",
      items: [
        item("git", "Git", { status: "installed" }),
        item("claude-code", "Claude Code", { installKind: "manual", method: undefined }),
      ],
    });
    const onDone = renderStep();

    fireEvent.click(await screen.findByRole("button", { name: "Continue" }));
    expect(screen.queryByRole("button", { name: "Install selected and continue" })).toBeNull();
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(flagWritten()).toBe(true);
  });

  it("holds Check again while a sweep is already running", async () => {
    let answer: (report: ToolchainReport) => void = () => {};
    const report = vi
      .fn()
      .mockRejectedValueOnce(new Error("sweep failed"))
      .mockImplementation(() => new Promise<ToolchainReport>((resolve) => (answer = resolve)));
    stubShell(WEB_REPORT, { report });
    renderStep();

    const again = await screen.findByRole("button", { name: "Check again" });
    expect(again).toBeEnabled();
    fireEvent.click(again);
    await waitFor(() => expect(screen.getByRole("button", { name: "Check again" })).toBeDisabled());
    expect(report).toHaveBeenCalledWith(true);
    act(() => answer(WEB_REPORT));
    expect(await screen.findByRole("checkbox", { name: "Node.js" })).toBeChecked();
  });

  it("moves on by itself when the shell has no installer", () => {
    window.__tasktrooperDesktop = {};
    const onDone = renderStep();
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(flagWritten()).toBe(true);
  });

  it("says so when nothing is missing", async () => {
    stubShell({
      ...WEB_REPORT,
      items: WEB_REPORT.items.map((entry) => ({ ...entry, status: "installed" as const })),
    });
    const onDone = renderStep();

    expect(await screen.findByText("Everything your team needs is already here")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(flagWritten()).toBe(true);
  });
});
