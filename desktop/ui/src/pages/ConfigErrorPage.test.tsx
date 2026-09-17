import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ConfigErrorPage } from "@/pages/ConfigErrorPage";

describe("ConfigErrorPage", () => {
  it("renders the critical empty-state variant, distinct from the default treatment", () => {
    render(<ConfigErrorPage missing={["VITE_API_KEY"]} />);
    const iconWrapper = document.querySelector("svg")!.parentElement!;
    expect(iconWrapper.className).toContain("bg-destructive/10");
    expect(screen.getByText("VITE_API_KEY")).toBeTruthy();
  });
});
