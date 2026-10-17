import { BookMarked, Bug, Search, SearchX, Unplug } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { api, type OpsErrorGroup } from "@/api";
import { isEntityType } from "@/components/operations/console/entityTypes";
import { ALL, type ExplorerRowType } from "@/components/operations/console/opsEntities";
import { entitySectionPath } from "@/components/operations/console/roleViews";
import { ErrorDefinitionEditor } from "@/components/operations/errorDefinitions/ErrorDefinitionEditor";
import { ERROR_DEFINITIONS_PATH, definitionPath, draftFromGroup } from "@/components/operations/errorDefinitions/errorDefinitions";
import { ErrorGroupDetail } from "@/components/operations/errors/ErrorGroupDetail";
import { ErrorGroupTable } from "@/components/operations/errors/ErrorGroupTable";
import {
  boundGroup,
  canCreateTask,
  groupKey,
  groupMatches,
  hasErrorFeed,
  isBindingRow,
  runtimeGroup,
  typeCountsOf,
  type ErrorFilter,
} from "@/components/operations/errors/errorGroups";
import { TypeFacetChips } from "@/components/operations/explorer/TypeFacetChips";
import { EntityMultiSelect } from "@/components/operations/query/EntityMultiSelect";
import {
  entityIndex,
  environmentIndex,
  errorsWindow,
  formatIdList,
  parseIdList,
  windowKey,
} from "@/components/operations/query/opsQuery";
import { UnavailableNotice } from "@/components/operations/query/UnavailableNotice";
import { useOpsContext, useOpsHref, useOpsShellState } from "@/components/operations/shell/OpsContext";
import { useOpsDirectory, useOpsEntities, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { useOpsErrors } from "@/hooks/useOpsErrors";
import { copyText } from "@/lib/clipboard";
import { cn, formatDate } from "@/lib/utils";

const PAGE = 100;
const FILTER_KEYS = ["entity", "type", "new", "q"];

interface ErrorsInboxProps {
  /** One entity's inbox: no entity or type filter, its component only. */
  entity?: OpsEntity;
  /** The entity has a confirmed errors.issues binding (Sentry), which is read with no environment connected. */
  bindingRead?: boolean;
}

/** New Relic's Errors inbox over every connected environment of the one
 * picked: grouped errors, newest problems first, with a side panel for one. */
export function ErrorsInbox({ entity: scoped, bindingRead = false }: ErrorsInboxProps) {
  const { t } = useI18n();
  const e = (key: string, params?: Record<string, string | number>) => t(`operations.errorsInbox.${key}`, params);
  const { range, env } = useOpsContext();
  const { refreshedAt } = useOpsShellState();
  const href = useOpsHref();
  const navigate = useNavigate();
  const newOnlyId = useId();
  const ignoredId = useId();
  const [params, setParams] = useSearchParams();
  const { entities: listed, loading: entitiesLoading } = useOpsEntities();
  const { repositories } = useOpsDirectory();
  const [limit, setLimit] = useState(PAGE);
  const [creating, setCreating] = useState<string | undefined>(undefined);
  const [defining, setDefining] = useState<OpsErrorGroup | null>(null);

  const entities = useMemo(
    () => (scoped && !listed.some((x) => x.id === scoped.id) ? [...listed, scoped] : listed),
    [listed, scoped],
  );
  const byId = useMemo(() => entityIndex(entities), [entities]);
  const environments = useMemo(() => environmentIndex(entities), [entities]);

  const entityParam = params.get("entity");
  const picked = useMemo(() => (scoped ? [scoped.id] : parseIdList(entityParam)), [scoped, entityParam]);
  const typeParam = params.get("type");
  const filter: ErrorFilter = {
    type: !scoped && isEntityType(typeParam) ? typeParam : undefined,
    newOnly: params.get("new") === "1",
    query: params.get("q") ?? "",
  };
  const selectedKey = params.get("group") ?? undefined;

  const win = errorsWindow(range, refreshedAt);
  const envLabel = t(`cloud.environments.${env}`);
  const inScope = scoped ? [scoped] : picked.length > 0 ? entities.filter((x) => picked.includes(x.id)) : entities;
  const connected = inScope.flatMap((x) => x.environments.filter((v) => v.environment === env));
  // The console-wide inbox always reads: the server also reads the
  // components that only a binding (a Sentry project) feeds. One entity's
  // inbox is read when it runs here or has such a binding.
  const enabled = !scoped || bindingRead || connected.length > 0;

  const showIgnored = params.get("ignored") === "1";
  const { data, error, loading, reload } = useOpsErrors(
    { env, since: win.since, componentIds: picked, window: windowKey(range), includeIgnored: showIgnored },
    enabled,
  );
  const ignoredCount = data?.ignored_count ?? 0;

  const groups = data?.groups ?? [];
  const visible = groups.filter((g) => groupMatches(g, filter, byId));
  const typeCounts = typeCountsOf(groups, filter, byId) as Map<ExplorerRowType, number>;
  const selected = selectedKey ? groups.find((g) => groupKey(g) === selectedKey) : undefined;
  const sinceText = formatDate(new Date(win.since).toISOString());

  const setParam = (key: string, value: string | null) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value === null || value === "") next.delete(key);
        else next.set(key, value);
        return next;
      },
      { replace: true },
    );
  const withGroup = (key: string | null) => {
    const next = new URLSearchParams(params);
    if (key) next.set("group", key);
    else next.delete("group");
    const qs = next.toString();
    return qs ? `?${qs}` : "?";
  };
  const clearFilters = () =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const key of FILTER_KEYS) if (!scoped || (key !== "entity" && key !== "type")) next.delete(key);
        return next;
      },
      { replace: true },
    );

  const entityHref = scoped ? undefined : (x: OpsEntity) => href(entitySectionPath(x, "errors"));

  const createTask = async (g: OpsErrorGroup) => {
    if (!canCreateTask(g)) return;
    setCreating(groupKey(g));
    try {
      const task = isBindingRow(g)
        ? await api.createBoundErrorTask(g.component_id, boundGroup(g, env))
        : await api.createErrorTask(g.environment_id, runtimeGroup(g));
      toast.success(e("taskCreated"), {
        action: { label: task.key, onClick: () => navigate(`/board?task=${encodeURIComponent(task.id)}`) },
      });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setCreating(undefined);
    }
  };
  const definitionHref = (g: OpsErrorGroup) =>
    g.definition ? href(definitionPath({ id: g.definition.id, repository_id: g.repository_id })) : undefined;
  const definitionSaved = (def: { id: string; repository_id: string }) => {
    toast.success(t("operations.errorDefinitions.inbox.definitionSaved"), {
      action: { label: t("operations.errorDefinitions.inbox.viewDefinition"), onClick: () => navigate(href(definitionPath(def))) },
    });
    void reload();
  };
  const definitionsHref = scoped
    ? href(`${ERROR_DEFINITIONS_PATH}?${new URLSearchParams({ repository: scoped.repositoryId, component: scoped.id }).toString()}`)
    : undefined;

  const copyFingerprint = async (g: OpsErrorGroup) => {
    if (await copyText(g.fingerprint)) toast.success(e("copied"));
    else toast.error(e("copyFailed"));
  };

  if (!scoped && entitiesLoading && listed.length === 0) {
    return (
      <div className="space-y-3" aria-busy>
        <Skeleton className="h-8 w-full max-w-xl" />
        <Skeleton className="h-72 w-full" />
      </div>
    );
  }

  const filters = (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        {!scoped && (
          <EntityMultiSelect entities={listed} selected={picked} onChange={(ids) => setParam("entity", formatIdList(ids))} />
        )}
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input
            type="search"
            value={filter.query}
            onChange={(ev) => setParam("q", ev.target.value)}
            placeholder={e("searchPlaceholder")}
            aria-label={e("searchLabel")}
            className="h-8 pl-8 text-caption"
          />
        </div>
        <div className="ml-auto flex flex-wrap items-center gap-x-4 gap-y-2">
          {(showIgnored || ignoredCount > 0) && (
            <div className="flex items-center gap-2">
              <Switch id={ignoredId} checked={showIgnored} onCheckedChange={(on) => setParam("ignored", on ? "1" : null)} />
              <Label htmlFor={ignoredId} className="cursor-pointer text-caption font-normal text-muted-foreground">
                {showIgnored
                  ? t("operations.errorDefinitions.inbox.showIgnored")
                  : t("operations.errorDefinitions.inbox.showIgnoredCount", { count: ignoredCount })}
              </Label>
            </div>
          )}
          <div className="flex items-center gap-2">
            <Switch id={newOnlyId} checked={filter.newOnly} onCheckedChange={(on) => setParam("new", on ? "1" : null)} />
            <Label htmlFor={newOnlyId} className="cursor-pointer text-caption font-normal text-muted-foreground">
              {e("newOnly")}
            </Label>
          </div>
          {definitionsHref && (
            <Button variant="ghost" size="sm" className="h-8" asChild>
              <Link to={definitionsHref}>
                <BookMarked aria-hidden />
                {t("operations.errorDefinitions.inbox.definitions")}
              </Link>
            </Button>
          )}
        </div>
      </div>
      {!scoped && groups.length > 0 && (
        <TypeFacetChips counts={typeCounts} active={filter.type ?? ALL} onSelect={(v) => setParam("type", v === ALL ? null : v)} />
      )}
    </div>
  );

  const noFeed = !bindingRead && !connected.some(hasErrorFeed);
  const nothingConnectedCard = (
    <Card>
      <EmptyState
        icon={Unplug}
        title={e("noEnvironments.title", { env: envLabel })}
        description={e("noEnvironments.body")}
        action={
          <Button variant="outline" size="sm" asChild>
            <Link to={href("/operations")}>{e("noEnvironments.action")}</Link>
          </Button>
        }
      />
    </Card>
  );
  if (!enabled) return nothingConnectedCard;

  let body;
  if (loading) {
    body = (
      <Card className="space-y-2 p-3" aria-busy>
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </Card>
    );
  } else if (!data) {
    body = (
      <Notice variant="error" title={e("loadFailed")}>
        {error}
      </Notice>
    );
  } else if (groups.length === 0 && connected.length === 0 && !bindingRead && (data.unavailable ?? []).length === 0 && ignoredCount === 0) {
    body = nothingConnectedCard;
  } else if (groups.length === 0) {
    body = (
      <Card>
        <EmptyState
          icon={Bug}
          title={e("empty.title")}
          description={noFeed ? e("empty.noFeed", { env: envLabel }) : e("empty.body", { env: envLabel, since: sinceText })}
          action={
            ignoredCount > 0 ? (
              <Button variant="outline" size="sm" onClick={() => setParam("ignored", "1")}>
                {t("operations.errorDefinitions.inbox.showIgnoredCount", { count: ignoredCount })}
              </Button>
            ) : undefined
          }
        />
      </Card>
    );
  } else if (visible.length === 0) {
    body = (
      <Card>
        <EmptyState
          icon={SearchX}
          variant="search"
          title={e("noMatch.title")}
          action={
            <Button variant="outline" size="sm" onClick={clearFilters}>
              {e("noMatch.action")}
            </Button>
          }
        />
      </Card>
    );
  } else {
    const shown = visible.slice(0, limit);
    body = (
      <div className="space-y-2">
        <p className="text-micro text-muted-foreground tabular-nums">{e("shownOf", { shown: shown.length, total: visible.length })}</p>
        <ErrorGroupTable
          groups={shown}
          environments={environments}
          entities={byId}
          window={{ from: win.since, to: refreshedAt }}
          selectedKey={selectedKey}
          selectHref={(g) => withGroup(groupKey(g))}
          entityHref={entityHref}
          showEntity={!scoped}
          creatingKey={creating}
          onCreateTask={(g) => void createTask(g)}
          onCopyFingerprint={(g) => void copyFingerprint(g)}
          definitionHref={definitionHref}
          onDefine={setDefining}
        />
        {visible.length > shown.length && (
          <div className="flex justify-center">
            <Button variant="outline" size="sm" onClick={() => setLimit((n) => n + PAGE)}>
              {e("showMore", { count: Math.min(PAGE, visible.length - shown.length) })}
            </Button>
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="@container space-y-4">
      {filters}
      {win.clamped && (
        <Notice variant="info" title={e("clamped.title")}>
          {e("clamped.body", { since: sinceText })}
        </Notice>
      )}
      {win.openEnded && !win.clamped && (
        <Notice variant="info" title={e("openEnded.title")}>
          {e("openEnded.body", { since: sinceText })}
        </Notice>
      )}
      {data && error && (
        <Notice variant="warning" title={e("refreshFailed")}>
          {error}
        </Notice>
      )}
      {data && <UnavailableNotice unavailable={data.unavailable} entities={entities} />}
      {data?.truncated && (
        <Notice variant="info" title={e("truncated.title")}>
          {e("truncated.body")}
        </Notice>
      )}
      <div className={cn("grid gap-4", selected && "@4xl:grid-cols-[minmax(0,1fr)_22rem]")}>
        <div className="min-w-0">{body}</div>
        {selected && (
          <ErrorGroupDetail
            className="order-first @4xl:order-none @4xl:sticky @4xl:top-0 @4xl:self-start"
            group={selected}
            environment={environments.get(selected.environment_id)}
            entity={environments.get(selected.environment_id)?.entity ?? byId.get(selected.component_id)}
            envLabel={envLabel}
            entityHref={entityHref}
            creating={creating === groupKey(selected)}
            closeHref={withGroup(null)}
            onCreateTask={() => void createTask(selected)}
            onCopyFingerprint={() => void copyFingerprint(selected)}
            definitionHref={definitionHref(selected)}
            onDefine={() => setDefining(selected)}
          />
        )}
      </div>
      {defining && (
        <ErrorDefinitionEditor
          open
          onOpenChange={(open) => !open && setDefining(null)}
          repositories={repositories}
          entities={entities}
          initial={draftFromGroup(defining)}
          sample={defining.sample && defining.sample !== defining.message ? `${defining.message}\n${defining.sample}` : defining.message}
          onSaved={definitionSaved}
        />
      )}
    </div>
  );
}
