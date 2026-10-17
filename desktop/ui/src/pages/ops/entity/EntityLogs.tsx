import { LogsExplorer } from "@/components/operations/logs/LogsExplorer";
import { EntityNotReporting } from "@/components/operations/query/EntityNotReporting";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useSignalBound } from "@/hooks/useOpsSignals";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

export const LOGS_SIGNAL = "logs.lines";

export default function EntityLogs({ entity, env, environment }: EntitySubPageProps) {
  const { t } = useI18n();
  useOpsPageScope({ time: true, compare: false, env: true });
  // With no environment connected, a logs.lines binding (Loki,
  // Elasticsearch) is still read.
  const bound = useSignalBound(entity.id, env, LOGS_SIGNAL, !environment);
  if (!environment && bound === undefined) return <Skeleton className="h-48 w-full" />;
  if (!environment && !bound) {
    return (
      <EntityNotReporting
        entity={entity}
        env={env}
        description={t("operations.logsExplorer.notReporting", { name: entity.name, env: t(`cloud.environments.${env}`) })}
      />
    );
  }
  return <LogsExplorer key={entity.id} entity={entity} bindingRead={!environment} />;
}
