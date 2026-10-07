import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { Rocket } from "lucide-react";
import { toast } from "sonner";
import {
  api,
  type CloudDeployment,
  type DeployEnvironment,
  type MatrixCell,
  type MatrixRepo,
  type MatrixView,
  type ProjectsOverview,
  type Release,
} from "@/api";
import { PageHeader } from "@/components/admin/PageHeader";
import { DeployFeedTable } from "@/components/operations/DeployFeedTable";
import { DeploymentDetailDrawer } from "@/components/operations/DeploymentDetailDrawer";
import { DeploymentMatrix } from "@/components/operations/DeploymentMatrix";
import { LiveEnvironmentCard } from "@/components/operations/LiveEnvironmentCard";
import { ReleaseDrawer } from "@/components/projects/repository/deploy/ReleaseDrawer";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";
import {
  ALL,
  DEFAULT_FILTER,
  NO_PROJECT,
  buildCatalog,
  buildFeed,
  envMatches,
  historyMatches,
  itemMatches,
  liveOf,
  mergeReleasePages,
  repoMatches,
  stateMatches,
  type DeployFeedItem,
  type FeedFilter,
  type StateFilter,
} from "@/lib/deployFeed";

// Releases move on their own (deploy → verify → verdict) while the tab sits
// open and there is no websocket; provider deployments cost a provider call
// per environment, so they refresh less often.
const RELEASE_REFRESH_MS = 15_000;
const PROVIDER_REFRESH_MS = 60_000;
const RELEASE_PAGE = 100;
const PROVIDER_LIMIT = 50;
const HISTORY_PAGE = 25;

const CACHE_OVERVIEW = "operations.deploy.overview";
const CACHE_RELEASES = "operations.deploy.releases";
const CACHE_PROVIDER = "operations.deploy.providerDeployments";
const CACHE_MATRIX = "operations.deployMatrix";
const CACHE_FILTER = "operations.deploy.filter";

const ENVIRONMENTS: (DeployEnvironment | typeof ALL)[] = ["production", "staging", "preview", ALL];
const STATES: StateFilter[] = [ALL, "success", "failed"];

