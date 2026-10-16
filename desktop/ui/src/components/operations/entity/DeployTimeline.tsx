import { Link } from "react-router-dom";
import type { DeployMarker } from "@/components/operations/charts/TimeSeriesChart";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";

interface DeployTimelineProps {
  markers: DeployMarker[];
  from: number;
  to: number;
  label: string;
}

/** The selected window as one lane with a diamond per deploy: the deploy
 * markers the charts carry, readable before any metric is bound. */
export function DeployTimeline({ markers, from, to, label }: DeployTimelineProps) {
  const { t, lang } = useI18n();
  const span = Math.max(1, to - from);
  const sameDay = new Date(from).toDateString() === new Date(to).toDateString();
  const fmt = new Intl.DateTimeFormat(intlLocale(lang), sameDay ? { hour: "2-digit", minute: "2-digit" } : { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
  const full = new Intl.DateTimeFormat(intlLocale(lang), { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
  const shown = markers.filter((m) => m.at >= from && m.at <= to).sort((a, b) => a.at - b.at);

  return (
    <div className="space-y-1">
      <div className="relative h-8" role="list" aria-label={label}>
        <div aria-hidden className="absolute top-1/2 right-0 left-0 h-px -translate-y-1/2 bg-border" />
        {shown.length === 0 && (
          <p className="absolute inset-0 flex items-center justify-center">
            <span className="bg-card px-2 text-micro text-muted-foreground">{t("operations.entity.summary.noDeploysInRange")}</span>
          </p>
        )}
        {shown.map((m) => {
          const left = `${((m.at - from) / span) * 100}%`;
          const text = t("operations.chart.deployAt", { label: m.label });
          const title = `${text} · ${full.format(m.at)}`;
          const diamond = <span aria-hidden className="block h-2.5 w-2.5 rotate-45 rounded-[2px] bg-muted-foreground transition-colors group-hover:bg-foreground" />;
          return (
            <span key={`${m.at}:${m.label}`} role="listitem" className="absolute top-1/2 -translate-x-1/2 -translate-y-1/2" style={{ left }}>
              {m.href ? (
                <Link to={m.href} title={title} aria-label={title} className="group flex h-6 w-6 items-center justify-center rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">
                  {diamond}
                </Link>
              ) : (
                <span title={title} className="group flex h-6 w-6 items-center justify-center">
                  {diamond}
                  <span className="sr-only">{title}</span>
                </span>
              )}
            </span>
          );
        })}
      </div>
      <div className="flex justify-between font-mono text-micro text-muted-foreground tabular-nums" aria-hidden>
        <span>{fmt.format(from)}</span>
        <span>{fmt.format(to)}</span>
      </div>
    </div>
  );
}
