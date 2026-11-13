import { Search } from "lucide-react";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EmptyState } from "@/components/ui/empty-state";

const mascot = () => document.querySelector("[data-mascot]")?.getAttribute("data-mascot");

describe("EmptyState", () => {
  it("shows the sleepy mascot by default", () => {
    render(<EmptyState icon={Search} title="Nothing here" />);
    expect(mascot()).toBe("sleepy");
    expect(document.querySelector("svg")).toBeNull();
  });

  it("shows the oops mascot for the critical variant", () => {
    render(<EmptyState icon={Search} title="Something broke" variant="critical" />);
    expect(mascot()).toBe("oops");
  });

  it("shows the search mascot for the search variant", () => {
    render(<EmptyState icon={Search} title="No results" variant="search" />);
    expect(mascot()).toBe("search");
  });

  it("uses the pose it is given over the variant's", () => {
    render(<EmptyState icon={Search} title="Shipped" variant="critical" mascot="celebrate" />);
    expect(mascot()).toBe("celebrate");
  });

  it("keeps the tinted icon when the mascot is turned off", () => {
    render(<EmptyState icon={Search} title="Something broke" variant="critical" mascot={false} />);
    expect(mascot()).toBeUndefined();
    expect(document.querySelector("svg")!.parentElement!.className).toContain("bg-destructive/10");
  });
});
