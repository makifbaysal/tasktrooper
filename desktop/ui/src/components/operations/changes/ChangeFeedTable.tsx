import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import type { Release } from "@/api";
import {
  changeDurationMs,
  changeStartedAt,
  changeTrigger,
  daysAgo,
  isMovingRelease,
  type ChangeDay,
  type ChangeOwner,
} from "@/components/operations/changes/changeFeed";
import { ImpactChips } from "@/components/operations/changes/ImpactChips";
import { ImpactVerdictPill } from "@/components/operations/changes/ImpactVerdictPill";
import { impactSummary, listImpact, type SignalCatalog } from "@/components/operations/changes/releaseImpact";
import { ReleaseStatusPill } from "@/components/operations/changes/ReleaseStatusPill";
import { entityPath } from "@/components/operations/console/opsEntities";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { Card } from "@/components/ui/card";
import { Elapsed } from "@/components/ui/elapsed";
import { HelpTooltip } from "@/components/ui/help-tooltip";
import { useSignalCatalog } from "@/hooks/useDataSources";
import { useI18n } from "@/hooks/useI18n";
import { githubCommitUrl } from "@/lib/githubUrl";
import { intlLocale } from "@/lib/languages";
import { cn, formatDate, formatDuration } from "@/lib/utils";

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-2 align-middle";
const MAX_TASKS = 2;

interface ChangeFeedTableProps {
  days: ChangeDay[];
  ownerOf: (release: Release) => ChangeOwner;
  detailHref: (release: Release) => string;
  selectedId?: string | null;
  showEntity?: boolean;
  now: number;
}

/** The change-tracking feed: one row per release, grouped by local day,
 * newest first. A row opens its change in the detail panel. */
export function ChangeFeedTable({ days, ownerOf, detailHref, selectedId, showEntity = true, now }: ChangeFeedTableProps) {
  const { t, lang } = useI18n();
  const c = (key: string, params?: Record<string, string | number>) => t(`operations.changes.${key}`, params);
  const columnCount = showEntity ? 9 : 8;
  const dayFormat = new Intl.DateTimeFormat(intlLocale(lang), { weekday: "short", month: "short", day: "numeric" });
  const catalog = useSignalCatalog();

  const dayTitle = (start: number) => {
    const ago = daysAgo(start, now);
    if (ago === 0) return c("today");
    if (ago === 1) return c("yesterday");
    return dayFormat.format(start);
  };

  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[960px] border-collapse text-caption" aria-label={c("listLabel")}>
          <thead className="bg-muted/40">
            <tr className="border-b border-border">
              <th scope="col" className={TH}>{c("columns.status")}</th>
              {showEntity && <th scope="col" className={TH}>{c("columns.entity")}</th>}
              <th scope="col" className={TH}>{c("columns.version")}</th>
              <th scope="col" className={TH}>{c("columns.commit")}</th>
              <th scope="col" className={TH}>{c("columns.tasks")}</th>
              <th scope="col" className={TH}>{c("columns.trigger")}</th>
              <th scope="col" className={cn(TH, "text-right")}>{c("columns.started")}</th>
              <th scope="col" className={cn(TH, "text-right")}>{c("columns.duration")}</th>
              <th scope="col" className={TH}>
                <span className="inline-flex items-center gap-1">
                  {c("columns.impact")}
                  <HelpTooltip text={c("impactHelp")} className="normal-case" />
                </span>
              </th>
            </tr>
          </thead>
          {days.map((day) => (
            <tbody key={day.key}>
              <tr className="border-b border-border bg-surface-sunken/60">
                <th scope="colgroup" colSpan={columnCount} className="px-3 py-1.5 text-left text-caption font-semibold">
                  {dayTitle(day.start)}
                  {daysAgo(day.start, now) <= 1 && (
                    <span className="ml-2 font-normal text-muted-foreground">{dayFormat.format(day.start)}</span>
                  )}
                  <span className="ml-2 font-mono font-normal text-muted-foreground tabular-nums">{day.releases.length}</span>
                </th>
              </tr>
              {day.releases.map((release) => (
                <ChangeRow
                  key={release.id}
                  release={release}
                  owner={ownerOf(release)}
                  href={detailHref(release)}
                  selected={release.id === selectedId}
                  showEntity={showEntity}
                  catalog={catalog}
                />
              ))}
            </tbody>
          ))}
        </table>
      </div>
    </Card>
  );
}

interface ChangeRowProps {
  release: Release;
  owner: ChangeOwner;
  href: string;
  selected: boolean;
  showEntity: boolean;
  catalog: SignalCatalog;
}

