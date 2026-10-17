import { Link, useNavigate } from "react-router-dom";
import type { Incident } from "@/api";
import { entityPath, issuesPath } from "@/components/operations/console/opsEntities";
import { incidentEnvironment, isOpenIncident } from "@/components/operations/console/opsStatus";
import { IssueSeverity, IssueStatusBadge } from "@/components/operations/issues/IssueSeverity";
import { issueDurationMs } from "@/components/operations/issues/issueList";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Elapsed } from "@/components/ui/elapsed";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate, formatDuration, formatRelativeTime } from "@/lib/utils";

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-2 align-middle";

type Translate = (key: string, params?: Record<string, string | number>) => string;

/** An incident's environment in the console's words, or its own label when
 * no environment answers to it. */
export function issueEnvironmentLabel(t: Translate, label: string): string {
  const env = incidentEnvironment(label);
  return env ? t(`cloud.environments.${env}`) : label;
}

export interface IssueOwner {
  entities: OpsEntity[];
  repositoryName?: string;
}

interface IssueTableProps {
  incidents: Incident[];
  ownerOf: (incident: Incident) => IssueOwner;
  detailHref: (incident: Incident) => string;
  selectedId?: string | null;
  showEntity?: boolean;
  now: number;
  /** The table's accessible name; the Issues list's own by default. */
  label?: string;
}

/** New Relic's issue list: the worst live issue first. A row opens the
 * issue in the detail panel. */
export function IssueTable({ incidents, ownerOf, detailHref, selectedId, showEntity = true, now, label }: IssueTableProps) {
  const { t } = useI18n();
  const i = (key: string) => t(`operations.issues.${key}`);
  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[880px] border-collapse text-caption" aria-label={label ?? i("listLabel")}>
          <thead className="bg-muted/40">
            <tr className="border-b border-border">
              <th scope="col" className={TH}>{i("columns.severity")}</th>
              <th scope="col" className={TH}>{i("columns.title")}</th>
              {showEntity && <th scope="col" className={TH}>{i("columns.entity")}</th>}
              <th scope="col" className={TH}>{i("columns.environment")}</th>
              <th scope="col" className={TH}>{i("columns.status")}</th>
              <th scope="col" className={cn(TH, "text-right")}>{i("columns.opened")}</th>
              <th scope="col" className={cn(TH, "text-right")}>{i("columns.duration")}</th>
              <th scope="col" className={cn(TH, "text-right")}>{i("columns.occurrences")}</th>
            </tr>
          </thead>
          <tbody>
            {incidents.map((incident) => (
              <IssueRow
                key={incident.id}
                incident={incident}
                owner={ownerOf(incident)}
                href={detailHref(incident)}
                selected={incident.id === selectedId}
                showEntity={showEntity}
                now={now}
              />
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

interface IssueRowProps {
  incident: Incident;
  owner: IssueOwner;
  href: string;
  selected: boolean;
  showEntity: boolean;
  now: number;
}

function IssueRow({ incident, owner, href, selected, showEntity, now }: IssueRowProps) {
  const { t, lang } = useI18n();
  const navigate = useNavigate();
  const opsHref = useOpsHref();
  const i = (key: string, params?: Record<string, string | number>) => t(`operations.issues.${key}`, params);
  const open = isOpenIncident(incident);
  const [first, ...rest] = owner.entities;
  const repositoryName = owner.repositoryName || i("unknownRepository");

  return (
    <tr
      data-selected={selected || undefined}
      className={cn(
        "cursor-pointer border-b border-border/60 last:border-0 hover:bg-muted/40",
        selected && "bg-info/5 hover:bg-info/10",
        !open && "text-muted-foreground",
      )}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("a, button")) return;
        navigate(href);
      }}
    >
      <td className={TD}>
        <IssueSeverity severity={incident.severity} />
      </td>
      <td className={cn(TD, "max-w-96")}>
        <Link
          to={href}
          aria-label={i("openIssue", { title: incident.title })}
          className={cn("block truncate font-medium hover:underline focus-visible:underline focus-visible:outline-none", open && "text-foreground")}
        >
          {incident.title}
        </Link>
        <span className="block truncate text-micro text-muted-foreground">{incident.source}</span>
      </td>
      {showEntity && (
        <td className={cn(TD, "max-w-56")}>
          {first ? (
            <span className="flex min-w-0 items-center gap-1.5">
              <Link to={opsHref(entityPath(first.id, "issues"))} className="truncate hover:underline">
                {first.name}
              </Link>
              {rest.length > 0 && (
                <span className="shrink-0 font-mono text-micro text-muted-foreground" title={rest.map((e) => e.name).join(", ")}>
                  {i("moreEntities", { count: rest.length })}
                </span>
              )}
            </span>
          ) : (
            <Link to={opsHref(issuesPath({ repositoryId: incident.repository_id }))} className="block truncate hover:underline">
              {repositoryName}
            </Link>
          )}
          {first && owner.repositoryName && owner.repositoryName !== first.name && (
            <span className="block truncate text-micro text-muted-foreground">{owner.repositoryName}</span>
          )}
        </td>
      )}
      <td className={cn(TD, "whitespace-nowrap")} title={incident.env}>
        {incidentEnvironment(incident.env) ? (
          issueEnvironmentLabel(t, incident.env)
        ) : (
          <Badge variant="outline" className="max-w-40 truncate px-1.5 py-0 font-mono text-micro font-normal">
            {incident.env}
          </Badge>
        )}
      </td>
      <td className={TD}>
        <IssueStatusBadge status={incident.status} />
      </td>
      <td className={cn(TD, "text-right whitespace-nowrap")} title={formatDate(incident.first_seen_at)}>
        {formatRelativeTime(incident.first_seen_at, lang, now)}
      </td>
      <td className={cn(TD, "text-right font-mono whitespace-nowrap tabular-nums")}>
        {open ? (
          <Elapsed since={Date.parse(incident.first_seen_at)} format={(ms) => formatDuration(ms, lang)} />
        ) : (
          formatDuration(issueDurationMs(incident, now), lang)
        )}
      </td>
      <td className={cn(TD, "text-right font-mono tabular-nums")}>{incident.occurrences}</td>
    </tr>
  );
}
