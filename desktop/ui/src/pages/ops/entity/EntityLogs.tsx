import { LogsExplorer } from "@/components/operations/logs/LogsExplorer";
import { EntityNotReporting } from "@/components/operations/query/EntityNotReporting";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useI18n } from "@/hooks/useI18n";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

export default function EntityLogs({ entity, env, environment }: EntitySubPageProps) {
  const { t } = useI18n();
  useOpsPageScope({ time: true, compare: false, env: true });
  if (!environment) {
    return (
      <EntityNotReporting
        entity={entity}
        env={env}
        description={t("operations.logsExplorer.notReporting", { name: entity.name, env: t(`cloud.environments.${env}`) })}
      />
    );
  }
  return <LogsExplorer key={entity.id} entity={entity} />;
}
