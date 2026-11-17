import { useState } from "react";
import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { HealthMark } from "@/components/operations/telemetry/HealthMark";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { formatCount, formatRatio } from "@/lib/telemetryHub";
import { mapLayout, type TelemetryServiceMap } from "@/lib/telemetryViews";
import { cn } from "@/lib/utils";

const EDGE_TONE: Record<string, string> = {
  critical: "stroke-destructive",
  warning: "stroke-warning",
};

/** The whole system in columns: who calls, the services, their data stores and
 * the third parties. A service links to its page; hovering a node lights its edges. */
export function ServiceMapGraph({ map, serviceHref }: { map: TelemetryServiceMap; serviceHref: (name: string) => string }) {
  const { t, lang } = useI18n();
  const locale = intlLocale(lang);
  const href = useOpsHref();
  const layout = mapLayout(map);
  const [hover, setHover] = useState<string | null>(null);
  const maxCalls = Math.max(1, ...layout.edges.map((e) => e.callsPerMin));
  const touches = (e: { from: string; to: string }) => hover !== null && (e.from === hover || e.to === hover);

  return (
    <Card className="overflow-x-auto p-4">
      <div className="relative" style={{ width: layout.width, height: layout.height }}>
        {layout.columns.map((c) => (
          <p key={c.tier} className="absolute top-0 text-micro font-medium tracking-wide text-muted-foreground uppercase" style={{ left: c.x }}>
            {t(`telemetryViews.map.tiers.${c.tier}`)}
          </p>
        ))}
        <svg className="pointer-events-none absolute inset-0" width={layout.width} height={layout.height} aria-hidden>
          {layout.edges.map((e) => (
            <path
              key={e.id}
              d={e.path}
              fill="none"
              className={cn(EDGE_TONE[e.status] ?? "stroke-muted-foreground/40", hover !== null && !touches(e) && "opacity-15", touches(e) && "opacity-100")}
              strokeWidth={1 + 3 * Math.sqrt(e.callsPerMin / maxCalls)}
            />
          ))}
        </svg>
        {layout.nodes.map((n) => {
          const body = (
            <>
              <span className="flex min-w-0 items-center gap-1.5">
                <HealthMark health={n.status} />
                <span className="truncate font-medium">{n.name}</span>
              </span>
              <span className="block truncate text-micro text-muted-foreground">
                {[n.system || n.kind, n.rpm > 0 ? t("telemetryViews.map.rpm", { value: formatCount(n.rpm, locale) }) : "", n.errorRate > 0 ? formatRatio(n.errorRate, locale) : ""]
                  .filter(Boolean)
                  .join(" · ")}
              </span>
            </>
          );
          const cls = cn(
            "absolute flex flex-col justify-center rounded-md border bg-card px-2 text-caption shadow-xs",
            n.status === "critical" ? "border-destructive/60" : n.status === "warning" ? "border-warning/60" : "border-border",
            n.incident && n.incident.open > 0 && "ring-1 ring-destructive/40",
          );
          const style = { left: n.x, top: n.y, width: layout.nodeWidth, height: layout.nodeHeight };
          const events = { onMouseEnter: () => setHover(n.id), onMouseLeave: () => setHover(null), onFocus: () => setHover(n.id), onBlur: () => setHover(null) };
          return n.tier === "service" ? (
            <Link key={n.id} to={href(serviceHref(n.name))} className={cn(cls, "hover:bg-muted/40")} style={style} {...events}>
              {body}
            </Link>
          ) : (
            <div key={n.id} className={cls} style={style} {...events}>
              {body}
            </div>
          );
        })}
      </div>
    </Card>
  );
}