export function DeploymentsPage() {
  const { t } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.deployments.${key}`, params);

  const [overview, setOverview] = useCachedState<ProjectsOverview | null>(CACHE_OVERVIEW, null);
  const [releases, setReleases] = useCachedState<Release[]>(CACHE_RELEASES, []);
  const [provider, setProvider] = useCachedState<Record<string, CloudDeployment[]>>(CACHE_PROVIDER, {});
  const [matrix, setMatrix] = useCachedState<MatrixView | null>(CACHE_MATRIX, null);
  const [filter, setFilter] = useCachedState<FeedFilter>(CACHE_FILTER, DEFAULT_FILTER);
  const [loading, setLoading] = useFirstLoad(CACHE_OVERVIEW, CACHE_RELEASES);
  const [unavailable, setUnavailable] = useState<Record<string, boolean>>({});
  const [hasOlder, setHasOlder] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [visible, setVisible] = useState(HISTORY_PAGE);
  const [openRelease, setOpenRelease] = useState<DeployFeedItem | null>(null);
  const [matrixSelection, setMatrixSelection] = useState<{ repo: MatrixRepo; cell: MatrixCell } | null>(null);
  const failedOnce = useRef(false);

  const loadBase = useCallback(async () => {
    const [ov, rel, mx] = await Promise.allSettled([
      api.getProjectsOverview(),
      api.listAllReleases({ limit: RELEASE_PAGE }),
      api.getDeployMatrix(),
    ]);
    if (ov.status === "fulfilled") setOverview((prev) => keepEqual(prev, ov.value));
    if (rel.status === "fulfilled") {
      const fresh = rel.value.releases ?? [];
      setReleases((prev) => keepEqual(prev, mergeReleasePages(fresh, prev)));
      setHasOlder((more) => more || fresh.length === RELEASE_PAGE);
    }
    if (mx.status === "fulfilled") setMatrix((prev) => keepEqual(prev, mx.value));
    const failure = [ov, rel].find((r) => r.status === "rejected") as PromiseRejectedResult | undefined;
    if (failure && !failedOnce.current) {
      failedOnce.current = true;
      toast.error(failure.reason instanceof Error ? failure.reason.message : t("common.actionFailed"));
    }
    setLoading(false);
  }, [t, setOverview, setReleases, setMatrix, setLoading]);

  usePolling(loadBase, RELEASE_REFRESH_MS, true);

  const catalog = useMemo(() => (overview ? buildCatalog(overview) : null), [overview]);
  const envsInView = useMemo(() => catalog?.envs.filter((e) => envMatches(e, filter)) ?? [], [catalog, filter]);
  const envKey = envsInView.map((e) => e.env.id).join(",");

  const loadProvider = useCallback(async () => {
    if (!envKey) return;
    await Promise.allSettled(
      envKey.split(",").map((id) =>
        api
          .getEnvironmentDeployments(id, { limit: PROVIDER_LIMIT })
          .then((res) => {
            const list = res.deployments ?? [];
            setProvider((prev) => (keepEqual(prev[id], list) === prev[id] ? prev : { ...prev, [id]: list }));
            setUnavailable((prev) => (prev[id] === false ? prev : { ...prev, [id]: false }));
          })
          .catch(() => setUnavailable((prev) => (prev[id] === true ? prev : { ...prev, [id]: true }))),
      ),
    );
  }, [envKey, setProvider]);

  usePolling(loadProvider, PROVIDER_REFRESH_MS, !!envKey);

  // The poll's own first tick covers the first set of environments; a filter
  // change that swaps the set is read at once rather than a minute later.
  const polledEnvKey = useRef(envKey);
  useEffect(() => {
    const previous = polledEnvKey.current;
    polledEnvKey.current = envKey;
    if (previous && envKey && previous !== envKey) void loadProvider();
  }, [envKey, loadProvider]);

  const feed = useMemo(() => (catalog ? buildFeed(catalog, releases, provider) : []), [catalog, releases, provider]);
  const scoped = useMemo(() => feed.filter((i) => itemMatches(i, { ...filter, state: ALL })), [feed, filter]);
  const active = scoped.filter((i) => stateMatches(i.state, "active"));
  const history = scoped.filter((i) => historyMatches(i.state, filter.state));

  const loadOlder = async () => {
    const oldest = releases.at(-1)?.created_at;
    if (!oldest) return;
    setLoadingOlder(true);
    try {
      const res = await api.listAllReleases({ limit: RELEASE_PAGE, before: oldest });
      const older = res.releases ?? [];
      setReleases((prev) => [...prev, ...older.filter((r) => !prev.some((p) => p.id === r.id))]);
      setHasOlder(older.length === RELEASE_PAGE);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setLoadingOlder(false);
    }
  };

  const showMore = () => {
    setVisible((v) => v + HISTORY_PAGE);
    if (visible + HISTORY_PAGE >= history.length && hasOlder) void loadOlder();
  };

  const update = (patch: Partial<FeedFilter>) => {
    setVisible(HISTORY_PAGE);
    setFilter((f) => {
      const next = { ...f, ...patch };
      const repo = catalog?.repos.find((r) => r.id === next.repositoryId);
      if (next.repositoryId !== ALL && (!repo || !repoMatches(repo, { projectId: next.projectId, repositoryId: ALL }))) {
        next.repositoryId = ALL;
      }
      return next;
    });
  };

  const legacyMatrix = matrix?.repos?.some((r) => r.cells.some((c) => c.dispatchable || c.last_run)) ?? false;

  if (loading && !catalog) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-10 w-full max-w-xl" />
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <Skeleton className="h-44" />
          <Skeleton className="h-44" />
        </div>
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  const repoOptions = catalog?.repos.filter((r) => repoMatches(r, { projectId: filter.projectId, repositoryId: ALL })) ?? [];
  const hasUnassigned = catalog?.repos.some((r) => r.projectIds.length === 0) ?? false;
  const shown = history.slice(0, visible);
  const canShowMore = history.length > visible || hasOlder;

  return (
    <div className="space-y-6">
      <PageHeader title={d("title")} description={d("description")} />

      <div className="flex flex-wrap items-center gap-3">
        <Select value={filter.projectId} onValueChange={(v) => update({ projectId: v })}>
          <SelectTrigger className="w-52" aria-label={d("filters.project")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{d("filters.allProjects")}</SelectItem>
            {catalog?.projects.map((p) => (
              <SelectItem key={p.id} value={p.id}>
                {p.name}
              </SelectItem>
            ))}
            {hasUnassigned && <SelectItem value={NO_PROJECT}>{d("filters.noProject")}</SelectItem>}
          </SelectContent>
        </Select>
        <Select value={filter.repositoryId} onValueChange={(v) => update({ repositoryId: v })}>
          <SelectTrigger className="w-52" aria-label={d("filters.repository")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{d("filters.allRepositories")}</SelectItem>
            {repoOptions.map((r) => (
              <SelectItem key={r.id} value={r.id}>
                {r.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Tabs
          value={filter.environment}
          onValueChange={(v) => update({ environment: v as FeedFilter["environment"] })}
          variant="pill"
          className="w-fit"
        >
          <TabsList aria-label={d("filters.environment")}>
            {ENVIRONMENTS.map((env) => (
              <TabsTrigger key={env} value={env}>
                {env === ALL ? d("filters.allEnvironments") : t(`cloud.environments.${env}`)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <section className="space-y-3">
        <h3 className="text-heading font-semibold">{d("live.title")}</h3>
        {envsInView.length === 0 ? (
          <Card>
            <EmptyState
              icon={Rocket}
              title={d("live.emptyTitle")}
              description={d("live.emptyDescription")}
              action={
                <Button variant="outline" size="sm" asChild>
                  <Link to="/projects">{d("live.emptyAction")}</Link>
                </Button>
              }
            />
          </Card>
        ) : (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {envsInView.map((ref) => {
              const { live, inFlight } = liveOf(ref.env.id, feed);
              return (
                <LiveEnvironmentCard
                  key={ref.env.id}
                  envRef={ref}
                  live={live}
                  inFlight={inFlight}
                  unavailable={unavailable[ref.env.id]}
                  onShowHistory={() => update({ repositoryId: ref.repo.id })}
                />
              );
            })}
          </div>
        )}
      </section>

      <Card className="p-5">
        <h3 className="text-heading font-semibold">{d("active.title")}</h3>
        <p className="mb-3 text-caption text-muted-foreground">{d("active.subtitle")}</p>
        {active.length === 0 ? (
          <p className="text-body text-muted-foreground">{d("active.empty")}</p>
        ) : (
          <DeployFeedTable items={active} onOpenRelease={setOpenRelease} />
        )}
      </Card>

      <Card className="p-5">
        <div className="mb-3 flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 className="text-heading font-semibold">{d("deployHistory.title")}</h3>
            <p className="text-caption text-muted-foreground">{d("deployHistory.subtitle")}</p>
          </div>
          <Tabs value={filter.state} onValueChange={(v) => update({ state: v as StateFilter })} variant="pill" className="w-fit">
            <TabsList aria-label={d("deployHistory.title")}>
              {STATES.map((s) => (
                <TabsTrigger key={s} value={s}>
                  {d(`deployHistory.state.${s}`)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </div>
        {shown.length === 0 ? (
          <p className="py-6 text-center text-body text-muted-foreground">{d("deployHistory.empty")}</p>
        ) : (
          <DeployFeedTable items={shown} onOpenRelease={setOpenRelease} />
        )}
        {canShowMore && shown.length > 0 && (
          <div className="mt-4 flex justify-center">
            <Button variant="outline" size="sm" onClick={showMore} disabled={loadingOlder}>
              {d("deployHistory.loadMore")}
            </Button>
          </div>
        )}
      </Card>

      {legacyMatrix && matrix && (
        <section className="space-y-3">
          <div>
            <h3 className="text-heading font-semibold">{d("legacy.title")}</h3>
            <p className="text-caption text-muted-foreground">{d("legacy.description")}</p>
          </div>
          <DeploymentMatrix
            view={{ repos: matrix.repos ?? [], envs: matrix.envs }}
            onSelect={(repo, cell) => setMatrixSelection({ repo, cell })}
          />
        </section>
      )}

      {openRelease?.release && (
        <ReleaseDrawer
          releaseId={openRelease.release.id}
          repositoryName={openRelease.repo?.name ?? ""}
          open
          onOpenChange={(open) => !open && setOpenRelease(null)}
          onChanged={() => void loadBase()}
        />
      )}
      {matrixSelection && (
        <DeploymentDetailDrawer
          repo={matrixSelection.repo}
          cell={matrixSelection.cell}
          open
          onOpenChange={(open) => !open && setMatrixSelection(null)}
          onActed={loadBase}
        />
      )}
    </div>
  );
}
