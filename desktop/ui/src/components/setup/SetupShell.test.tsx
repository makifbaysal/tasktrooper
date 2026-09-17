import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SetupShell } from "@/components/setup/SetupShell";

describe("SetupShell brand", () => {
  it("shows the Logo mark and the TaskTrooper name at the top of the flow", () => {
    render(
      <SetupShell title="Get TaskTrooper ready">
        <p>body</p>
      </SetupShell>,
    );
    expect(screen.getByText("TaskTrooper")).toBeTruthy();
    expect(document.querySelector("svg")).toBeTruthy();
  });
});
