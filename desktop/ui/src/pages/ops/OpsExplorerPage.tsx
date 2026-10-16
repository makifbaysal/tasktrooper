import { Activity, Search, SearchX } from "lucide-react";
import { useId, useMemo } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { entityTypeOf, goldenColumnsFor, isEntityType, type OpsExplorerType } from "@/components/operations/console/entityTypes";
import {
  ALL,
  DEFAULT_EXPLORER_FILTER,
  NO_PROJECT,
  buildExplorerRows,
  filterRows,
  healthCounts,
  issuesPath,
  openIncidentsElsewhere,
  rowMatches,
  typeCounts,
  uniqueIncidents,
  type ExplorerFilter,
} from "@/components/operations/console/opsEntities";
import { OPS_STATUSES, type OpsStatus } from "@/components/operations/console/opsStatus";
import { EntityTable } from "@/components/operations/explorer/EntityTable";
import { orphanStoreAppRows } from "@/components/operations/releases/mobileReleases";
import { HealthSummaryStrip } from "@/components/operations/explorer/HealthSummaryStrip";
import { TypeFacetChips } from "@/components/operations/explorer/TypeFacetChips";
import { useOpsContext, useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useOpsDirectory, useOpsEntities } from "@/components/operations/shell/useOpsEntities";
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
import { goldenWindow, indexGolden } from "@/components/operations/console/goldenMetrics";
import { useAllStoreApps } from "@/hooks/useMobileReleases";
import { useOpsConsole } from "@/hooks/useOpsConsole";
import { OPS_EXPLORER_GOLDEN_POLL_MS, useOpsGolden } from "@/hooks/useOpsSignals";

function isStatus(value: string | null): value is OpsStatus {
  return OPS_STATUSES.some((s) => s === value);
}

interface OpsExplorerPageProps {
  type?: OpsExplorerType;
}

/** "All entities" and the four type lists (Services, Web, Mobile, Workers):
 * one page, New Relic's entity explorer. */
