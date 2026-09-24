import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ComponentRole } from "@/api";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { I18nProvider } from "@/hooks/useI18n";

function renderRole(role: ComponentRole) {
  return render(
    <I18nProvider>
      <RoleBadge role={role} />
    </I18nProvider>,
  );
}

describe("RoleBadge", () => {
  it("gives every role a distinct color token", () => {
    const roles: ComponentRole[] = [
      "frontend", "backend", "mobile", "desktop", "worker", "library", "infra", "cli", "other",
    ];
    const seen = new Set<string>();
    for (const role of roles) {
      const { container, unmount } = renderRole(role);
      const badge = container.firstElementChild as HTMLElement;
      seen.add(badge.className);
      unmount();
    }
    expect(seen.size).toBe(roles.length);
  });

  it("renders the localized role label", () => {
    renderRole("backend");
    expect(screen.getByText("Backend")).toBeInTheDocument();
  });
});
