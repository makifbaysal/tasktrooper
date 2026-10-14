import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ToolchainStatusBadge } from "@/components/toolchain/ToolchainStatusBadge";
import { I18nProvider } from "@/hooks/useI18n";
import type { ToolchainJobState, ToolchainJobStep } from "@/lib/desktop-bridge";
import { ALREADY_INSTALLED } from "@/lib/toolchainJob";

function done(...steps: Partial<ToolchainJobStep>[]): ToolchainJobState {
  return {
    phase: "done",
    percent: 100,
    steps: steps.map((step, i) => ({ id: "git", label: `Step ${i}`, state: "done", installKind: "auto", ...step })),
  };
}

function renderBadge(state: ToolchainJobState) {
  return render(
    <I18nProvider>
      <ToolchainStatusBadge state={state} />
    </I18nProvider>,
  );
}

describe("ToolchainStatusBadge", () => {
  it("says complete when every step is installed or was already there", () => {
    renderBadge(done({ state: "done" }, { state: "skipped", error: ALREADY_INSTALLED }));
    expect(screen.getByText("Setup complete")).toBeInTheDocument();
  });

  it("says stopped, not complete, when steps were cancelled", () => {
    renderBadge(done({ state: "done" }, { state: "cancelled", detail: "Still finishing in the background…" }));
    expect(screen.getByText("Setup stopped")).toBeInTheDocument();
    expect(screen.queryByText("Setup complete")).toBeNull();
  });

  it("says incomplete when a step was skipped for a missing dependency", () => {
    renderBadge(done({ state: "done" }, { state: "skipped", error: "Needs Node.js, which did not install." }));
    expect(screen.getByText("Setup incomplete")).toBeInTheDocument();
  });

  it("counts failures before anything else", () => {
    renderBadge(done({ state: "failed" }, { state: "failed" }, { state: "cancelled" }));
    expect(screen.getByText("Setup: 2 failed")).toBeInTheDocument();
  });

  it("renders nothing while idle", () => {
    const { container } = renderBadge({ phase: "idle", percent: 0, steps: [] });
    expect(container).toBeEmptyDOMElement();
  });
});
