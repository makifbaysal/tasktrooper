import { BookMarked, Plus, Search, SearchX, Sparkles } from "lucide-react";
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import {
  ApiError,
  ERROR_DEFINITION_STATUSES,
  api,
  isNotSuggestedError,
  type ErrorDefinition,
  type ErrorDefinitionStatus,
  type ErrorSuggestRequest,
} from "@/api";
import { AIJobCard } from "@/components/operations/ai/AIJobCard";
import { countItems, isJobRunning } from "@/components/operations/ai/aiJobs";
import { ALL } from "@/components/operations/console/opsEntities";
import { AskAIDefineErrorsDialog } from "@/components/operations/errorDefinitions/AskAIDefineErrorsDialog";
import { ErrorDefinitionDetailPanel } from "@/components/operations/errorDefinitions/ErrorDefinitionDetailPanel";
import { ErrorDefinitionEditor } from "@/components/operations/errorDefinitions/ErrorDefinitionEditor";
import { ErrorDefinitionsTable } from "@/components/operations/errorDefinitions/ErrorDefinitionsTable";
import {
  definitionMatches,
  emptyDefinitionDraft,
  isDefinitionStatus,
  removeDefinition,
  statusCounts,
  upsertDefinition,
  type DefinitionDraft,
} from "@/components/operations/errorDefinitions/errorDefinitions";
import { useOpsContext } from "@/components/operations/shell/OpsContext";
import { useOpsDirectory, useOpsEntities } from "@/components/operations/shell/useOpsEntities";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useAIJob } from "@/hooks/useAIJob";
import { useErrorDefinitions } from "@/hooks/useErrorDefinitions";
import { useI18n } from "@/hooks/useI18n";

export const DEFINE_ERRORS_JOB_KEY = "operations.aiJob.defineErrors";

interface EditorState {
  definition?: ErrorDefinition;
  initial?: DefinitionDraft;
}

/** Error definitions: what each kind of runtime error means and what it
 * sets off. Filtered by repository and component, split by status (the
 * release engineer's suggestions wait in their own tab), one in the side
 * panel at a time; written by hand or asked of the AI. */
