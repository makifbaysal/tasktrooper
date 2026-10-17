import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Card } from "@/components/ui/card";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { I18nProvider } from "@/hooks/useI18n";

describe("elevation", () => {
  it("gives Card the Level-1 raised shadow", () => {
    render(<Card data-testid="card">content</Card>);
    expect(screen.getByTestId("card").className).toContain("shadow-[var(--shadow-raised)]");
  });

  it("gives a Dialog's content surface a visibly heavier Level-2 shadow than Card", () => {
    render(
      <I18nProvider>
        <Dialog open>
          <DialogContent aria-describedby={undefined}>dialog body</DialogContent>
        </Dialog>
      </I18nProvider>,
    );
    const content = screen.getByRole("dialog");
    expect(content.className).toContain("shadow-[var(--shadow-overlay)]");
    expect(content.className).not.toContain("shadow-[var(--shadow-raised)]");
  });

  it("names a Dialog's close button in the reader's language", () => {
    render(
      <I18nProvider>
        <Dialog open>
          <DialogContent aria-describedby={undefined}>dialog body</DialogContent>
        </Dialog>
      </I18nProvider>,
    );
    expect(screen.getByRole("button", { name: "Close" })).toBeInTheDocument();
    expect(screen.queryByText("Kapat")).toBeNull();
  });
});