function ChangeRow({ release: r, owner, href, selected, showEntity, catalog }: ChangeRowProps) {
  const { t, lang } = useI18n();
  const navigate = useNavigate();
  const opsHref = useOpsHref();
  const c = (key: string, params?: Record<string, string | number>) => t(`operations.changes.${key}`, params);
  const started = changeStartedAt(r);
  const duration = changeDurationMs(r);
  const sha = r.commit_sha;
  const commitUrl = githubCommitUrl(owner.repository?.remoteUrl, sha);
  const tasks = r.tasks ?? [];
  const label = r.version || sha?.slice(0, 7) || r.id.slice(0, 8);
  const startedAt = Date.parse(started);
  const time = Number.isFinite(startedAt)
    ? new Intl.DateTimeFormat(intlLocale(lang), { hour: "2-digit", minute: "2-digit" }).format(startedAt)
    : "—";

  return (
    <tr
      data-selected={selected || undefined}
      className={cn(
        "cursor-pointer border-b border-border/60 last:border-0 hover:bg-muted/40",
        selected && "bg-info/5 hover:bg-info/10",
      )}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("a, button")) return;
        navigate(href);
      }}
    >
      <td className={TD}>
        <ReleaseStatusPill status={r.status} />
      </td>
      {showEntity && (
        <td className={cn(TD, "max-w-56")}>
          {owner.entity ? (
            <Link to={opsHref(entityPath(owner.entity.id))} className="block truncate font-medium hover:underline">
              {owner.name}
            </Link>
          ) : (
            <span className="block truncate font-medium">{owner.name || c("unknownRepository")}</span>
          )}
          {owner.repositoryName && owner.repositoryName !== owner.name && (
            <span className="block truncate text-micro text-muted-foreground">{owner.repositoryName}</span>
          )}
        </td>
      )}
      <td className={cn(TD, "max-w-48")}>
        <Link
          to={href}
          aria-label={c("openRelease", { version: label })}
          className="block truncate font-mono font-medium hover:underline focus-visible:underline focus-visible:outline-none"
        >
          {label}
        </Link>
        {r.tag && r.tag !== r.version && <span className="block truncate font-mono text-micro text-muted-foreground">{r.tag}</span>}
      </td>
      <td className={cn(TD, "whitespace-nowrap font-mono")}>
        {sha ? (
          commitUrl ? (
            <a href={commitUrl} target="_blank" rel="noreferrer" className="text-info hover:underline" title={sha} aria-label={c("commitLink", { sha: sha.slice(0, 7) })}>
              {sha.slice(0, 7)}
            </a>
          ) : (
            <span title={sha}>{sha.slice(0, 7)}</span>
          )
        ) : (
          <span className="text-muted-foreground" aria-label={c("noCommit")}>
            —
          </span>
        )}
      </td>
      <td className={cn(TD, "whitespace-nowrap")}>
        {tasks.length === 0 ? (
          <span className="text-muted-foreground">—</span>
        ) : (
          <span className="inline-flex items-center gap-1.5">
            {tasks.slice(0, MAX_TASKS).map((task) => (
              <Link
                key={task.id}
                to={`/board?task=${encodeURIComponent(task.id)}`}
                title={task.title}
                className="font-mono text-info hover:underline"
              >
                {task.key ?? task.id.slice(0, 8)}
              </Link>
            ))}
            {tasks.length > MAX_TASKS && (
              <span className="font-mono text-muted-foreground" title={tasks.slice(MAX_TASKS).map((x) => x.key ?? x.title).join(", ")}>
                {c("moreTasks", { count: tasks.length - MAX_TASKS })}
              </span>
            )}
          </span>
        )}
      </td>
      <td className={cn(TD, "whitespace-nowrap")}>
        <span className="block">{c(`trigger.${changeTrigger(r)}`)}</span>
        {r.executor && <span className="block text-micro text-muted-foreground">{t(`release.executors.${r.executor}`)}</span>}
      </td>
      <td className={cn(TD, "text-right font-mono whitespace-nowrap tabular-nums")} title={formatDate(started)}>
        {time}
      </td>
      <td className={cn(TD, "text-right font-mono whitespace-nowrap tabular-nums")}>
        {duration !== undefined ? (
          formatDuration(duration, lang)
        ) : isMovingRelease(r) ? (
          <Elapsed since={startedAt} format={(ms) => formatDuration(ms, lang)} className="text-info" />
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
      <td className={TD}>
        <ImpactCell release={r} catalog={catalog} />
      </td>
    </tr>
  );
}

/** The deploy's impact verdict when its soak checked one, then what the
 * verification itself recorded. */
function ImpactCell({ release, catalog }: { release: Release; catalog: SignalCatalog }) {
  const { t, lang } = useI18n();
  const ctx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const impact = listImpact(release, catalog);
  return (
    <div className="flex flex-wrap items-center gap-1">
      {impact && <ImpactVerdictPill state={impact.state} detail={impact.worst ? impactSummary(impact.worst, ctx, catalog) : undefined} />}
      <ImpactChips checks={release.checks} status={release.status} />
    </div>
  );
}
