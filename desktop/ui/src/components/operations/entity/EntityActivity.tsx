import { History, Rocket, Siren } from "lucide-react";
import { Link } from "react-router-dom";
import type { Incident, Release } from "@/api";
import { releaseLabel, releaseTime } from "@/components/operations/console/opsEntities";
import { isOpenIncident } from "@/components/operations/console/opsStatus";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

export type ActivityItem =
  | { kind: "change"; at: number; release: Release }
  | { kind: "issue"; at: number; incident: Incident };

// An issue sits where it was first seen when that is in the window, else where
// it was last seen: an incident still firing from before the window shows up.
export function buildActivity(releases: Release[], incidents: Incident[], from: number, to: number, limit = 20): ActivityItem[] {
  const inWindow = (at: number) => Number.isFinite(at) && at >= from && at <= to;
  const items: ActivityItem[] = [];
  for (const release of releases) {
    const at = Date.parse(releaseTime(release));
    if (inWindow(at)) items.push({ kind: "change", at, release });
  }
  for (const incident of incidents) {
    const first = Date.parse(incident.first_seen_at);
    const last = Date.parse(incident.last_seen_at);
    const at = inWindow(first) ? first : inWindow(last) ? last : NaN;
    if (Number.isFinite(at)) items.push({ kind: "issue", at, incident });
  }
  return items.sort((a, b) => b.at - a.at).slice(0, limit);
}

interface EntityActivityProps {
  items: ActivityItem[];
  loading?: boolean;
  changeHref: (release: Release) => string;
  issueHref: (incident: Incident) => string;
  changesHref: string;
  issuesHref: string;
  className?: string;
}

/** The entity's right-hand column: deploys and issues in the window, newest
 * first, each opening its own sub-page. */
export function EntityActivity({ items, loading = false, changeHref, issueHref, changesHref, issuesHref, className }: EntityActivityProps) {
  const { t, lang } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.entity.activity.${key}`, params);

  return (
    <Card className={cn("flex min-w-0 flex-col p-3", className)}>
      <div className="flex items-center justify-between gap-2 pb-2">
        <h2 className="text-caption font-semibold">{a("title")}</h2>
        <History className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
      </div>
      {loading && items.length === 0 ? (
        <div className="space-y-2">
          <Skeleton className="h-9" />
          <Skeleton className="h-9" />
          <Skeleton className="h-9" />
        </div>
      ) : items.length === 0 ? (
        <p className="py-6 text-center text-caption text-muted-foreground">{a("empty")}</p>
      ) : (
        <ol className="-mx-1 space-y-0.5" aria-label={a("title")}>
          {items.map((item) => {
            const time = new Date(item.at).toISOString();
            if (item.kind === "change") {
              const r = item.release;
              return (
                <li key={`c:${r.id}`}>
                  <Link to={changeHref(r)} className="flex gap-2 rounded-md px-1 py-1.5 hover:bg-muted/50">
                    <Rocket className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-caption">
                        {a("deploy")} <span className="font-mono">{releaseLabel(r)}</span>
                      </span>
                      <span className="block truncate text-micro text-muted-foreground" title={formatDate(time)}>
                        {t(`release.statuses.${r.status}`)} · {formatRelativeTime(time, lang)}
                      </span>
                    </span>
                  </Link>
                </li>
              );
            }
            const i = item.incident;
            const open = isOpenIncident(i);
            return (
              <li key={`i:${i.id}`}>
                <Link to={issueHref(i)} className="flex gap-2 rounded-md px-1 py-1.5 hover:bg-muted/50">
                  <Siren className={cn("mt-0.5 h-3.5 w-3.5 shrink-0", open ? "text-destructive" : "text-muted-foreground")} aria-hidden />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-caption">{i.title}</span>
                    <span className="block truncate text-micro text-muted-foreground" title={formatDate(time)}>
                      {t(`operations.issue.severity.${i.severity}`)} · {t(`operations.issue.status.${i.status}`)} · {formatRelativeTime(time, lang)}
                    </span>
                  </span>
                </Link>
              </li>
            );
          })}
        </ol>
      )}
      <div className="mt-auto flex flex-wrap gap-x-4 gap-y-1 border-t border-border pt-2 text-micro">
        <Link to={changesHref} className="font-medium text-info hover:underline">
          {a("allChanges")}
        </Link>
        <Link to={issuesHref} className="font-medium text-info hover:underline">
          {a("allIssues")}
        </Link>
      </div>
    </Card>
  );
}