export function ErrorDefinitionsView() {
  const { t } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.errorDefinitions.${key}`, params);
  const { env } = useOpsContext();
  const [params, setParams] = useSearchParams();
  const { repositories } = useOpsDirectory();
  const { entities } = useOpsEntities();
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());
  const [editor, setEditor] = useState<EditorState | null>(null);
  const [asking, setAsking] = useState(false);

  const repositoryId = params.get("repository") ?? "";
  const componentId = repositoryId ? (params.get("component") ?? "") : "";
  const query = params.get("q") ?? "";
  const definitionId = params.get("definition");
  const statusParam = params.get("status");

  const list = useErrorDefinitions({ repositoryId: repositoryId || undefined, componentId: componentId || undefined });
  const all = useMemo(() => list.data ?? [], [list.data]);
  const selected = definitionId ? all.find((x) => x.id === definitionId) : undefined;
  const tab: ErrorDefinitionStatus = isDefinitionStatus(statusParam) ? statusParam : (selected?.status ?? "active");
  const counts = statusCounts(all);
  const visible = all.filter((x) => x.status === tab && definitionMatches(x, query, t));

  const ai = useAIJob(DEFINE_ERRORS_JOB_KEY, () => void list.reload());
  const suggestedByJob = countItems(ai.job, "error_definition");

  const entityById = useMemo(() => new Map(entities.map((x) => [x.id, x])), [entities]);
  const repoById = useMemo(() => new Map(repositories.map((r) => [r.id, r])), [repositories]);
  const repoComponents = useMemo(
    () => entities.filter((x) => x.repositoryId === repositoryId).sort((a, b) => a.name.localeCompare(b.name)),
    [entities, repositoryId],
  );

  const scopeOf = (def: ErrorDefinition): string => {
    const where = def.component_id
      ? (entityById.get(def.component_id)?.name ?? def.component_id.slice(0, 8))
      : d("everyComponent");
    if (repositoryId) return where;
    const repo = repoById.get(def.repository_id)?.name ?? def.repository_id.slice(0, 8);
    return d("scopeInRepository", { where, repository: repo });
  };

  const patchParams = (patch: Record<string, string | null>) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const [key, value] of Object.entries(patch)) {
          if (value === null || value === "") next.delete(key);
          else next.set(key, value);
        }
        return next;
      },
      { replace: true },
    );
  const withDefinition = (id: string | null) => {
    const next = new URLSearchParams(params);
    if (id) next.set("definition", id);
    else next.delete("definition");
    const qs = next.toString();
    return qs ? `?${qs}` : "?";
  };

  const setBusyFor = (id: string, on: boolean) =>
    setBusy((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });

  const refused = (def: ErrorDefinition, err: unknown) => {
    if (isNotSuggestedError(err)) {
      toast.info(d("writeErrors.notSuggested"));
      void list.reload();
    } else if (err instanceof ApiError && err.status === 404) {
      list.update((prev) => removeDefinition(prev, def.id));
      toast.info(d("writeErrors.gone"));
    } else {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    }
  };

  const review = async (def: ErrorDefinition, verdict: "accept" | "dismiss") => {
    setBusyFor(def.id, true);
    try {
      const saved = verdict === "accept" ? await api.acceptErrorDefinition(def.id) : await api.dismissErrorDefinition(def.id);
      list.update((prev) => upsertDefinition(prev, saved));
      toast.success(d(verdict === "accept" ? "accepted" : "dismissed", { name: saved.name }));
    } catch (err) {
      refused(def, err);
    } finally {
      setBusyFor(def.id, false);
    }
  };

  const remove = async (def: ErrorDefinition): Promise<string | null> => {
    setBusyFor(def.id, true);
    try {
      await api.deleteErrorDefinition(def.id);
    } catch (err) {
      if (!(err instanceof ApiError && err.status === 404)) {
        setBusyFor(def.id, false);
        return err instanceof Error ? err.message : t("common.actionFailed");
      }
    }
    setBusyFor(def.id, false);
    list.update((prev) => removeDefinition(prev, def.id));
    patchParams({ definition: null });
    toast.success(d("deleted", { name: def.name }));
    return null;
  };

  const saved = (def: ErrorDefinition) => {
    list.update((prev) => upsertDefinition(prev, def));
    patchParams({ definition: def.id, status: def.status === "active" ? null : def.status });
  };

  const startJob = (request: ErrorSuggestRequest) => ai.start(() => api.suggestErrorDefinitions(request));

  const filters = (
    <div className="flex flex-wrap items-center gap-2">
      <Select value={repositoryId || ALL} onValueChange={(v) => patchParams({ repository: v === ALL ? null : v, component: null, definition: null })}>
        <SelectTrigger className="h-8 w-52 text-caption" aria-label={d("filters.repository")}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{d("filters.allRepositories")}</SelectItem>
          {repositories.map((r) => (
            <SelectItem key={r.id} value={r.id}>
              {r.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select
        value={componentId || ALL}
        onValueChange={(v) => patchParams({ component: v === ALL ? null : v, definition: null })}
        disabled={!repositoryId}
      >
        <SelectTrigger className="h-8 w-52 text-caption" aria-label={d("filters.component")}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{d("filters.allComponents")}</SelectItem>
          {repoComponents.map((c) => (
            <SelectItem key={c.id} value={c.id}>
              {c.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <div className="relative w-full max-w-xs">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
        <Input
          type="search"
          value={query}
          onChange={(ev) => patchParams({ q: ev.target.value })}
          placeholder={d("filters.searchPlaceholder")}
          aria-label={d("filters.searchLabel")}
          className="h-8 pl-8 text-caption"
        />
      </div>
      <div className="ml-auto flex flex-wrap items-center gap-2">
        <Button size="sm" variant="outline" onClick={() => setAsking(true)} disabled={repositories.length === 0 || ai.starting || isJobRunning(ai.job)}>
          <Sparkles aria-hidden />
          {d("ask.button")}
        </Button>
        <Button size="sm" onClick={() => setEditor({ initial: emptyDefinitionDraft(repositoryId || (repositories.length === 1 ? repositories[0].id : ""), componentId) })}>
          <Plus aria-hidden />
          {d("new")}
        </Button>
      </div>
    </div>
  );

  let body;
  if (list.loading) {
    body = (
      <Card className="space-y-2 p-3" aria-busy>
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </Card>
    );
  } else if (!list.data) {
    body = (
      <Notice variant="error" title={d("loadFailed")}>
        {list.error}
      </Notice>
    );
  } else if (all.length === 0 && !query) {
    body = (
      <Card>
        <EmptyState
          icon={BookMarked}
          title={d("empty.title")}
          description={d("empty.body")}
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button size="sm" variant="outline" onClick={() => setAsking(true)} disabled={repositories.length === 0 || isJobRunning(ai.job)}>
                <Sparkles aria-hidden />
                {d("ask.button")}
              </Button>
              <Button size="sm" onClick={() => setEditor({ initial: emptyDefinitionDraft(repositoryId, componentId) })}>
                <Plus aria-hidden />
                {d("new")}
              </Button>
            </div>
          }
        />
      </Card>
    );
  } else if (visible.length === 0) {
    body = query ? (
      <Card>
        <EmptyState
          icon={SearchX}
          variant="search"
          title={d("noMatch.title")}
          action={
            <Button variant="outline" size="sm" onClick={() => patchParams({ q: null })}>
              {d("noMatch.action")}
            </Button>
          }
        />
      </Card>
    ) : (
      <Card className="px-4 py-8 text-center text-caption text-muted-foreground">{d(`tabEmpty.${tab}`)}</Card>
    );
  } else {
    body = (
      <ErrorDefinitionsTable
        definitions={visible}
        scopeOf={scopeOf}
        detailHref={(def) => withDefinition(def.id)}
        selectedId={definitionId}
        reviewing={tab === "suggested"}
        busy={busy}
        onAccept={(def) => void review(def, "accept")}
        onDismiss={(def) => void review(def, "dismiss")}
      />
    );
  }

  return (
    <div className="space-y-4">
      {filters}
      <AIJobCard
        job={ai.job}
        refusal={ai.refusal}
        startError={ai.startError}
        pollError={ai.pollError}
        onDismiss={ai.dismiss}
        actions={
          ai.job?.status === "done" && suggestedByJob > 0 ? (
            <Button
              size="sm"
              onClick={() => patchParams({ repository: ai.job?.repository_id ?? null, component: null, status: "suggested", definition: null, q: null })}
            >
              {d("reviewSuggestions", { count: suggestedByJob })}
            </Button>
          ) : undefined
        }
      />
      <Tabs value={tab} onValueChange={(v) => patchParams({ status: v === "active" ? null : v, definition: null })} variant="pill" className="w-fit">
        <TabsList aria-label={d("tabs.label")}>
          {ERROR_DEFINITION_STATUSES.map((s) => (
            <TabsTrigger key={s} value={s}>
              {d(`tabs.${s}`)}
              {s === "suggested" && counts.suggested > 0 ? (
                <Badge variant="info" className="px-1.5 py-0 text-micro tabular-nums">
                  {counts.suggested}
                </Badge>
              ) : (
                <span className="text-micro tabular-nums text-muted-foreground">{counts[s]}</span>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      {list.data && list.error && (
        <Notice variant="warning" title={d("refreshFailed")}>
          {list.error}
        </Notice>
      )}
      {body}

      <ErrorDefinitionDetailPanel
        definitionId={definitionId}
        definition={selected}
        loading={list.loading}
        scope={selected ? scopeOf(selected) : ""}
        busy={selected ? busy.has(selected.id) : false}
        onOpenChange={(open) => !open && patchParams({ definition: null })}
        onEdit={(def) => setEditor({ definition: def })}
        onAccept={(def) => void review(def, "accept")}
        onDismiss={(def) => void review(def, "dismiss")}
        onDelete={remove}
      />
      {editor && (
        <ErrorDefinitionEditor
          open
          onOpenChange={(open) => !open && setEditor(null)}
          repositories={repositories}
          entities={entities}
          definition={editor.definition}
          initial={editor.initial}
          onSaved={saved}
        />
      )}
      {asking && (
        <AskAIDefineErrorsDialog
          open
          onOpenChange={setAsking}
          repositories={repositories}
          entities={entities}
          defaultRepositoryId={repositoryId || undefined}
          defaultComponentId={componentId || undefined}
          env={env}
          onStart={startJob}
        />
      )}
    </div>
  );
}
