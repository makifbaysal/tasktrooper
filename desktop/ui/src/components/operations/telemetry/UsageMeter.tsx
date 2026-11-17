import { Progress } from "@/components/ui/progress";
import { usagePercent, usageTone } from "@/lib/telemetryViews";

interface UsageMeterProps {
  used: number;
  limit: number;
  label: string;
  /** The text under the bar, e.g. "1.2 of 4 cores". */
  caption?: string;
}

/** Used against a limit: green, amber from 75%, red from 90%. */
export function UsageMeter({ used, limit, label, caption }: UsageMeterProps) {
  const pct = usagePercent(used, limit);
  return (
    <div className="min-w-24">
      <Progress value={pct} tone={usageTone(pct)} label={label} className="h-1.5" />
      {caption && <span className="mt-1 block font-mono text-micro text-muted-foreground">{caption}</span>}
    </div>
  );
}
