import { GitCommitHorizontal, Search, SearchX } from "lucide-react";
import { useCallback, useMemo, type ReactNode } from "react";
import { useLocation, useSearchParams } from "react-router-dom";
import type { Release } from "@/api";
import { ChangeFeedTable } from "@/components/operations/changes/ChangeFeedTable";
import {
  CHANGE_STATUS_GROUPS,
  changeAt,
  changeMatches,
  changeOwner,
  changeStatusGroup,
  groupByDay,
  inChangeWindow,
  inEnvironment,
  isChangeStatusFilter,
  statusGroupCounts,
  type ChangeStatusFilter,
} from "@/components/operations/changes/changeFeed";
import { RELEASE_STATUS_META } from "@/components/operations/changes/ReleaseStatusPill";
import { ReleaseDetailPanel } from "@/components/operations/changes/ReleaseDetailPanel";
import { FacetChips, type FacetChip } from "@/components/operations/console/FacetChips";
import { ALL, releaseLabel, releasesForEntity } from "@/components/operations/console/opsEntities";
import { DeployTimeline } from "@/components/operations/entity/DeployTimeline";
import { useOpsContext } from "@/components/operations/shell/OpsContext";
import { useOpsDirectory, useOpsEntities, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { CHANGE_MAX_PAGES, CHANGE_PAGE_SIZE, changeScopeKey, useChangeScope, useReleaseFeed, type ChangeScope } from "@/hooks/useOpsChanges";

const GROUP_RELEASE_STATUS = {
  inProgress: "deploying",
  pending: "pending",
  released: "released",
  rolledBack: "rolled_back",
  failed: "failed",
} as const;

interface ChangesViewProps {
  /** Scopes the feed to one entity (its sub-page, or the page's entity filter). */
  entity?: OpsEntity;
  showEntity?: boolean;
  /** Filters the page puts first in the toolbar (the entity picker). */
  toolbarStart?: ReactNode;
}

/** New Relic's change tracking: every deploy in the environment and window,
 * grouped by day, with the change itself in a panel (`?release=`). */
export function ChangesView({ entity, showEntity = true, toolbarStart }: ChangesViewProps) {
  const { scope, soleComponent, ready } = useChangeScope(entity);
  if (!ready) return <ChangesSkeleton />;
  return (
    <ChangesFeed
      key={changeScopeKey(scope)}
      scope={scope}
      soleComponent={soleComponent}
      entity={entity}
      showEntity={showEntity}
      toolbarStart={toolbarStart}
    />
  );
}

function ChangesSkeleton() {
  return (
    <div className="space-y-4" aria-busy>
      <Skeleton className="h-9 w-full max-w-xl" />
      <Skeleton className="h-16 w-full" />
      <Skeleton className="h-72 w-full" />
    </div>
  );
}

interface ChangesFeedProps extends ChangesViewProps {
  scope: ChangeScope;
  soleComponent: boolean;
  showEntity: boolean;
}

function ChangesFeed({ scope, soleComponent, entity, showEntity, toolbarStart }: ChangesFeedProps) {
  const { t } = useI18n();
  const c = (key: string, params?: Record<string, string | number>) => t(`operations.changes.${key}`, params);
  const { env, range } = useOpsContext();
  const [params, setParams] = useSearchParams();
  const { pathname } = useLocation();
  const { entities } = useOpsEntities();
  const { repositories } = useOpsDirectory();
  const feed = useReleaseFeed(scope, { from: range.from, until: range.preset === "custom" ? range.to : undefined });

  const statusParam = params.get("status");
  const status: ChangeStatusFilter = isChangeStatusFilter(statusParam) ? statusParam : ALL;
  const query = params.get("q") ?? "";
  const selectedId = params.get("release");
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
    const entitiesById = new Map(entities.map((e) => [e.id, e]));
    if (entity) entitiesById.set(entity.id, entity);
    const repositoriesById = new Map(repositories.map((r) => [r.id, r]));
    const byRepository = new Map<string, OpsEntity[]>();
    for (const e of entitiesById.values()) byRepository.set(e.repositoryId, [...(byRepository.get(e.repositoryId) ?? []), e]);
    return (r: Release) => changeOwner(r, entitiesById, repositoriesById, byRepository);
  }, [entities, entity, repositories]);

  const scoped = useMemo(() => {
    const base = entity ? releasesForEntity(entity, soleComponent, env, feed.releases) : feed.releases.filter((r) => inEnvironment(r, env));
    return base.filter((r) => inChangeWindow(r, range));
  }, [entity, soleComponent, env, feed.releases, range]);

  const searched = useMemo(() => scoped.filter((r) => changeMatches(r, query, ownerOf(r))), [scoped, query, ownerOf]);
  const visible = useMemo(
    () => (status === ALL ? searched : searched.filter((r) => changeStatusGroup(r.status) === status)),
    [searched, status],
  );
  const days = useMemo(() => groupByDay(visible), [visible]);
  const counts = statusGroupCounts(searched);

  const detailHref = useCallback(
    (r: Release) => {
      const next = new URLSearchParams(params);
      next.set("release", r.id);
      return `${pathname}?${next.toString()}`;
    },
    [params, pathname],
  );

  const markers = useMemo(
    () =>
      scoped
        .filter((r) => r.deployed_at || r.deploy_started_at)
        .map((r) => ({ at: changeAt(r), label: releaseLabel(r), href: detailHref(r) })),
    [scoped, detailHref],
  );

  const facets: FacetChip<ChangeStatusFilter>[] = [
    { value: ALL, label: c("all"), count: searched.length },
    ...CHANGE_STATUS_GROUPS.map((group) => {
      const meta = RELEASE_STATUS_META[GROUP_RELEASE_STATUS[group]];
      return { value: group, label: c(`groups.${group}`), count: counts[group], icon: meta.icon };
    }).filter((f) => f.count > 0 || f.value === status),
  ];

  const clearFilters = () =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("status");
        next.delete("q");
        return next;
      },
      { replace: true },
    );

  const listed = selectedId ? feed.releases.find((r) => r.id === selectedId) : undefined;
  const now = Date.now();

  let body: ReactNode;
  if (feed.loading && feed.releases.length === 0) {
    body = <Skeleton className="h-72 w-full" />;
  } else if (scoped.length === 0) {
    body = (
      <Card>
        <EmptyState
          icon={GitCommitHorizontal}
          title={c("empty.title")}
          description={entity ? c("empty.entityDescription", { name: entity.name, env: envLabel }) : c("empty.description", { env: envLabel })}
        />
      </Card>
    );
  } else if (visible.length === 0) {
    body = (
      <Card>
        <EmptyState
          icon={SearchX}
          variant="search"
          title={c("noMatch.title")}
          action={
            <Button variant="outline" size="sm" onClick={clearFilters}>
              {c("noMatch.action")}
            </Button>
          }
        />
      </Card>
    );
  } else {
    body = (
      <ChangeFeedTable days={days} ownerOf={ownerOf} detailHref={detailHref} selectedId={selectedId} showEntity={showEntity} now={now} />
    );
  }

  return (
    <div className="space-y-4">
      {feed.error && (
        <Notice variant={feed.releases.length > 0 ? "warning" : "error"} title={c("loadFailed")}>
          {feed.error}
        </Notice>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {toolbarStart}
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input
            type="search"
            value={query}
            onChange={(e) => setParam("q", e.target.value)}
            placeholder={c("searchPlaceholder")}
            aria-label={c("searchLabel")}
            className="h-8 pl-8 text-caption"
          />
        </div>
        <span className="ml-auto font-mono text-caption text-muted-foreground tabular-nums">{c("count", { count: visible.length })}</span>
      </div>

      <FacetChips label={c("statusFilter")} items={facets} active={status} onSelect={(value) => setParam("status", value)} />

      {scoped.length > 0 && (
        <Card className="px-4 pt-3 pb-2">
          <DeployTimeline markers={markers} from={range.from} to={range.to} label={c("laneLabel")} />
        </Card>
      )}

      {feed.truncated && (
        <Notice variant="info" title={c("truncated", { count: CHANGE_PAGE_SIZE * CHANGE_MAX_PAGES })} />
      )}

      {body}

      <ReleaseDetailPanel
        releaseId={selectedId}
        listed={listed}
        ownerOf={ownerOf}
        onOpenChange={(open) => {
          if (!open) setParam("release", null);
        }}
        onChanged={feed.apply}
      />
    </div>
  );
}
