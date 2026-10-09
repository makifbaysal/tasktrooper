import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FirstRunChoice } from "@/components/setup/FirstRunChoice";
import { I18nProvider } from "@/hooks/useI18n";

function renderChoice() {
  return render(
    <I18nProvider>
      <FirstRunChoice onChosen={vi.fn()} />
    </I18nProvider>,
  );
}

afterEach(() => {
  delete window.__tasktrooperDesktop;
});

describe("FirstRunChoice", () => {
  it("hides the account option when the bridge has no account mode", () => {
    renderChoice();

    expect(screen.getByRole("button", { name: "Continue without an account" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sign in" })).toBeNull();
  });

  it("offers the account option when the bridge exposes signIn", () => {
    window.__tasktrooperDesktop = {
      account: { signIn: async () => {}, signOut: async () => {}, state: async () => ({ mode: "local" }) },
    };

    renderChoice();

    expect(screen.getByRole("button", { name: "Continue without an account" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });
});
