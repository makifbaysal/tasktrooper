import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ToolchainIndicator } from "@/components/layout/ToolchainIndicator";
import { I18nProvider } from "@/hooks/useI18n";
import type {
  DesktopRunnerHost,
  DesktopToolchainHost,
  TaskTrooperDesktopHost,
  ToolchainJobState,
} from "@/lib/desktop-bridge";

const IDLE: ToolchainJobState = { phase: "idle", steps: [], percent: 0 };

const RUNNING: ToolchainJobState = {
  phase: "running",
  jobId: "j1",
  percent: 60,
  steps: [
    { id: "homebrew", label: "Homebrew", state: "done", installKind: "interactive" },
    { id: "node", label: "Node.js", state: "running", installKind: "auto", detail: "Downloading node…" },
    { id: "xcode-clt", label: "Command Line Tools", state: "waiting", installKind: "interactive" },
    { id: "chrome", label: "Google Chrome", state: "queued", installKind: "auto" },
  ],
};

const DONE_WITH_FAILURES: ToolchainJobState = {
  phase: "done",
  jobId: "j1",
  percent: 100,
  restartRecommended: true,
  steps: [
    { id: "homebrew", label: "Homebrew", state: "done", installKind: "interactive" },
    { id: "node", label: "Node.js", state: "failed", installKind: "auto", error: "brew exited with 1" },
    { id: "chrome", label: "Google Chrome", state: "failed", installKind: "auto", error: "download failed" },
  ],
};

function stubShell(initial: ToolchainJobState, extra: Partial<TaskTrooperDesktopHost> = {}) {
  let push: (state: ToolchainJobState) => void = () => {};
  const toolchain: DesktopToolchainHost = {
    report: vi.fn().mockResolvedValue({ generatedAt: 1, platform: "darwin", items: [] }),
    plan: vi.fn().mockResolvedValue([]),
    state: vi.fn().mockResolvedValue(initial),
    subscribe: vi.fn((cb: (state: ToolchainJobState) => void) => {
      push = cb;
      return () => {};
    }),
    install: vi.fn().mockResolvedValue(initial),
    cancel: vi.fn().mockResolvedValue({ ...initial, phase: "done" }),
    dismiss: vi.fn().mockResolvedValue(IDLE),
    openPage: vi.fn().mockResolvedValue(undefined),
  };
  window.__tasktrooperDesktop = { toolchain, ...extra };
  return { toolchain, push: (state: ToolchainJobState) => act(() => push(state)) };
}

function renderIndicator() {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/board"]}>
        <ToolchainIndicator />
        <Routes>
          <Route path="/board" element={<p>Board page</p>} />
          <Route path="/settings/environment" element={<p>Environment settings page</p>} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

async function openMenu() {
  fireEvent.pointerDown(await screen.findByTestId("toolchain-indicator"), { button: 0 });
  return screen.findByRole("menu");
}

afterEach(() => {
  delete window.__tasktrooperDesktop;
});

describe("ToolchainIndicator", () => {
  it("renders nothing outside the desktop app", () => {
    renderIndicator();
    expect(screen.queryByTestId("toolchain-indicator")).toBeNull();
  });

  it("renders nothing on a shell without the toolchain bridge", () => {
    window.__tasktrooperDesktop = {
      runner: { snapshot: () => Promise.resolve(null), subscribe: () => () => {} } as unknown as DesktopRunnerHost,
    };
    renderIndicator();
    expect(screen.queryByTestId("toolchain-indicator")).toBeNull();
  });

  it("renders nothing while idle, and never runs detection", async () => {
    const { toolchain } = stubShell(IDLE);
    renderIndicator();
    await waitFor(() => expect(toolchain.state).toHaveBeenCalled());
    await act(async () => {});
    expect(screen.queryByTestId("toolchain-indicator")).toBeNull();
    expect(toolchain.report).not.toHaveBeenCalled();
  });

  it("shows the running percent and follows the shell's pushes", async () => {
    const { push } = stubShell(RUNNING);
    renderIndicator();
    expect(await screen.findByText("Setting up · 60%")).toBeInTheDocument();

    push({ ...RUNNING, percent: 80 });
    expect(screen.getByText("Setting up · 80%")).toBeInTheDocument();

    push(IDLE);
    expect(screen.queryByTestId("toolchain-indicator")).toBeNull();
  });

  it("says setup is complete when nothing failed", async () => {
    stubShell({ ...DONE_WITH_FAILURES, restartRecommended: false, steps: DONE_WITH_FAILURES.steps.slice(0, 1) });
    renderIndicator();
    expect(await screen.findByText("Setup complete")).toBeInTheDocument();
  });

  // One test drives the whole menu: Radix menus in jsdom refuse to open in a
  // fresh render once any earlier test in the file has opened one, while the
  // same live component reopens fine (see NotificationCenter.test).
  it("drives the job from its menu: steps, stop, failures, details, dismiss, restart", async () => {
    const runner = { restartChild: vi.fn().mockResolvedValue(null), restart: vi.fn() } as unknown as DesktopRunnerHost;
    const { toolchain } = stubShell(RUNNING, { runner, mode: "local" });
    vi.mocked(toolchain.cancel).mockResolvedValue(DONE_WITH_FAILURES);
    // Answering with the finished job keeps the chip (and its menu) mounted for the restart below.
    vi.mocked(toolchain.dismiss).mockResolvedValue(DONE_WITH_FAILURES);
    renderIndicator();
    await screen.findByText("Setting up · 60%");

    await openMenu();
    expect(screen.getByText("Setting up this computer")).toBeInTheDocument();
    expect(screen.getByText("Downloading node…")).toBeInTheDocument();
    expect(screen.getByText(/An installer or Terminal window is open/)).toBeInTheDocument();
    expect(screen.getByText("1 of 4 finished")).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /dismiss/i })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: /restart agents/i })).toBeNull();

    fireEvent.click(screen.getByRole("menuitem", { name: /stop/i }));
    await waitFor(() => expect(toolchain.cancel).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("Setup: 2 failed")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());

    await openMenu();
    expect(screen.getByText("brew exited with 1")).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /stop/i })).toBeNull();
    fireEvent.click(screen.getByRole("menuitem", { name: /details/i }));
    expect(await screen.findByText("Environment settings page")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());

    await openMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: /dismiss/i }));
    await waitFor(() => expect(toolchain.dismiss).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());

    // Last: after a Radix dialog has been up, jsdom will not reopen the menu.
    await openMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: /restart agents/i }));
    fireEvent.click(await screen.findByRole("button", { name: "Restart" }));
    await waitFor(() => expect(runner.restartChild).toHaveBeenCalledWith("agent-server"));
    expect(runner.restart).not.toHaveBeenCalled();
  });
});
