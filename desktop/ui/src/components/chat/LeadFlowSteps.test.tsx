import "@testing-library/jest-dom/vitest";
import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { LeadFlowSteps } from "@/components/chat/LeadFlowSteps";
import { I18nProvider } from "@/hooks/useI18n";
import { setPMUATEnabled } from "@/hooks/usePMUATEnabled";

vi.mock(import("@/api"), async (importOriginal) => {
  const actual = await importOriginal();
  return { ...actual, api: { ...actual.api, getSettings: vi.fn(() => new Promise<never>(() => {})) } };
});

const lead = { id: "pm", name: "Pia", enabled: true } as Agent;

function renderFlow() {
  return render(
    <I18nProvider>
      <LeadFlowSteps agent={lead} />
    </I18nProvider>,
  );
}

const cellOf = (key: string) => screen.getByTestId(`flow-step-${key}`).className;

describe("LeadFlowSteps", () => {
  afterEach(() => act(() => setPMUATEnabled(true)));

  it("shows all eleven steps while the PM UAT step is on", () => {
    act(() => setPMUATEnabled(true));
    renderFlow();
    expect(screen.getAllByTestId(/^flow-step-/)).toHaveLength(11);
    expect(screen.getByTestId("flow-step-acceptance")).toBeInTheDocument();
    expect(cellOf("design")).toContain("sm:row-start-1");
    expect(cellOf("build")).toContain("sm:col-span-5 sm:col-start-[26] sm:row-start-1");
    expect(cellOf("deploy")).toContain("sm:col-start-1 sm:row-start-2");
  });

  it("drops the PM check and re-flows into two even rows when it is off", () => {
    act(() => setPMUATEnabled(false));
    renderFlow();
    expect(screen.getAllByTestId(/^flow-step-/)).toHaveLength(10);
    expect(screen.queryByTestId("flow-step-acceptance")).not.toBeInTheDocument();
    expect(cellOf("design")).toContain("sm:row-start-1");
    expect(cellOf("build")).toContain("sm:col-span-6 sm:col-start-[25] sm:row-start-2");
    expect(cellOf("deploy")).toContain("sm:col-start-1 sm:row-start-2");
  });
});
