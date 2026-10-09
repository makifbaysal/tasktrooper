import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FirstRunChoice } from "@/components/setup/FirstRunChoice";
import { I18nProvider } from "@/hooks/useI18n";
import { FIRST_RUN_MODE_KEY } from "@/lib/firstRun";

const signIn = vi.fn<() => Promise<void>>();

function renderChoice() {
  return render(
    <I18nProvider>
      <FirstRunChoice onChosen={vi.fn()} />
    </I18nProvider>,
  );
}

function bridgeWithAccount() {
  window.__tasktrooperDesktop = {
    account: { signIn, signOut: async () => {}, state: async () => ({ mode: "local" }) },
  };
}

afterEach(() => {
  delete window.__tasktrooperDesktop;
  window.localStorage.clear();
  signIn.mockReset();
});

describe("FirstRunChoice", () => {
  it("hides the account option when the bridge has no account mode", () => {
    renderChoice();

    expect(screen.getByRole("button", { name: "Continue without an account" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sign in" })).toBeNull();
  });

  it("offers the account option when the bridge exposes signIn", () => {
    bridgeWithAccount();

    renderChoice();

    expect(screen.getByRole("button", { name: "Continue without an account" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });

  it("writes the account choice before awaiting signIn", async () => {
    let resolveSignIn: () => void = () => {};
    signIn.mockImplementation(() => new Promise<void>((resolve) => (resolveSignIn = resolve)));
    bridgeWithAccount();
    renderChoice();

    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(window.localStorage.getItem(FIRST_RUN_MODE_KEY)).toBe("account");

    resolveSignIn();
    await waitFor(() => expect(screen.getByRole("button", { name: "Signing in…" })).toBeDisabled());
  });

  it("clears the account choice when signIn rejects", async () => {
    signIn.mockRejectedValue(new Error("boom"));
    bridgeWithAccount();
    renderChoice();

    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByText("boom")).toBeInTheDocument();
    expect(window.localStorage.getItem(FIRST_RUN_MODE_KEY)).toBeNull();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
  });
});
