import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PageHeader } from "@/components/admin/PageHeader";

describe("PageHeader type scale", () => {
  it("renders the title on the display scale and the description on the caption scale", () => {
    render(<PageHeader title="Deployments" description="What is live where" />);
    expect(screen.getByRole("heading", { name: "Deployments" }).className).toContain("text-display");
    expect(screen.getByText("What is live where").className).toContain("text-caption");
  });
});
