import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Badge } from "@/components/ui/badge";

describe("Badge status variants", () => {
  it("gives the info variant the --info token, distinguishable from success/warning/destructive", () => {
    render(<Badge variant="info">Running</Badge>);
    const badge = screen.getByText("Running");
    expect(badge.className).toContain("bg-info/15");
    expect(badge.className).toContain("text-info");
    expect(badge.className).not.toContain("bg-success");
    expect(badge.className).not.toContain("bg-warning");
    expect(badge.className).not.toContain("bg-destructive");
  });
});
