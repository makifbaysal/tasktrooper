import { useEffect, useState } from "react";
import { CHART_SLOTS } from "@/components/operations/charts/chartData";
import { keepEqual } from "@/lib/stableState";

export interface ChartTheme {
  series: string[];
  axis: string;
  grid: string;
  surface: string;
  text: string;
  destructive: string;
  fontSans: string;
  fontMono: string;
}

// What a stylesheet-less document (a test, a detached print) falls back to:
// the light palette, so a chart never draws in the canvas default black.
const FALLBACK_SERIES = ["#2a78d6", "#eb6834", "#1baf7a", "#eda100", "#e87ba4", "#008300", "#6250d6", "#e34948"];

function cssVar(style: CSSStyleDeclaration, names: string[], fallback: string): string {
  for (const name of names) {
    const value = style.getPropertyValue(name).trim();
    if (value) return value;
  }
  return fallback;
}

export function readChartTheme(root: Element = document.documentElement): ChartTheme {
  const style = getComputedStyle(root);
  const series = Array.from({ length: CHART_SLOTS }, (_, i) =>
    cssVar(style, [`--color-chart-${i + 1}`, `--chart-${i + 1}`], FALLBACK_SERIES[i]),
  );
  return {
    series,
    axis: cssVar(style, ["--color-muted-foreground", "--muted-foreground"], "#6b7280"),
    grid: cssVar(style, ["--color-border", "--border"], "#e5e7eb"),
    surface: cssVar(style, ["--color-card", "--card"], "#ffffff"),
    text: cssVar(style, ["--color-foreground", "--foreground"], "#111827"),
    destructive: cssVar(style, ["--color-destructive", "--destructive"], "#dc2626"),
    fontSans: cssVar(style, ["--font-sans"], "ui-sans-serif, system-ui, sans-serif"),
    fontMono: cssVar(style, ["--font-mono"], "ui-monospace, monospace"),
  };
}

// The theme lives on <html class="dark">, not in React state every chart can
// read, so this watches the class and re-reads the tokens when it changes.
export function useChartTheme(): ChartTheme {
  const [theme, setTheme] = useState(readChartTheme);
  useEffect(() => {
    const observer = new MutationObserver(() => setTheme((prev) => keepEqual(prev, readChartTheme())));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    return () => observer.disconnect();
  }, []);
  return theme;
}
