import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SetupStepList } from "@/components/setup/SetupStepList";
import { I18nProvider } from "@/hooks/useI18n";
import type { SetupSteps } from "@/lib/setup";

const steps: SetupSteps = {
  environment: { id: "environment", state: "done", actionable: true },
  agent: { id: "agent", state: "todo", actionable: true },
  github: { id: "github", state: "todo", actionable: true },
  project: { id: "project", state: "todo", actionable: true },
};

describe("SetupStepList step states", () => {
  it("gives done, active and upcoming steps visually distinct treatments", () => {
    render(
      <I18nProvider>
        <SetupStepList steps={steps} selected="agent" onSelect={() => {}} />
      </I18nProvider>,
    );

    const circles = screen.getAllByText(/^(1|2|3|4)$/, { exact: false });
    const doneCircle = document.querySelector(".border-success");
    const activeCircle = document.querySelector(".border-primary");
    const upcomingCircle = document.querySelector(".border-muted-foreground\\/30");

    expect(doneCircle).toBeTruthy();
    expect(activeCircle).toBeTruthy();
    expect(upcomingCircle).toBeTruthy();
    expect(doneCircle!.className).not.toContain("border-primary");
    expect(activeCircle!.className).not.toContain("border-success");
    expect(circles.length).toBeGreaterThan(0);
  });
});
