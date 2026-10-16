import { AlertsView } from "@/components/operations/alertRules/AlertsView";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useI18n } from "@/hooks/useI18n";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

/** An entity's Alerts: its rules in the environment picked above, with
 * "Suggest rules" and "New rule" scoped to it. */
export default function EntityAlerts({ entity, env }: EntitySubPageProps) {
  const { t } = useI18n();
  useOpsPageScope({ time: true, compare: false, env: true });
  return (
    <div className="space-y-4">
      <div className="min-w-0 space-y-1">
        <h2 className="text-heading font-semibold">{t("operations.alertRules.entityTitle")}</h2>
        <p className="max-w-prose text-caption text-muted-foreground">
          {t("operations.alertRules.entityDescription", { name: entity.name, env: t(`cloud.environments.${env}`) })}
        </p>
      </div>
      <AlertsView entity={entity} showEntity={false} />
    </div>
  );
}
