import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { AppStoreConnectCard } from "@/components/admin/AppStoreConnectCard";
import { I18nProvider } from "@/hooks/useI18n";

function renderCard(configured: boolean) {
  render(
    <MemoryRouter>
      <I18nProvider>
        <AppStoreConnectCard
          credential={configured ? { provider: "asc", configured: true, updated_at: "2026-09-15T20:12:00Z" } : undefined}
          onChanged={vi.fn()}
        />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("AppStoreConnectCard", () => {
  it("does not ask for the key again once one is saved", () => {
    renderCard(true);

    expect(screen.getByText("Connected")).toBeInTheDocument();
    expect(screen.queryByLabelText("Key ID")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Replace key" }));
    expect(screen.getByLabelText("Key ID")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByLabelText("Key ID")).not.toBeInTheDocument();
  });

  it("asks for the key while none is saved", () => {
    renderCard(false);

    expect(screen.getByText("Not connected")).toBeInTheDocument();
    expect(screen.getByLabelText("Key ID")).toBeInTheDocument();
  });
});
