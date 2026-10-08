import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DesignTokensPreview } from "@/components/projects/designsystem/DesignTokensPreview";
import { I18nProvider } from "@/hooks/useI18n";

function renderPreview(tokens: Record<string, unknown> | null, overriddenPaths?: string[]) {
  return render(
    <I18nProvider>
      <DesignTokensPreview tokens={tokens} overriddenPaths={overriddenPaths} />
    </I18nProvider>,
  );
}

describe("DesignTokensPreview", () => {
  it("draws color tokens as swatches painted with their own value", () => {
    renderPreview({ color: { $type: "color", primary: { $value: "#ff0000" }, ink: { $value: "#000000" } } });

    const colors = screen.getByRole("region", { name: "Colors" });
    expect(within(colors).getAllByTestId("token-swatch")).toHaveLength(2);
    const swatch = within(colors).getByRole("img", { name: "color.primary: #ff0000" });
    expect(swatch).toHaveStyle({ backgroundColor: "rgb(255, 0, 0)" });
    expect(within(colors).getByText("primary")).toBeInTheDocument();
    expect(within(colors).getByText("#ff0000")).toBeInTheDocument();
  });

  it("shows an alias as written and paints the color it resolves to", () => {
    renderPreview({
      color: {
        $type: "color",
        primary: { $value: "#0000ff" },
        accent: { $value: "{color.primary}" },
        lost: { $value: "{color.nowhere}" },
      },
    });

    expect(screen.getByText("{color.primary}")).toBeInTheDocument();
    expect(screen.getByText("Alias of color.primary → #0000ff")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "color.accent: {color.primary}" })).toHaveStyle({
      backgroundColor: "rgb(0, 0, 255)",
    });
    expect(screen.getByText("{color.nowhere}")).toBeInTheDocument();
    expect(screen.getByText("Unresolved alias")).toBeInTheDocument();
  });

  it("draws spacing as bars and radii as boxes sized by the token", () => {
    renderPreview({
      spacing: { $type: "dimension", sm: { $value: "8px" }, lg: { $value: { value: 24, unit: "px" } } },
      radius: { card: { $type: "dimension", $value: "12px" } },
    });

    const spacing = screen.getByRole("region", { name: "Spacing and sizes" });
    const rows = within(spacing).getAllByTestId("token-dimension");
    expect(rows).toHaveLength(2);
    expect(rows[0].lastElementChild).toHaveStyle({ width: "8px" });
    expect(rows[1].lastElementChild).toHaveStyle({ width: "24px" });
    expect(within(spacing).getByText("spacing.sm")).toBeInTheDocument();

    const radii = screen.getByRole("region", { name: "Radii" });
    expect(within(radii).getByTestId("token-radius").firstElementChild).toHaveStyle({ borderRadius: "12px" });
  });

  it("lists everything it cannot draw in a path → value table", () => {
    renderPreview({
      motion: { fast: { $type: "duration", $value: "120ms" } },
      weird: { thing: { $value: { a: 1 } } },
      notAToken: 42,
    });

    const other = screen.getByRole("region", { name: "Other tokens" });
    const rows = within(other).getAllByTestId("token-other");
    expect(rows).toHaveLength(2);
    expect(within(rows[0]).getByText("motion.fast")).toBeInTheDocument();
    expect(within(rows[0]).getByText("duration")).toBeInTheDocument();
    expect(within(rows[0]).getByText("120ms")).toBeInTheDocument();
    expect(within(rows[1]).getByText('{"a":1}')).toBeInTheDocument();
  });

  it("marks overridden tokens", () => {
    renderPreview({ color: { primary: { $type: "color", $value: "#123456" } } }, ["color.primary"]);
    expect(screen.getByText("Overridden")).toBeInTheDocument();
  });

  it("says so when there is nothing to show", () => {
    renderPreview(null);
    expect(screen.getByText("This version has no tokens.")).toBeInTheDocument();
  });
});
