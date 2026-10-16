import { ErrorsInbox } from "@/components/operations/errors/ErrorsInbox";
import { EntityNotReporting } from "@/components/operations/query/EntityNotReporting";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useI18n } from "@/hooks/useI18n";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

export default function EntityErrors({ entity, env, environment }: EntitySubPageProps) {
  const { t } = useI18n();
  useOpsPageScope({ time: true, compare: false, env: true });
  if (!environment) {
    return (
      <EntityNotReporting
        entity={entity}
        env={env}
        description={t("operations.errorsInbox.notReporting", { name: entity.name, env: t(`cloud.environments.${env}`) })}
      />
    );
  }
  return <ErrorsInbox key={entity.id} entity={entity} />;
}
