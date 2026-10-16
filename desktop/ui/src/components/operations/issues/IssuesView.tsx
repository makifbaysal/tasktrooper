import { Search, SearchX, ShieldCheck } from "lucide-react";
import { useCallback, useId, useMemo, useRef, type ReactNode } from "react";
import { useLocation, useSearchParams } from "react-router-dom";
import type { Incident, IncidentSeverity, IncidentStatus } from "@/api";
import { FacetChips, type FacetChip } from "@/components/operations/console/FacetChips";
import { ALL } from "@/components/operations/console/opsEntities";
import { incidentEnvironment, isOpenIncident, OPEN_INCIDENT_STATUSES } from "@/components/operations/console/opsStatus";
import { IssueDetailPanel } from "@/components/operations/issues/IssueDetailPanel";
import { ISSUE_SEVERITY_META } from "@/components/operations/issues/IssueSeverity";
import {
  CLOSED_ISSUE_STATUSES,
  ISSUE_SEVERITIES,
  entitiesForIncident,
  groupEntitiesByRepository,
  isIssueSeverity,
  isIssueStatus,
  issueInWindow,
  issueMatches,
  sortIssues,
  type IssueFilter,
} from "@/components/operations/issues/issueList";
import { IssueTable, type IssueOwner } from "@/components/operations/issues/IssueTable";
import { useOpsContext } from "@/components/operations/shell/OpsContext";
import { useOpsDirectory, useOpsEntities, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { INCIDENT_LIST_LIMIT } from "@/hooks/useOpsIssues";

export const CLOSED_PARAM = "closed";

/** `?closed=1`: resolved and ignored issues are listed too. */
export function useIncludeClosed(): [boolean, (on: boolean) => void] {
  const [params, setParams] = useSearchParams();
  const on = params.get(CLOSED_PARAM) === "1";
  const set = useCallback(
    (next: boolean) =>
      setParams(
        (prev) => {
          const out = new URLSearchParams(prev);
          if (next) out.set(CLOSED_PARAM, "1");
          else {
            out.delete(CLOSED_PARAM);
            const status = out.get("status");
            if (status && CLOSED_ISSUE_STATUSES.some((s) => s === status)) out.delete("status");
          }
          return out;
        },
        { replace: true },
      ),
    [setParams],
  );
  return [on, set];
}

interface IssuesViewProps {
  /** Already in the picked environment and scope, any status. */
  incidents: Incident[];
  /** The scope's incidents whose label no environment answers to: listed on
   * their own, whatever environment is picked, so nothing live is hidden. */
  otherIncidents?: Incident[];
  /** The read came back full: the least recently seen are not listed. */
  truncated?: boolean;
  loading: boolean;
  error?: string;
  onChanged: () => void;
  entity?: OpsEntity;
  showEntity?: boolean;
  toolbarStart?: ReactNode;
  /** Filter chips the page shows above the list (a repository filter). */
  scopeNote?: ReactNode;
}

const NO_INCIDENTS: Incident[] = [];

/** New Relic's Issues: the live incidents first and worst, filters in the
 * URL, the issue itself in a panel (`?id=`, where notifications link). */
export function IssuesView({
  incidents,
  otherIncidents = NO_INCIDENTS,
  truncated = false,
  loading,
  error,
  onChanged,
  entity,
  showEntity = true,
  toolbarStart,
  scopeNote,
}: IssuesViewProps) {
  const { t } = useI18n();
  const i = (key: string, params?: Record<string, string | number>) => t(`operations.issues.${key}`, params);
  const { env, range, setEnv } = useOpsContext();
  const [params, setParams] = useSearchParams();
  const { pathname } = useLocation();
  const { entities } = useOpsEntities();
  const { repositories } = useOpsDirectory();
  const [includeClosed, setIncludeClosed] = useIncludeClosed();
  const switchId = useId();
  const otherHeadingId = useId();

  const severityParam = params.get("severity");
  const statusParam = params.get("status");
  const filter: IssueFilter = {
    severity: isIssueSeverity(severityParam) ? severityParam : ALL,
    status: isIssueStatus(statusParam) && (includeClosed || OPEN_INCIDENT_STATUSES.includes(statusParam)) ? statusParam : ALL,
    query: params.get("q") ?? "",
  };
  const selectedId = params.get("id");
  const envLabel = t(`cloud.environments.${env}`);

  const setParam = useCallback(
    (key: string, value: string | null) =>
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value === null || value === "" || value === ALL) next.delete(key);
          else next.set(key, value);
          return next;
        },
        { replace: true },
      ),
    [setParams],
  );

  const ownerOf = useMemo(() => {
    const byRepository = groupEntitiesByRepository(entities);
    if (entity && !(byRepository.get(entity.repositoryId) ?? []).some((e) => e.id === entity.id)) {
      byRepository.set(entity.repositoryId, [...(byRepository.get(entity.repositoryId) ?? []), entity]);
    }
    const names = new Map(repositories.map((r) => [r.id, r.name]));
    return (incident: Incident): IssueOwner => ({
      entities: entity ? [entity] : entitiesForIncident(incident, byRepository),
      repositoryName: names.get(incident.repository_id),
    });
  }, [entities, entity, repositories]);

  const now = Date.now();
  const repoName = (x: Incident) => ownerOf(x).repositoryName;
  const narrow = (list: Incident[]) => {
    const inScope = list.filter((x) => (includeClosed ? issueInWindow(x, range, now) : isOpenIncident(x)));
    const withoutSeverity = inScope.filter((x) => issueMatches(x, { ...filter, severity: ALL }, repoName(x)));
    const visible = sortIssues(withoutSeverity.filter((x) => filter.severity === ALL || x.severity === filter.severity));
    return { inScope, withoutSeverity, visible };
  };
  const { inScope, withoutSeverity: ownWithoutSeverity, visible } = narrow(incidents);
  const other = narrow(otherIncidents);
  const withoutSeverity = [...ownWithoutSeverity, ...other.withoutSeverity];

  // A notification's `?id=` link names no environment: once the issue is
  // read, the console follows it there, once per issue, so it is listed too.
  const followedId = useRef<string | null>(null);
  const followIncident = useCallback(
    (x: Incident) => {
      if (followedId.current === x.id) return;
      followedId.current = x.id;
      const target = incidentEnvironment(x.env);
      if (target && target !== env) setEnv(target);
    },
    [env, setEnv],
  );

  const severityFacets: FacetChip<IncidentSeverity | typeof ALL>[] = [
    { value: ALL, label: i("allSeverities"), count: withoutSeverity.length },
    ...ISSUE_SEVERITIES.map((s) => ({
      value: s,
      label: t(`operations.issue.severity.${s}`),
      count: withoutSeverity.filter((x) => x.severity === s).length,
      icon: ISSUE_SEVERITY_META[s].icon,
      iconClassName: ISSUE_SEVERITY_META[s].className,
    })),
  ];
  const statuses: IncidentStatus[] = includeClosed ? [...OPEN_INCIDENT_STATUSES, ...CLOSED_ISSUE_STATUSES] : OPEN_INCIDENT_STATUSES;

  const detailHref = useCallback(
    (x: Incident) => {
      const next = new URLSearchParams(params);
      next.set("id", x.id);
      return `${pathname}?${next.toString()}`;
    },
    [params, pathname],
  );

  const filtered = filter.severity !== ALL || filter.status !== ALL || filter.query.trim() !== "";
  const clearFilters = () =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const key of ["severity", "status", "q"]) next.delete(key);
        return next;
      },
      { replace: true },
    );

  let body: ReactNode;
  if (loading && incidents.length === 0 && otherIncidents.length === 0) {
    body = <Skeleton className="h-72 w-full" />;
  } else if (inScope.length === 0) {
    const key = includeClosed ? "emptyClosed" : "empty";
    body = (
      <Card>
        <EmptyState
          icon={ShieldCheck}
          title={entity ? i(`${key}.entityTitle`, { name: entity.name, env: envLabel }) : i(`${key}.title`, { env: envLabel })}
          description={i(`${key}.description`)}
        />
      </Card>
    );
  } else if (visible.length === 0 && filtered && other.visible.length === 0) {
    body = (
      <Card>
        <EmptyState
          icon={SearchX}
          variant="search"
          title={i("noMatch.title")}
          action={
            <Button variant="outline" size="sm" onClick={clearFilters}>
              {i("noMatch.action")}
            </Button>
          }
        />
      </Card>
    );
  } else if (visible.length > 0) {
    body = <IssueTable incidents={visible} ownerOf={ownerOf} detailHref={detailHref} selectedId={selectedId} showEntity={showEntity} now={now} />;
  }

  const listed = selectedId ? (incidents.find((x) => x.id === selectedId) ?? otherIncidents.find((x) => x.id === selectedId)) : undefined;

  return (
    <div className="space-y-4">
      {error && (
        <Notice variant={incidents.length > 0 ? "warning" : "error"} title={i("loadFailed")}>
          {error}
        </Notice>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {toolbarStart}
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input
            type="search"
            value={filter.query}
            onChange={(e) => setParam("q", e.target.value)}
            placeholder={i("searchPlaceholder")}
            aria-label={i("searchLabel")}
            className="h-8 pl-8 text-caption"
          />
        </div>
        <Select value={filter.status} onValueChange={(v) => setParam("status", v)}>
          <SelectTrigger className="h-8 w-44 text-caption" aria-label={i("status")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{includeClosed ? i("allStatuses") : i("allLiveStatuses")}</SelectItem>
            {statuses.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`operations.issue.status.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="flex items-center gap-2">
          <Switch id={switchId} checked={includeClosed} onCheckedChange={setIncludeClosed} />
          <Label htmlFor={switchId} className="cursor-pointer text-caption font-normal text-muted-foreground">
            {i("includeClosed")}
          </Label>
        </div>
        <span className="ml-auto font-mono text-caption text-muted-foreground tabular-nums">
          {i("count", { count: visible.length + other.visible.length })}
        </span>
      </div>

      {scopeNote}

      {truncated && (
        <Notice variant="info" title={i("truncated.title", { count: INCIDENT_LIST_LIMIT })}>
          {i("truncated.body")}
        </Notice>
      )}

      <FacetChips label={i("severityFilter")} items={severityFacets} active={filter.severity} onSelect={(v) => setParam("severity", v)} />

      {body}

      {other.visible.length > 0 && (
        <section aria-labelledby={otherHeadingId} className="space-y-2">
          <div className="space-y-0.5">
            <h2 id={otherHeadingId} className="text-body font-semibold">
              {i("other.title", { count: other.visible.length })}
            </h2>
            <p className="text-caption text-muted-foreground">{i("other.description")}</p>
          </div>
          <IssueTable
            incidents={other.visible}
            ownerOf={ownerOf}
            detailHref={detailHref}
            selectedId={selectedId}
            showEntity={showEntity}
            now={now}
            label={i("other.listLabel")}
          />
        </section>
      )}

      <IssueDetailPanel
        incidentId={selectedId}
        listed={listed}
        ownerOf={ownerOf}
        onOpenChange={(open) => {
          if (!open) setParam("id", null);
        }}
        onChanged={onChanged}
        onLoaded={followIncident}
      />
    </div>
  );
}
