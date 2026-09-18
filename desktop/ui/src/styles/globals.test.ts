import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";

// Vite's `?raw` loader returns an empty string for this file because of its
// `@import "tailwindcss"` directive, so the token values are read straight
// off disk instead — vitest runs under Node, unlike the shipped app code.
// vitest always runs from the `ui` package root, so this is stable.
const css = readFileSync("src/styles/globals.css", "utf-8");

function readBlock(selector: string): string {
  const start = css.indexOf(`${selector} {`);
  if (start === -1) throw new Error(`selector not found: ${selector}`);
  const end = css.indexOf("}", start);
  return css.slice(start, end);
}

function readOklch(block: string, name: string): { l: number; c: number; h: number } {
  const match = block.match(new RegExp(`${name}:\\s*oklch\\(([^)]+)\\)`));
  if (!match) throw new Error(`token not found: ${name}`);
  const [l, c, h] = match[1].trim().split(/\s+/).map(Number);
  return { l, c, h };
}

function oklchToSrgb({ l, c, h }: { l: number; c: number; h: number }): [number, number, number] {
  const hr = (h * Math.PI) / 180;
  const a = c * Math.cos(hr);
  const b = c * Math.sin(hr);

  const l_ = l + 0.3963377774 * a + 0.2158037573 * b;
  const m_ = l - 0.1055613458 * a - 0.0638541728 * b;
  const s_ = l - 0.0894841775 * a - 1.291485548 * b;

  const ll = l_ ** 3;
  const mm = m_ ** 3;
  const ss = s_ ** 3;

  const linToSrgb = (v: number) => {
    const clamped = Math.min(1, Math.max(0, v));
    return clamped <= 0.0031308 ? 12.92 * clamped : 1.055 * clamped ** (1 / 2.4) - 0.055;
  };

  const r = linToSrgb(4.0767416621 * ll - 3.3077115913 * mm + 0.2309699292 * ss);
  const g = linToSrgb(-1.2684380046 * ll + 2.6097574011 * mm - 0.3413193965 * ss);
  const bb = linToSrgb(-0.0041960863 * ll - 0.7034186147 * mm + 1.707614701 * ss);

  return [r, g, bb];
}

function relativeLuminance([r, g, b]: [number, number, number]): number {
  const toLinear = (v: number) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
  return 0.2126 * toLinear(r) + 0.7152 * toLinear(g) + 0.0722 * toLinear(b);
}

function contrastRatio(a: [number, number, number], b: [number, number, number]): number {
  const [l1, l2] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x);
  return (l1 + 0.05) / (l2 + 0.05);
}

describe("primary accent token", () => {
  it.each([
    [":root", "light"],
    [".dark", "dark"],
  ])("renders a navy blue in %s mode, not the previous pink", (selector) => {
    const block = readBlock(selector);
    const primary = readOklch(block, "--primary");

    expect(primary.h).toBeGreaterThan(200);
    expect(primary.h).toBeLessThan(300);
  });

  it.each([
    [":root", "light"],
    [".dark", "dark"],
  ])("meets WCAG AA contrast (>= 4.5:1) for chat-bubble text on the primary background in %s mode", (selector) => {
    const block = readBlock(selector);
    const primary = oklchToSrgb(readOklch(block, "--primary"));
    const primaryForeground = oklchToSrgb(readOklch(block, "--primary-foreground"));

    expect(contrastRatio(primary, primaryForeground)).toBeGreaterThanOrEqual(4.5);
  });

  it("keeps the light-mode primary identical to the fixed brand-mark used by the header logo box", () => {
    const root = readBlock(":root");
    const primary = readOklch(root, "--primary");
    const brandMark = oklchToSrgb({ l: 0.264, c: 0.079, h: 268.5 });
    const primaryRgb = oklchToSrgb(primary);

    for (let i = 0; i < 3; i++) {
      expect(primaryRgb[i]).toBeCloseTo(brandMark[i], 2);
    }
  });

  it("leaves destructive, success and warning tokens untouched", () => {
    const root = readBlock(":root");
    const dark = readBlock(".dark");

    expect(readOklch(root, "--destructive")).toEqual({ l: 0.577, c: 0.22, h: 27 });
    expect(readOklch(root, "--success")).toEqual({ l: 0.55, c: 0.15, h: 145 });
    expect(readOklch(root, "--warning")).toEqual({ l: 0.75, c: 0.15, h: 85 });
    expect(readOklch(dark, "--destructive")).toEqual({ l: 0.55, c: 0.2, h: 27 });
    expect(readOklch(dark, "--success")).toEqual({ l: 0.65, c: 0.17, h: 145 });
    expect(readOklch(dark, "--warning")).toEqual({ l: 0.78, c: 0.14, h: 85 });
  });
});
