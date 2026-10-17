import { Link } from "react-router-dom";
import type { OpsUnavailable } from "@/api";
import { repositoryDeployPath } from "@/components/operations/console/opsEntities";
import { dataSourcePath } from "@/components/operations/errors/errorGroups";
import { environmentIndex } from "@/components/operations/query/opsQuery";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

export const CLOUD_ACCOUNTS_PATH = "/settings/integrations";

interface UnavailableNoticeProps {
  unavailable: OpsUnavailable[];
  entities: OpsEntity[];
}

/** The environments a fan-out could not read, each with why and the fix:
 * reconnect the account, connect the environment again, or try again. */
export function UnavailableNotice({ unavailable, entities }: UnavailableNoticeProps) {
  const { t } = useI18n();
  const u = (key: string, params?: Record<string, string | number>) => t(`operations.opsQuery.unavailable.${key}`, params);
  if (unavailable.length === 0) return null;
  const index = environmentIndex(entities);
  const byEntity = new Map(entities.map((e) => [e.id, e]));

  return (
    <Notice variant="warning" title={u("title", { count: unavailable.length })}>
      <ul className="mt-1 space-y-1 text-caption">
        {unavailable.map((item) => {
          const ref = index.get(item.environment_id);
          const entity = ref?.entity ?? byEntity.get(item.component_id);
          const fix = item.data_source_id ? (
            <Link to={dataSourcePath(item.data_source_id)} className="font-medium text-info hover:underline">
              {t("operations.telemetry.errors.fixSource")}
            </Link>
          ) : item.code === "cloud_auth" ? (
            <Link to={CLOUD_ACCOUNTS_PATH} className="font-medium text-info hover:underline">
              {u("fix.cloud_auth")}
            </Link>
          ) : item.code === "not_connected" && entity ? (
            <Link to={repositoryDeployPath(entity.repositoryId)} className="font-medium text-info hover:underline">
              {u("fix.not_connected")}
            </Link>
          ) : null;
          return (
            <li
              key={`${item.data_source_id ?? item.environment_id}\u0000${item.component_id}\u0000${item.code}`}
              className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5"
            >
              <span className="font-medium">{entity?.name ?? u("unknownEntity")}</span>
              {ref?.resource && <span className="truncate font-mono text-micro opacity-80">{ref.resource}</span>}
              <span className="opacity-90">{u(`reason.${item.code}`)}</span>
              {item.code === "provider_error" && item.message && (
                <span className="max-w-full truncate font-mono text-micro opacity-80" title={item.message}>
                  {item.message}
                </span>
              )}
              {fix}
            </li>
          );
        })}
      </ul>
    </Notice>
  );
}
