import { useSearchParams } from "react-router-dom";
import { AlertsView } from "@/components/operations/alertRules/AlertsView";
import { EntityPicker } from "@/components/operations/console/EntityPicker";
import { ALL } from "@/components/operations/console/opsEntities";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useOpsEntities, useOpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

/** `/operations/alerts`: every alert rule in the environment picked in the
 * top bar; `?entity=` narrows it to one, `?state=` to a state, `?rule=` opens one. */
export default function OpsAlertsPage() {
  const { t } = useI18n();
  const a = (key: string) => t(`operations.alertRules.${key}`);
  useOpsPageScope({ time: true, compare: false });
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
        next.delete("rule");
        return next;
      },
      { replace: true },
    );

  if (entityId && !entity && loading) {
    return (
      <div className="space-y-4" aria-busy>
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
    );
  }

  const picker = (
    <EntityPicker
      entities={entities}
      current={entity}
      value={entity?.id ?? ALL}
      label={a("entity")}
      allLabel={a("allEntities")}
      onChange={pick}
    />
  );

  return (
    <div className="space-y-4">
      {entityId && !entity && <Notice variant="info" title={a("entityNotFound")} />}
      <AlertsView key={entity?.id ?? ALL} entity={entity} showEntity={!entity} toolbarStart={picker} />
    </div>
  );
}
