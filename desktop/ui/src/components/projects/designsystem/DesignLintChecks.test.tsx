import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DesignLintChecks } from "@/components/projects/designsystem/DesignLintChecks";
import { I18nProvider } from "@/hooks/useI18n";

describe("DesignLintChecks", () => {
  it("lists errors before warnings, each with its severity", () => {
    render(
      <I18nProvider>
        <DesignLintChecks
          findings={[
            { code: "missing_section", severity: "warning", value: "Do's and Don'ts" },
            { code: "contrast_below_aa", severity: "error", path: "color.accent", related_path: "color.on-accent", ratio: 1.82 },
          ]}
        />
      </I18nProvider>,
    );

    const rows = screen.getAllByRole("listitem");
    expect(rows[0]).toHaveTextContent("Errorcolor.on-accent on color.accent is 1.82:1 — AA needs 4.5:1");
    expect(rows[1]).toHaveTextContent("WarningDESIGN.md has no Do's and Don'ts section");
    expect(screen.getByText("Errors: 1")).toBeInTheDocument();
    expect(screen.getByText("Warnings: 1")).toBeInTheDocument();
    expect(screen.queryByText("No problems found")).not.toBeInTheDocument();
  });

  it("says no problems were found when there are none (or the list is absent)", () => {
    render(
      <I18nProvider>
        <DesignLintChecks findings={undefined} />
      </I18nProvider>,
    );
    expect(screen.getByText("No problems found")).toBeInTheDocument();
  });
});
