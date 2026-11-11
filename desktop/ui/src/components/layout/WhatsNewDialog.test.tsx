import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WhatsNewOnLaunch } from "@/components/layout/WhatsNewDialog";
import { WhatsNewCard } from "@/components/runner/WhatsNewCard";
import { I18nProvider } from "@/hooks/useI18n";
import { WHATS_NEW } from "@/lib/whatsNew/entries";
import { WHATS_NEW_SEEN_KEY } from "@/lib/whatsNew/seen";

const VERSION = WHATS_NEW.find((r) => "items" in r)!.version;

function stubHost(version: string, extra: Record<string, unknown> = {}) {
  window.__tasktrooperDesktop = {
    info: () => Promise.resolve({ app: "tasktrooper-desktop", version, platform: "darwin" }),
    ...extra,
  } as never;
}

function renderLaunch() {
  return render(
    <I18nProvider>
      <WhatsNewOnLaunch />
    </I18nProvider>,
  );
}

beforeEach(() => window.localStorage.clear());
afterEach(() => {
  cleanup();
  delete window.__tasktrooperDesktop;
  vi.restoreAllMocks();
});

describe("WhatsNewOnLaunch", () => {
  it("opens a modal dialog on the first launch after the version changed, and records it", async () => {
    window.localStorage.setItem(WHATS_NEW_SEEN_KEY, "0.3.12");
    stubHost(VERSION);
    renderLaunch();

    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain(`What's new in TaskTrooper ${VERSION}`);
    expect(dialog.querySelectorAll("li").length).toBeGreaterThanOrEqual(3);
    expect(window.localStorage.getItem(WHATS_NEW_SEEN_KEY)).toBe(VERSION);
  });

  it("closes with the button and with Escape", async () => {
    window.localStorage.setItem(WHATS_NEW_SEEN_KEY, "0.3.12");
    stubHost(VERSION);
    renderLaunch();

    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Got it" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    cleanup();
    window.localStorage.setItem(WHATS_NEW_SEEN_KEY, "0.3.12");
    renderLaunch();
    await screen.findByRole("dialog");
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape", code: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("opens the release page through the shell", async () => {
    window.localStorage.setItem(WHATS_NEW_SEEN_KEY, "0.3.12");
    const openExternal = vi.fn().mockResolvedValue(true);
    stubHost(VERSION, { runner: { openExternal } });
    renderLaunch();

    fireEvent.click(await screen.findByRole("button", { name: "See all changes" }));
    expect(openExternal).toHaveBeenCalledWith(`https://github.com/makifbaysal/tasktrooper/releases/tag/v${VERSION}`);
  });

  it("stays silent on a fresh install and records the version", async () => {
    stubHost(VERSION);
    renderLaunch();

    await waitFor(() => expect(window.localStorage.getItem(WHATS_NEW_SEEN_KEY)).toBe(VERSION));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("does not show twice for the same version", async () => {
    window.localStorage.setItem(WHATS_NEW_SEEN_KEY, VERSION);
    stubHost(VERSION);
    renderLaunch();

    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("shows nothing for a version with no entry, but records it", async () => {
    window.localStorage.setItem(WHATS_NEW_SEEN_KEY, "0.3.12");
    stubHost("9.9.9");
    renderLaunch();

    await waitFor(() => expect(window.localStorage.getItem(WHATS_NEW_SEEN_KEY)).toBe("9.9.9"));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("shows nothing in a browser with no desktop shell", async () => {
    window.localStorage.setItem(WHATS_NEW_SEEN_KEY, "0.3.12");
    renderLaunch();

    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(window.localStorage.getItem(WHATS_NEW_SEEN_KEY)).toBe("0.3.12");
  });
});

describe("WhatsNewCard", () => {
  it("reopens the latest notes from Settings", async () => {
    render(
      <I18nProvider>
        <WhatsNewCard />
      </I18nProvider>,
    );

    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Show what's new" }));
    expect(await screen.findByRole("dialog")).toBeTruthy();
  });
});
