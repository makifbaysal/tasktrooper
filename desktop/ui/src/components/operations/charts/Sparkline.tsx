import { useMemo } from "react";
import { seriesColorIndex } from "@/components/operations/charts/chartData";
import { cn } from "@/lib/utils";

export interface SparklineProps {
  points: Array<[number, number | null]>;
  colorIndex?: number;
  width?: number;
  height?: number;
  ariaLabel?: string;
  className?: string;
}

export interface SparklineGeometry {
  segments: string[];
  last?: { x: number; y: number };
}

const PAD = 3;

export function sparklineGeometry(points: Array<[number, number | null]>, width: number, height: number): SparklineGeometry {
  const valid = points.filter(([x]) => Number.isFinite(x)).sort((a, b) => a[0] - b[0]);
  const values = valid.map(([, y]) => y).filter((y): y is number => y !== null && Number.isFinite(y));
  if (values.length === 0) return { segments: [] };
  const x0 = valid[0][0];
  const x1 = valid[valid.length - 1][0];
  const lo = Math.min(...values);
  const hi = Math.max(...values);
  const sx = (x: number) => (x1 === x0 ? width / 2 : PAD + ((x - x0) / (x1 - x0)) * (width - 2 * PAD));
  const sy = (y: number) => (hi === lo ? height / 2 : height - PAD - ((y - lo) / (hi - lo)) * (height - 2 * PAD));
  const segments: string[] = [];
  let current: string[] = [];
  let last: { x: number; y: number } | undefined;
  for (const [x, y] of valid) {
    if (y === null || !Number.isFinite(y)) {
      if (current.length) segments.push(current.join(" "));
      current = [];
      continue;
    }
    const px = Math.round(sx(x) * 10) / 10;
    const py = Math.round(sy(y) * 10) / 10;
    current.push(`${current.length ? "L" : "M"}${px} ${py}`);
    last = { x: px, y: py };
  }
  if (current.length) segments.push(current.join(" "));
  return { segments, last };
}

export function Sparkline({ points, colorIndex, width = 96, height = 24, ariaLabel, className }: SparklineProps) {
  const geometry = useMemo(() => sparklineGeometry(points, width, height), [points, width, height]);
  const stroke = `var(--chart-${seriesColorIndex({ colorIndex }, 0)})`;
  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className={cn("shrink-0 overflow-visible", className)}
      role={ariaLabel ? "img" : undefined}
      aria-label={ariaLabel}
      aria-hidden={ariaLabel ? undefined : true}
    >
      {geometry.segments.map((d, i) => (
        <path key={i} d={d} fill="none" stroke={stroke} strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" />
      ))}
      {geometry.last && <circle cx={geometry.last.x} cy={geometry.last.y} r={2.5} fill={stroke} />}
    </svg>
  );
}
