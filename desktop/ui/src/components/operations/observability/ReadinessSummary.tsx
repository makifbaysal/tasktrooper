import { CircleCheck, TriangleAlert } from "lucide-react";
import type { ObservabilityReadiness } from "@/api";
import {
  detectedInstrumentation,
  readinessGaps,
  signalCounts,
} from "@/components/operations/observability/observabilityReadiness";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface ReadinessSummaryProps {
  readiness: ObservabilityReadiness;
  /** At most this many gaps, then "+N more". */
  maxGaps?: number;
  className?: string;
}

/** What a component reports now: its signals bound, the observability
 * libraries found in its code, and each gap in words. */
export function ReadinessSummary({ readiness, maxGaps, className }: ReadinessSummaryProps) {
  const { t } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.observability.readiness.${key}`, params);
  const counts = signalCounts(readiness);
  const found = detectedInstrumentation(readiness);
  const gaps = readinessGaps(readiness, t);
  const shownGaps = maxGaps !== undefined ? gaps.slice(0, maxGaps) : gaps;

  return (
    <div className={cn("space-y-2.5 text-caption", className)}>
      <p className="flex flex-wrap items-center gap-x-3 gap-y-1">
        {counts.goldenTotal > 0 && (
          <span className="tabular-nums">
            {r("golden", { bound: counts.goldenBound, total: counts.goldenTotal })}
          </span>
        )}
        <span className="text-muted-foreground tabular-nums">{r("signals", { bound: counts.bound, total: counts.total })}</span>
        {readiness.provider && (
          <span className="text-muted-foreground">{r("provider", { provider: t(`cloud.providers.${readiness.provider}`) })}</span>
        )}
      </p>
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-muted-foreground">{r("instrumentation")}</span>
        {found.length > 0 ? (
          found.map((k) => (
            <Badge key={k} variant="success" className="px-2 py-0 text-micro">
              {t(`operations.observability.instrumentation.${k}`)}
            </Badge>
          ))
        ) : (
          <span className="text-muted-foreground italic">{r("noInstrumentation")}</span>
        )}
      </div>
      {gaps.length > 0 ? (
        <ul className="space-y-1" aria-label={r("gaps")}>
          {shownGaps.map((gap) => (
            <li key={gap} className="flex items-start gap-1.5">
              <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" aria-hidden />
              <span>{gap}</span>
            </li>
          ))}
          {shownGaps.length < gaps.length && <li className="pl-5 text-muted-foreground">{r("moreGaps", { count: gaps.length - shownGaps.length })}</li>}
        </ul>
      ) : (
        <p className="flex items-center gap-1.5 text-muted-foreground">
          <CircleCheck className="h-3.5 w-3.5 text-success" aria-hidden />
          {r("ready")}
        </p>
      )}
    </div>
  );
}
