import type { DeployEnvironment } from "@/api";
import { IssuesView, useIncludeClosed } from "@/components/operations/issues/IssuesView";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { useI18n } from "@/hooks/useI18n";
import { useEntityIncidents } from "@/hooks/useOpsEntityData";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

/** One entity's issues: its repository's incidents in the picked
 * environment, attributed the way the explorer counts them, plus the ones
 * whose environment label the console does not know. */
export default function EntityIssues({ entity, env }: EntitySubPageProps) {
  const { t } = useI18n();
  const [includeClosed] = useIncludeClosed();
  useOpsPageScope({ time: includeClosed, compare: false, note: includeClosed ? undefined : t("operations.issues.liveNote") });
  return <EntityIssueList key={includeClosed ? "all" : "live"} entity={entity} env={env} includeClosed={includeClosed} />;
}

function EntityIssueList({ entity, env, includeClosed }: { entity: OpsEntity; env: DeployEnvironment; includeClosed: boolean }) {
  const { incidents, others, truncated, loading, error, reload } = useEntityIncidents(entity, env, { includeClosed });
  return (
    <IssuesView
      incidents={incidents}
      otherIncidents={others}
      truncated={truncated}
      loading={loading}
      error={error}
      onChanged={reload}
      entity={entity}
      showEntity={false}
    />
  );
}
