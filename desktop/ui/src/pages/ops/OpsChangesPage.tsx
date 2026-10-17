import { useSearchParams } from "react-router-dom";
import { ChangesView } from "@/components/operations/changes/ChangesView";
import { WorkflowDeploysSection } from "@/components/operations/changes/WorkflowDeploysSection";
import { EntityPicker } from "@/components/operations/console/EntityPicker";
import { ALL } from "@/components/operations/console/opsEntities";
import { useOpsContext, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useOpsEntities, useOpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

/** `/operations/changes`: every deploy across entities in the environment
 * and window picked in the top bar; `?entity=` narrows it to one. Below the
 * feed, the workflow deploy targets outside release tracking, when any. */
export default function OpsChangesPage() {
  const { t } = useI18n();
  const c = (key: string) => t(`operations.changes.${key}`);
  useOpsPageScope({ time: true, compare: false });
  const { env } = useOpsContext();
  const [params, setParams] = useSearchParams();
  const entityId = params.get("entity") ?? undefined;
  const { entities } = useOpsEntities();
  const { entity, loading } = useOpsEntity(entityId);

  const pick = (value: string) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value === ALL) next.delete("entity");
        else next.set("entity", value);
        next.delete("release");
        return next;
      },
      { replace: true },
    );

  if (entityId && !entity && loading) {
    return (
      <div className="space-y-4" aria-busy>
        <Skeleton className="h-9 w-full max-w-xl" />
        <Skeleton className="h-72 w-full" />
      </div>
    );
  }

  const picker = (
    <EntityPicker
      entities={entities}
      current={entity}
      value={entity?.id ?? ALL}
      label={c("entity")}
      allLabel={c("allEntities")}
      onChange={pick}
    />
  );

  return (
    <div className="space-y-4">
      {entityId && !entity && <Notice variant="info" title={c("entityNotFound")} />}
      <ChangesView entity={entity} showEntity={!entity} toolbarStart={picker} />
      <WorkflowDeploysSection environment={env} repositoryId={entity?.repositoryId} />
    </div>
  );
}
