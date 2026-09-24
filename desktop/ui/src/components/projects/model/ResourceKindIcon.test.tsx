import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ResourceKind } from "@/api";
import { ResourceKindIcon } from "@/components/projects/model/ResourceKindIcon";

describe("ResourceKindIcon", () => {
  it("renders a distinct icon per known resource kind", () => {
    const kinds: ResourceKind[] = [
      "database", "cache", "queue", "storage", "search", "api", "auth", "email", "payments", "ai", "observability",
    ];
    const seen = new Set<string>();
    for (const kind of kinds) {
      const { container, unmount } = render(<ResourceKindIcon kind={kind} />);
      seen.add(container.innerHTML);
      unmount();
    }
    expect(seen.size).toBe(kinds.length);
  });

  it("falls back to the 'other' icon for a kind the server may send that this build does not know yet", () => {
    const { container: known } = render(<ResourceKindIcon kind="other" />);
    const { container: unknown } = render(<ResourceKindIcon kind={"future_kind" as ResourceKind} />);
    expect(unknown.innerHTML).toBe(known.innerHTML);
  });
});
