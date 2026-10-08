import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AppUpdatesCard } from "@/components/runner/AppUpdatesCard";
import { I18nProvider } from "@/hooks/useI18n";
import type { DesktopUpdateStatus, DesktopUpdatesHost } from "@/lib/desktop-bridge";

function stubShell(initial: DesktopUpdateStatus, overrides: Partial<DesktopUpdatesHost> = {}) {
  let push: (status: DesktopUpdateStatus) => void = () => {};
  const updates: DesktopUpdatesHost = {
    status: vi.fn().mockResolvedValue(initial),
    subscribe: vi.fn((cb: (status: DesktopUpdateStatus) => void) => {
      push = cb;
      return () => {};
    }),
    check: vi.fn().mockResolvedValue(initial),
    restart: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  window.__tasktrooperDesktop = {
    info: () => Promise.resolve({ app: "tasktrooper-desktop", version: "0.2.22", platform: "darwin" }),
    updates,
  };
  return { updates, push: (status: DesktopUpdateStatus) => act(() => push(status)) };
}

function renderCard() {
  return render(
    <I18nProvider>
      <AppUpdatesCard />
    </I18nProvider>,
  );
}

afterEach(() => {
  delete window.__tasktrooperDesktop;
});

describe("AppUpdatesCard", () => {
  it("says updates need the desktop app when there is no shell", () => {
    renderCard();
    expect(screen.getByText(/only available in the TaskTrooper desktop app/i)).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("shows the running version and checks the feed on demand", async () => {
    const { updates } = stubShell(
      { phase: "current", checkedAt: Date.now() },
      { check: vi.fn().mockResolvedValue({ phase: "current", checkedAt: Date.now() }) },
    );
    renderCard();

    expect(await screen.findByText("You are running TaskTrooper 0.2.22.")).toBeTruthy();
    expect(screen.getByText("You're on the latest version.")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /check for updates/i }));
    await waitFor(() => expect(updates.check).toHaveBeenCalledTimes(1));
  });

  it("follows the download pushed by the shell, then offers to restart once it is staged", async () => {
    const { updates, push } = stubShell({ phase: "idle" });
    renderCard();
    await screen.findByRole("button", { name: /check for updates/i });

    push({ phase: "available", version: "0.2.23", percent: 40 });
    expect(screen.getByText("Downloading TaskTrooper 0.2.23… 40%")).toBeTruthy();
    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("40");
    expect(screen.queryByRole("button", { name: /check for updates/i })).toBeNull();

    push({ phase: "ready", version: "0.2.23", percent: 100 });
    expect(screen.getByText("TaskTrooper 0.2.23 is ready to install.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /restart and install/i }));
    await waitFor(() => expect(updates.restart).toHaveBeenCalledTimes(1));
  });

  it("explains a build that cannot update itself, without a check button", async () => {
    stubShell({ phase: "unsupported", detail: "This build carries no update feed." });
    renderCard();
    expect(await screen.findByText("This build carries no update feed.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /check for updates/i })).toBeNull();
  });
});
