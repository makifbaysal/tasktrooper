import { Search } from "lucide-react";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EmptyState } from "@/components/ui/empty-state";

describe("EmptyState variant", () => {
  it("defaults to the muted circle treatment when no variant is given", () => {
    render(<EmptyState icon={Search} title="Nothing here" />);
    const iconWrapper = document.querySelector("svg")!.parentElement!;
    expect(iconWrapper.className).toContain("bg-muted");
  });

  it("renders the critical variant with a destructive-tinted icon wrapper", () => {
    render(<EmptyState icon={Search} title="Something broke" variant="critical" />);
    const iconWrapper = document.querySelector("svg")!.parentElement!;
    expect(iconWrapper.className).toContain("bg-destructive/10");
  });

  it("renders the search variant with an info-tinted icon wrapper", () => {
    render(<EmptyState icon={Search} title="No results" variant="search" />);
    const iconWrapper = document.querySelector("svg")!.parentElement!;
    expect(iconWrapper.className).toContain("bg-info/10");
  });
});
