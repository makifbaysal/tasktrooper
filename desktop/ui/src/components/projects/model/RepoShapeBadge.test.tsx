import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { RepoShapeBadge } from "@/components/projects/model/RepoShapeBadge";
import { I18nProvider } from "@/hooks/useI18n";

describe("RepoShapeBadge", () => {
  it("gives single and monorepo distinct variants and localized labels", () => {
    const { container: single } = render(
      <I18nProvider>
        <RepoShapeBadge shape="single" />
      </I18nProvider>,
    );
    const { container: mono } = render(
      <I18nProvider>
        <RepoShapeBadge shape="monorepo" />
      </I18nProvider>,
    );
    expect(screen.getByText("Monorepo")).toBeInTheDocument();
    expect((single.firstElementChild as HTMLElement).className).not.toBe(
      (mono.firstElementChild as HTMLElement).className,
    );
  });
});