export default function OpsExplorerPage({ type }: OpsExplorerPageProps) {
  const { t } = useI18n();
  const x = (key: string, params?: Record<string, string | number>) => t(`operations.explorer.${key}`, params);
  const { env, range } = useOpsContext();
  const href = useOpsHref();
  const [params, setParams] = useSearchParams();
  const groupId = useId();
  const hasGolden = type ? goldenColumnsFor(type).length > 0 : false;
  // The golden columns read the window; the rest of the list never does.
  useOpsPageScope(hasGolden ? { time: true, compare: false } : { time: false, note: x("timeNote") });

  const { entities, loading: entitiesLoading, error: entitiesError } = useOpsEntities();
  const { repositories, projects } = useOpsDirectory();
  const { incidents, releases } = useOpsConsole();
  const listsStoreApps = !type || type === "mobile";
  const storeApps = useAllStoreApps(listsStoreApps);

  const typeParam = params.get("type");
  const healthParam = params.get("health");
  const filter: ExplorerFilter = {
    ...DEFAULT_EXPLORER_FILTER,
    type: type ?? (isEntityType(typeParam) ? typeParam : ALL),
    health: isStatus(healthParam) ? healthParam : ALL,
    projectId: params.get("project") ?? ALL,
    query: params.get("q") ?? "",
  };
  const groupByType = !type && params.get("group") === "type";

  const setParam = (key: string, value: string | null) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value === null || value === "" || value === ALL) next.delete(key);
        else next.set(key, value);
        return next;
      },
      { replace: true },
    );

  const scoped = useMemo(() => (type ? entities.filter((e) => entityTypeOf(e.role) === type) : entities), [entities, type]);
  const golden = useMemo(() => (type ? goldenColumnsFor(type, scoped.map((e) => e.role)) : []), [type, scoped]);
  const goldenRead = useOpsGolden(env, range, null, hasGolden, OPS_EXPLORER_GOLDEN_POLL_MS);
  const goldenValues = useMemo(() => indexGolden(goldenRead.data), [goldenRead.data]);
  const goldenClamped = hasGolden && goldenWindow(range).clamped;
  const rows = useMemo(() => {
    const built = buildExplorerRows({ entities: scoped, repositories, incidents, releases, environment: env });
    const own = type ? built.filter((r) => r.kind === "entity") : built;
    return listsStoreApps ? [...own, ...orphanStoreAppRows(storeApps, entities, repositories)] : own;
  }, [scoped, repositories, incidents, releases, env, type, listsStoreApps, storeApps, entities]);

  const elsewhere = useMemo(
    () => openIncidentsElsewhere(incidents, type ? new Set(scoped.map((e) => e.repositoryId)) : undefined),
    [incidents, scoped, type],
  );
  const elsewhereLabels = [...new Set(elsewhere.map((i) => i.env))];

  const visible = filterRows(rows, filter);
  const counts = healthCounts(rows, filter);
  const types = typeCounts(rows, filter);
  const openIssues = uniqueIncidents(rows.filter((r) => rowMatches(r, filter, "health"))).length;
  const hasUnassigned = rows.some((r) => r.projectIds.length === 0);
  const envLabel = t(`cloud.environments.${env}`);
  const entityRows = rows.filter((r) => r.kind === "entity");
  const nothingReports = entityRows.length > 0 && entityRows.every((r) => !r.environment);
  const filtered = filter.health !== ALL || filter.query.trim() !== "" || filter.projectId !== ALL || (!type && filter.type !== ALL);

  if (entitiesLoading && entities.length === 0) {
    return (
      <div className="space-y-4" aria-busy>
        <div className="grid grid-cols-2 gap-2 md:grid-cols-5">
          {Array.from({ length: 5 }, (_, i) => (
            <Skeleton key={i} className="h-16" />
          ))}
        </div>
        <Skeleton className="h-9 w-full max-w-xl" />
        <Skeleton className="h-72 w-full" />
      </div>
    );
  }

  if (rows.length === 0 && !filtered) {
    const emptyKey = type ? `emptyType.${type}` : "empty";
    return (
      <div className="space-y-4">
        {entitiesError && (
          <Notice variant="error" title={x("loadFailed")}>
            {entitiesError}
          </Notice>
        )}
        <Card>
          <EmptyState
            icon={Activity}
            title={x(`${emptyKey}.title`)}
            description={x(`${emptyKey}.description`)}
            action={
              <Button variant="outline" size="sm" asChild>
                <Link to="/projects">{x("empty.action")}</Link>
              </Button>
            }
          />
        </Card>
      </div>
    );
  }

  const clearFilters = () =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const key of ["health", "project", "q", "type"]) next.delete(key);
        return next;
      },
      { replace: true },
    );

  return (
    <div className="@container space-y-4">
      {entitiesError && (
        <Notice variant="warning" title={x("loadFailed")}>
          {entitiesError}
        </Notice>
      )}

      <HealthSummaryStrip
        counts={counts}
        active={filter.health}
        onSelect={(status) => setParam("health", status)}
        openIssues={openIssues}
        issuesHref={href(issuesPath())}
        environmentLabel={envLabel}
      />

      {elsewhere.length > 0 && (
        <Notice variant="info" title={x("otherEnvironments.title", { count: elsewhere.length })}>
          <span>
            {x("otherEnvironments.body", {
              labels: elsewhereLabels.slice(0, 3).join(", ") + (elsewhereLabels.length > 3 ? ", …" : ""),
            })}{" "}
          </span>
          <Link to={href(issuesPath())} className="font-medium text-info hover:underline">
            {x("otherEnvironments.action")}
          </Link>
        </Notice>
      )}

      {nothingReports && (
        <Notice variant="info" title={x("notReporting.title", { env: envLabel })}>
          {x("notReporting.body")}
        </Notice>
      )}

      {hasGolden && goldenRead.error && (
        <Notice variant="warning" title={x(goldenRead.data ? "goldenRefreshFailed" : "goldenFailed")}>
          <span className="break-words">{goldenRead.error} </span>
          <Button variant="link" size="sm" className="h-auto p-0" onClick={() => void goldenRead.reload()}>
            {t("operations.chart.retry")}
          </Button>
        </Notice>
      )}

      {goldenClamped && (
        <Notice variant="info" title={t("operations.console.goldenClamped")} />
      )}

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input
            type="search"
            value={filter.query}
            onChange={(e) => setParam("q", e.target.value)}
            placeholder={x("searchPlaceholder")}
            aria-label={x("searchLabel")}
            className="h-8 pl-8 text-caption"
          />
        </div>
        <Select value={filter.projectId} onValueChange={(v) => setParam("project", v)}>
          <SelectTrigger className="h-8 w-48 text-caption" aria-label={x("project")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{x("allProjects")}</SelectItem>
            {projects.map((p) => (
              <SelectItem key={p.id} value={p.id}>
                {p.name}
              </SelectItem>
            ))}
            {(hasUnassigned || filter.projectId === NO_PROJECT) && <SelectItem value={NO_PROJECT}>{x("noProject")}</SelectItem>}
          </SelectContent>
        </Select>
        {!type && (
          <div className="ml-auto flex items-center gap-2">
            <Switch id={groupId} checked={groupByType} onCheckedChange={(on) => setParam("group", on ? "type" : null)} />
            <Label htmlFor={groupId} className="cursor-pointer text-caption font-normal text-muted-foreground">
              {x("groupByType")}
            </Label>
          </div>
        )}
      </div>

      {!type && <TypeFacetChips counts={types} active={filter.type} onSelect={(value) => setParam("type", value)} />}

      {visible.length === 0 ? (
        <Card>
          <EmptyState
            icon={SearchX}
            variant="search"
            title={x("noMatch.title")}
            action={
              <Button variant="outline" size="sm" onClick={clearFilters}>
                {x("noMatch.action")}
              </Button>
            }
          />
        </Card>
      ) : (
        <EntityTable
          rows={visible}
          golden={golden}
          goldenValues={goldenValues}
          goldenRead={goldenRead}
          groupByType={groupByType}
          showType={!type || type === "mobile"}
        />
      )}
    </div>
  );
}
