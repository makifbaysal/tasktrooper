import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ProjectType } from "@/api";
import { ProjectTypeBadge } from "@/components/projects/model/ProjectTypeBadge";
import { I18nProvider } from "@/hooks/useI18n";

function renderType(type: ProjectType) {
  return render(
    <I18nProvider>
      <ProjectTypeBadge type={type} />
    </I18nProvider>,
  );
}

describe("ProjectTypeBadge", () => {
  it("renders the localized label for every project type", () => {
    renderType("monorepo");
    expect(screen.getByText("Monorepo")).toBeInTheDocument();
  });

  it("gives monorepo and multi_repo visually distinct variants", () => {
    const { container: mono } = renderType("monorepo");
    const { container: multi } = renderType("multi_repo");
    expect((mono.firstElementChild as HTMLElement).className).not.toBe(
      (multi.firstElementChild as HTMLElement).className,
    );
  });
});
