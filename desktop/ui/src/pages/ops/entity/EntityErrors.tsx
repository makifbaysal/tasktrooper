import { ErrorsInbox } from "@/components/operations/errors/ErrorsInbox";
import { EntityNotReporting } from "@/components/operations/query/EntityNotReporting";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useSignalBound } from "@/hooks/useOpsSignals";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

export const ERRORS_SIGNAL = "errors.issues";

export default function EntityErrors({ entity, env, environment }: EntitySubPageProps) {
  const { t } = useI18n();
  useOpsPageScope({ time: true, compare: false, env: true });
  // With no environment connected, a Sentry project bound to errors.issues
  // is still read.
  const bound = useSignalBound(entity.id, env, ERRORS_SIGNAL, !environment);
  if (!environment && bound === undefined) return <Skeleton className="h-48 w-full" />;
  if (!environment && !bound) {
    return (
      <EntityNotReporting
        entity={entity}
        env={env}
        description={t("operations.errorsInbox.notReporting", { name: entity.name, env: t(`cloud.environments.${env}`) })}
      />
    );
  }
  return <ErrorsInbox key={entity.id} entity={entity} bindingRead={!environment} />;
}
