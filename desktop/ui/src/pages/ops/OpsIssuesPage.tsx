import { X } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { useSearchParams } from "react-router-dom";
import { EntityPicker } from "@/components/operations/console/EntityPicker";
import { ALL } from "@/components/operations/console/opsEntities";
import { IssuesView, useIncludeClosed } from "@/components/operations/issues/IssuesView";
import { otherEnvironmentIssues, scopeIssues, siblingsOf } from "@/components/operations/issues/issueList";
import { useOpsContext, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useOpsDirectory, useOpsEntities, useOpsEntity, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { issueFeedKey, useIssueFeed } from "@/hooks/useOpsIssues";

/** `/operations/issues`: every live incident in the environment picked in
 * the top bar; `?entity=` / `?repository=` narrow it, `?closed=1` adds the
 * resolved and ignored ones in the window, `?id=` opens one. */
export default function OpsIssuesPage() {
  const { t } = useI18n();
  const i = (key: string, params?: Record<string, string | number>) => t(`operations.issues.${key}`, params);
  const [params, setParams] = useSearchParams();
  const [includeClosed] = useIncludeClosed();
  useOpsPageScope({ time: includeClosed, compare: false, note: includeClosed ? undefined : i("liveNote") });

  const entityId = params.get("entity") ?? undefined;
  const repositoryParam = params.get("repository") ?? undefined;
  const { entity, loading } = useOpsEntity(entityId);
  const { entities } = useOpsEntities();
  const { repositories } = useOpsDirectory();

  const update = (apply: (next: URLSearchParams) => void) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        apply(next);
        next.delete("id");
        return next;
      },
      { replace: true },
    );

  const pick = (value: string) =>
    update((next) => {
      next.delete("repository");
      if (value === ALL) next.delete("entity");
      else next.set("entity", value);
    });

  if (entityId && !entity && loading) {
    return (
      <div className="space-y-4" aria-busy>
        <Skeleton className="h-9 w-full max-w-xl" />
        <Skeleton className="h-72 w-full" />
      </div>
    );
  }

  const repositoryId = entity?.repositoryId ?? repositoryParam;
  const repositoryName = repositories.find((r) => r.id === repositoryParam)?.name;

  const scopeNote: ReactNode = (
    <>
      {entityId && !entity && <Notice variant="info" title={i("entityNotFound")} />}
      {!entity && repositoryParam && (
        <div>
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1.5 rounded-full px-3"
            aria-label={i("clearRepository")}
            onClick={() => update((next) => next.delete("repository"))}
          >
            {i("repositoryFilter", { name: repositoryName ?? i("unknownRepository") })}
            <X aria-hidden />
          </Button>
        </div>
      )}
    </>
  );

  return (
    <GlobalIssues
      key={issueFeedKey(repositoryId, includeClosed)}
      repositoryId={repositoryId}
      includeClosed={includeClosed}
      entity={entity}
      scopeNote={scopeNote}
      toolbarStart={
        <EntityPicker
          entities={entities}
          current={entity}
          value={entity?.id ?? ALL}
          label={i("entity")}
          allLabel={i("allEntities")}
          onChange={pick}
        />
      }
    />
  );
}

interface GlobalIssuesProps {
  repositoryId?: string;
  includeClosed: boolean;
  entity?: OpsEntity;
  toolbarStart: ReactNode;
  scopeNote: ReactNode;
}

function GlobalIssues({ repositoryId, includeClosed, entity, toolbarStart, scopeNote }: GlobalIssuesProps) {
  const { env } = useOpsContext();
  const { entities } = useOpsEntities();
  const feed = useIssueFeed(repositoryId, includeClosed);
  const incidents = useMemo(
    () => scopeIssues(feed.incidents, { env, entity, siblings: entity ? siblingsOf(entity, entities) : undefined, repositoryId }),
    [feed.incidents, env, entity, entities, repositoryId],
  );
  const others = useMemo(() => otherEnvironmentIssues(feed.incidents, { entity, repositoryId }), [feed.incidents, entity, repositoryId]);
  return (
    <IssuesView
      incidents={incidents}
      otherIncidents={others}
      truncated={feed.truncated}
      loading={feed.loading}
      error={feed.error}
      onChanged={feed.reload}
      entity={entity}
      showEntity={!entity}
      toolbarStart={toolbarStart}
      scopeNote={scopeNote}
    />
  );
}
