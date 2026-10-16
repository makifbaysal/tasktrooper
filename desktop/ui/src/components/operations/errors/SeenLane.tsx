import { cn } from "@/lib/utils";

interface SeenLaneProps {
  /** The window, unix ms. */
  from: number;
  to: number;
  firstSeen: string;
  lastSeen: string;
  label: string;
  className?: string;
}

/** Where in the window a group was seen, from its first to its last
 * occurrence. Providers give no per-minute counts, so this is the span, not a
 * rate; a group older than the window runs off its left edge. */
export function SeenLane({ from, to, firstSeen, lastSeen, label, className }: SeenLaneProps) {
  const span = to - from;
  const first = Date.parse(firstSeen);
  const last = Date.parse(lastSeen);
  if (!(span > 0) || !Number.isFinite(first) || !Number.isFinite(last)) return null;
  const clamp = (v: number) => Math.min(1, Math.max(0, v));
  const start = clamp((Math.max(first, from) - from) / span);
  const end = clamp((last - from) / span);
  return (
    <span role="img" aria-label={label} title={label} className={cn("relative block h-1.5 w-20 shrink-0 overflow-hidden rounded-full bg-muted", className)}>
      <span
        className={cn("absolute inset-y-0 rounded-full bg-destructive/70", first < from && "rounded-l-none")}
        style={{ left: `${start * 100}%`, width: `max(3px, ${Math.max(0, end - start) * 100}%)` }}
      />
    </span>
  );
}
