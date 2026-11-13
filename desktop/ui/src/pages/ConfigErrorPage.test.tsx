import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ConfigErrorPage } from "@/pages/ConfigErrorPage";

describe("ConfigErrorPage", () => {
  it("renders the critical empty state with the oops mascot", () => {
    render(<ConfigErrorPage missing={["VITE_API_KEY"]} />);
    expect(document.querySelector("[data-mascot]")?.getAttribute("data-mascot")).toBe("oops");
    expect(screen.getByText("VITE_API_KEY")).toBeTruthy();
  });
});
