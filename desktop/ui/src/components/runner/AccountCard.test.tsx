import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "@/api";
import { AccountCard } from "@/components/runner/AccountCard";
import { TemporaryLocalBanner } from "@/components/runner/TemporaryLocalBanner";
import { I18nProvider } from "@/hooks/useI18n";

function stubShell(mode: "local" | "account" = "local", extra: { temporaryLocal?: boolean; keys?: boolean } = {}) {
  const account = {
    signIn: vi.fn().mockResolvedValue(undefined),
    signOut: vi.fn().mockResolvedValue(undefined),
    state: vi.fn().mockResolvedValue({
      mode,
      origin: "https://app.tasktrooper.ai",
      ...(extra.temporaryLocal ? { temporaryLocal: true } : {}),
    }),
    ...(extra.keys ? { openKeys: vi.fn().mockResolvedValue(undefined) } : {}),
  };
  window.__tasktrooperDesktop = { account };
  return account;
}

function renderCard() {
  return render(
    <I18nProvider>
      <AccountCard />
    </I18nProvider>,
  );
}

afterEach(() => {
  delete window.__tasktrooperDesktop;
  vi.restoreAllMocks();
});

describe("AccountCard", () => {
  it("says accounts need the desktop app when there is no shell", () => {
    renderCard();
    expect(screen.getByText(/only available in the TaskTrooper desktop app/i)).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("connects at once when no local run would be stopped", async () => {
    const account = stubShell();
    vi.spyOn(api, "activeRuns").mockResolvedValue({ runs: [] });
    renderCard();
    fireEvent.click(await screen.findByRole("button", { name: /connect an account/i }));
    await waitFor(() => expect(account.signIn).toHaveBeenCalledTimes(1));
  });

  it("asks first, naming how many runs would stop, and connects only on confirm", async () => {
    const account = stubShell();
    vi.spyOn(api, "activeRuns").mockResolvedValue({ runs: [{}, {}] as never });
    renderCard();
    fireEvent.click(await screen.findByRole("button", { name: /connect an account/i }));
    expect(await screen.findByText("2 local runs are in progress")).toBeTruthy();
    expect(account.signIn).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));
    expect(screen.queryByText("2 local runs are in progress")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /connect an account/i }));
    fireEvent.click(await screen.findByRole("button", { name: /continue/i }));
    await waitFor(() => expect(account.signIn).toHaveBeenCalledTimes(1));
  });

  it("asks plainly when the local runs cannot be counted", async () => {
    const account = stubShell();
    vi.spyOn(api, "activeRuns").mockRejectedValue(new Error("offline"));
    renderCard();
    fireEvent.click(await screen.findByRole("button", { name: /connect an account/i }));
    expect(await screen.findByText("Stop the local server?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));
    await waitFor(() => expect(account.signIn).toHaveBeenCalledTimes(1));
  });

  it("signs out when the computer is in account mode", async () => {
    const account = stubShell("account");
    renderCard();
    expect(await screen.findByText(/Signed in to https:\/\/app\.tasktrooper\.ai/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /sign out/i }));
    await waitFor(() => expect(account.signOut).toHaveBeenCalledTimes(1));
  });

  it("has no API-key button, even in a shell with the key window", async () => {
    stubShell("local", { keys: true });
    renderCard();
    await screen.findByRole("button", { name: /connect an account/i });
    expect(screen.queryByRole("button", { name: /api keys/i })).toBeNull();
  });

  it("while running locally for now, offers the way back and the sign-out", async () => {
    const account = stubShell("local", { temporaryLocal: true });
    vi.spyOn(api, "activeRuns").mockResolvedValue({ runs: [] });
    renderCard();
    expect(await screen.findByText(/without it for now/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /back to the account/i }));
    await waitFor(() => expect(account.signIn).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole("button", { name: /sign out/i }));
    await waitFor(() => expect(account.signOut).toHaveBeenCalledTimes(1));
  });
});

describe("TemporaryLocalBanner", () => {
  function renderBanner() {
    return render(
      <I18nProvider>
        <TemporaryLocalBanner />
      </I18nProvider>,
    );
  }

  it("shows while running locally for now, and goes back on press", async () => {
    const account = stubShell("local", { temporaryLocal: true });
    renderBanner();
    expect(await screen.findByText("Using TaskTrooper without your account for now")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /back to the account/i }));
    await waitFor(() => expect(account.signIn).toHaveBeenCalledTimes(1));
  });

  it("shows nothing otherwise, or outside the shell", async () => {
    const account = stubShell("local");
    const { container } = renderBanner();
    await waitFor(() => expect(account.state).toHaveBeenCalled());
    expect(container.textContent).toBe("");
    delete window.__tasktrooperDesktop;
    expect(renderBanner().container.textContent).toBe("");
  });
});
