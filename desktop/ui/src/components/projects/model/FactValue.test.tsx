import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Fact } from "@/api";
import { FactValue } from "@/components/projects/model/FactValue";
import { I18nProvider } from "@/hooks/useI18n";

function renderFact(fact: Fact<string>, onRevert?: () => void) {
  return render(
    <I18nProvider>
      <FactValue fact={fact} onRevert={onRevert} />
    </I18nProvider>,
  );
}

describe("FactValue", () => {
  it("renders only the value when it is not overridden", () => {
    renderFact({ detected: "Go" });
    expect(screen.getByText("Go")).toBeInTheDocument();
    expect(screen.queryByText("Edited by you")).not.toBeInTheDocument();
  });

  it("shows the edited marker when overridden, and the override wins over detected", () => {
    renderFact({ detected: "Go", override: "Rust" });
    expect(screen.getByText("Rust")).toBeInTheDocument();
    expect(screen.getByText("Edited by you")).toBeInTheDocument();
  });

  it("renders the empty label when neither detected nor override is set", () => {
    render(
      <I18nProvider>
        <FactValue fact={{}} emptyLabel="Not set" />
      </I18nProvider>,
    );
    expect(screen.getByText("Not set")).toBeInTheDocument();
  });

  it("only offers revert when overridden and onRevert is given, and calls it on click", () => {
    const onRevert = vi.fn();
    renderFact({ detected: "Go", override: "Rust" }, onRevert);
    const button = screen.getByText("Revert to detected");
    fireEvent.click(button);
    expect(onRevert).toHaveBeenCalledTimes(1);
  });

  it("does not offer revert when not overridden, even with onRevert given", () => {
    const onRevert = vi.fn();
    renderFact({ detected: "Go" }, onRevert);
    expect(screen.queryByText("Revert to detected")).not.toBeInTheDocument();
  });
});
