import { describe, expect, it } from "vitest";
import { cssColor, cssDimension, cssShadow, flattenDesignTokens, groupDesignTokens, typographySample } from "@/lib/designTokens";

const tree = {
  color: {
    $type: "color",
    primary: { $value: "#1d4ed8" },
    accent: { $value: "{color.primary}" },
    broken: { $value: "{color.missing}" },
    $description: "brand colors",
  },
  surface: { $value: "oklch(0.98 0.01 265)" },
  spacing: {
    sm: { $value: "8px", $type: "dimension" },
    md: { $value: { value: 16, unit: "px" }, $type: "dimension" },
  },
  radius: { card: { $value: "12px", $type: "dimension" } },
  font: {
    body: { $type: "fontFamily", $value: ["Inter Variable", "sans-serif"] },
    size: { base: { $type: "dimension", $value: "14px" } },
  },
  shadow: {
    raised: {
      $type: "shadow",
      $value: { color: "#00000033", offsetX: "0px", offsetY: "1px", blur: "3px", spread: "0px" },
    },
  },
  motion: { fast: { $type: "duration", $value: "120ms" } },
  notAToken: "plain string",
};

describe("flattenDesignTokens", () => {
  it("walks groups, inherits $type and skips metadata", () => {
    const paths = flattenDesignTokens(tree).map((t) => t.path);
    expect(paths).toEqual([
      "color.primary",
      "color.accent",
      "color.broken",
      "surface",
      "spacing.sm",
      "spacing.md",
      "radius.card",
      "font.body",
      "font.size.base",
      "shadow.raised",
      "motion.fast",
    ]);
  });

  it("resolves aliases and keeps the alias text", () => {
    const tokens = flattenDesignTokens(tree);
    const accent = tokens.find((t) => t.path === "color.accent")!;
    expect(accent.value).toBe("{color.primary}");
    expect(accent.alias).toBe("color.primary");
    expect(accent.resolved).toBe("#1d4ed8");
    const broken = tokens.find((t) => t.path === "color.broken")!;
    expect(broken.resolved).toBeUndefined();
    expect(broken.category).toBe("color");
  });

  it("categorizes by $type, then by the value and path", () => {
    const groups = groupDesignTokens(flattenDesignTokens(tree));
    expect(groups.color.map((t) => t.path)).toEqual(["color.primary", "color.accent", "color.broken", "surface"]);
    expect(groups.dimension.map((t) => t.path)).toEqual(["spacing.sm", "spacing.md"]);
    expect(groups.radius.map((t) => t.path)).toEqual(["radius.card"]);
    expect(groups.typography.map((t) => t.path)).toEqual(["font.body", "font.size.base"]);
    expect(groups.shadow.map((t) => t.path)).toEqual(["shadow.raised"]);
    expect(groups.other.map((t) => t.path)).toEqual(["motion.fast"]);
  });

  it("survives a cyclic alias and a non-object tree", () => {
    const cyclic = { a: { $value: "{b}" }, b: { $value: "{a}" } };
    expect(flattenDesignTokens(cyclic).map((t) => t.resolved)).toEqual([undefined, undefined]);
    expect(flattenDesignTokens(null)).toEqual([]);
  });
});

describe("css helpers", () => {
  it("only lets real colors through", () => {
    expect(cssColor("#fff")).toBe("#fff");
    expect(cssColor("rgb(0 0 0 / 50%)")).toBe("rgb(0 0 0 / 50%)");
    expect(cssColor({ colorSpace: "srgb", components: [0, 0, 0], hex: "#000000" })).toBe("#000000");
    expect(cssColor("url(https://example.com/x.png)")).toBeUndefined();
    expect(cssColor("var(--x)")).toBeUndefined();
  });

  it("formats dimensions and shadows", () => {
    expect(cssDimension("1.5rem")).toBe("1.5rem");
    expect(cssDimension({ value: 4, unit: "px" })).toBe("4px");
    expect(cssDimension("calc(100% - 1px)")).toBeUndefined();
    expect(cssShadow({ color: "#000", offsetX: "0px", offsetY: "2px", blur: "4px", spread: "0px" })).toBe("0px 2px 4px 0px #000");
  });

  it("builds a type sample from a font family list", () => {
    const [body] = flattenDesignTokens({ body: { $type: "fontFamily", $value: ["Inter Variable", "sans-serif"] } });
    expect(typographySample(body)).toEqual({ fontFamily: '"Inter Variable", sans-serif' });
  });
});
