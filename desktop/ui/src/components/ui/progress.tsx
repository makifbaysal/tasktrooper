import { cn } from "@/lib/utils";

export type ProgressTone = "default" | "success" | "warning" | "destructive";

const TONE: Record<ProgressTone, string> = {
  default: "bg-primary",
  success: "bg-success",
  warning: "bg-warning",
  destructive: "bg-destructive",
};

interface ProgressProps {
  /** 0-100. Clamped, so a caller's easing math never has to clamp itself. */
  value: number;
  className?: string;
  tone?: ProgressTone;
  /** The bar's accessible name when no visible label names it. */
  label?: string;
}

export function Progress({ value, className, tone = "default", label }: ProgressProps) {
  const pct = Math.min(100, Math.max(0, value));
  return (
    <div
      role="progressbar"
      aria-valuenow={Math.round(pct)}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={label}
      className={cn("h-2 w-full overflow-hidden rounded-full bg-surface-sunken", className)}
    >
      <div
        className={cn("h-full rounded-full transition-[width] duration-300 ease-out", TONE[tone])}
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}
