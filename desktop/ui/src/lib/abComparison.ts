import type { ABReport } from "@/api";

export type ABRowKey = "act" | "clarity" | "actPct" | "preferred";

export interface ABRow {
  key: ABRowKey;
  unit: "score" | "pct";
  aMean: number;
  bMean: number;
  aMedian?: number;
  bMedian?: number;
  diff: number;
  paired?: { a: number; b: number; same: number };
  effect?: { label: "negligible" | "small" | "medium" | "large"; size: number };
}

export function abRows(ab: ABReport): ABRow[] {
  const [a, b] = ab.sides;
  const metric = (key: "act" | "clarity"): ABRow => {
    const m = ab[key];
    return {
      key,
      unit: "score",
      aMean: m.a.mean,
      bMean: m.b.mean,
      aMedian: m.a.median,
      bMedian: m.b.median,
      diff: m.diff,
      paired: { a: m.a_higher, b: m.b_higher, same: m.same },
      effect: { label: m.effect_label, size: m.effect_size },
    };
  };
  return [
    metric("act"),
    metric("clarity"),
    { key: "actPct", unit: "pct", aMean: a.act_pct, bMean: b.act_pct, diff: b.act_pct - a.act_pct },
    { key: "preferred", unit: "pct", aMean: a.win_pct, bMean: b.win_pct, diff: b.win_pct - a.win_pct },
  ];
}

export function signed(value: number, digits = 1): string {
  const rounded = Number(value.toFixed(digits));
  if (rounded === 0) return (0).toFixed(digits);
  return `${rounded > 0 ? "+" : "−"}${Math.abs(rounded).toFixed(digits)}`;
}

export function positionBiased(ab: ABReport): boolean {
  return Math.abs(ab.first_shown_win_pct - 50) >= 20;
}
